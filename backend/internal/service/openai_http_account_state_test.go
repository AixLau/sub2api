package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/responsesstate"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Embedding GatewayCache keeps this test focused on the HTTP-state extension;
// ResolveHTTPAccountState only calls the two methods implemented below.
type httpAccountStateCacheStub struct {
	GatewayCache
	owners map[string]int64
	getErr error
	binds  int
}

func (s *httpAccountStateCacheStub) GetHTTPAccountStateOwner(_ context.Context, _, _ int64, key string) (int64, error) {
	if s.getErr != nil {
		return 0, s.getErr
	}
	owner := s.owners[key]
	if owner == 0 {
		return 0, ErrStickySessionNotFound
	}
	return owner, nil
}

func (s *httpAccountStateCacheStub) BindHTTPAccountStateOwner(_ context.Context, _, _ int64, key string, owner int64, _ time.Duration) error {
	if existing := s.owners[key]; existing != 0 && existing != owner {
		return ErrOpenAIHTTPStateOwnerConflict
	}
	s.owners[key] = owner
	s.binds++
	return nil
}

func TestResolveHTTPAccountStateBindsKnownOwnerAndFailsClosed(t *testing.T) {
	groupID := int64(9)
	body := []byte(`{"input":[{"type":"reasoning","encrypted_content":"cipher"}]}`)
	keys := responsesstate.InputKeys(body)
	cache := &httpAccountStateCacheStub{owners: map[string]int64{keys[0]: 41}}
	svc := &OpenAIGatewayService{cache: cache}

	binding, err := svc.ResolveHTTPAccountState(context.Background(), &groupID, 7, body)
	require.NoError(t, err)
	require.Equal(t, int64(41), binding.AccountID)
	require.Equal(t, 1, cache.binds)

	unknown, err := svc.ResolveHTTPAccountState(context.Background(), &groupID, 7, []byte(`{"input":[{"type":"compaction","encrypted_content":"new"}]}`))
	require.Zero(t, unknown.AccountID)
	var stateErr *OpenAIHTTPAccountStateError
	require.ErrorAs(t, err, &stateErr)
	require.Equal(t, "account_bound_state_unknown", stateErr.Code)
}

func TestResolveHTTPAccountStateRejectsMixedOwnersAndStoreErrors(t *testing.T) {
	groupID := int64(9)
	body := []byte(`{"input":[{"type":"reasoning","encrypted_content":"a"},{"type":"compaction","encrypted_content":"b"}]}`)
	keys := responsesstate.InputKeys(body)
	cache := &httpAccountStateCacheStub{owners: map[string]int64{keys[0]: 41, keys[1]: 42}}
	svc := &OpenAIGatewayService{cache: cache}
	_, err := svc.ResolveHTTPAccountState(context.Background(), &groupID, 7, body)
	var stateErr *OpenAIHTTPAccountStateError
	require.ErrorAs(t, err, &stateErr)
	require.Equal(t, "account_bound_state_conflict", stateErr.Code)

	cache = &httpAccountStateCacheStub{owners: map[string]int64{keys[0]: 41}, getErr: errors.New("redis down")}
	svc.cache = cache
	_, err = svc.ResolveHTTPAccountState(context.Background(), &groupID, 7, body)
	require.ErrorAs(t, err, &stateErr)
	require.Equal(t, "account_state_store_unavailable", stateErr.Code)
}

func TestOpenAIGatewayServiceForwardRejectsHTTPAccountOwnerMismatchBeforeNativeFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	ctx := WithHTTPAccountStateBinding(context.Background(), HTTPAccountStateBinding{AccountID: 1})
	account := &Account{ID: 2}

	result, err := (&OpenAIGatewayService{}).Forward(ctx, c, account, []byte(`{"model":"gpt-5.1","input":[{"type":"reasoning","encrypted_content":"cipher"}]}`))

	require.Nil(t, result)
	var stateErr *OpenAIHTTPAccountStateError
	require.ErrorAs(t, err, &stateErr)
	require.Equal(t, "account_bound_state_unavailable", stateErr.Code)
	// The owner guard runs before plugin/native routing, so account B cannot
	// receive the replayed encrypted state as a fallback request.
	require.Empty(t, recorder.Body.String())
}

func TestOpenAIHTTPAccountStateErrorIsNotRetryableTransportFailure(t *testing.T) {
	err := HTTPAccountStateUnavailable()
	require.False(t, errors.Is(err, context.DeadlineExceeded))
	require.Equal(t, 400, err.Status)
}
