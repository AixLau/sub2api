package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexSessionIdentityV2Store struct {
	GatewayCache
	mu     sync.Mutex
	values map[string]string
	sets   int
}

// codexSessionIdentitySharedBackend models one durable Redis-like namespace.
// Each test instance gets its own cache wrapper below; only this backend is
// shared, so the tests exercise cross-process SetNX/read behavior rather than
// accidentally relying on one service's in-memory state.
type codexSessionIdentitySharedBackend struct {
	mu     sync.Mutex
	values map[string]string
	sets   int
}

type codexSessionIdentitySharedStore struct {
	GatewayCache
	backend *codexSessionIdentitySharedBackend
}

// codexSessionIdentityRedisStore gives each service its own Redis client while
// both clients connect to the same durable Redis namespace.
type codexSessionIdentityRedisStore struct {
	GatewayCache
	client *redis.Client
}

func (s *codexSessionIdentityRedisStore) GetCodexSessionIdentity(ctx context.Context, key string) (string, error) {
	value, err := s.client.Get(ctx, "openai_codex_session_identity:"+key).Result()
	if err == redis.Nil {
		return "", ErrCodexSessionIdentityNotFound
	}
	return value, err
}

func (s *codexSessionIdentityRedisStore) SetCodexSessionIdentityIfAbsent(ctx context.Context, key, value string) (bool, error) {
	return s.client.SetNX(ctx, "openai_codex_session_identity:"+key, value, 0).Result()
}

func (s *codexSessionIdentitySharedStore) GetCodexSessionIdentity(_ context.Context, key string) (string, error) {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	value, ok := s.backend.values[key]
	if !ok {
		return "", ErrCodexSessionIdentityNotFound
	}
	return value, nil
}

func (s *codexSessionIdentitySharedStore) SetCodexSessionIdentityIfAbsent(_ context.Context, key, value string) (bool, error) {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	if _, ok := s.backend.values[key]; ok {
		return false, nil
	}
	s.backend.values[key] = value
	s.backend.sets++
	return true, nil
}

func newCodexSessionIdentitySharedInstance(backend *codexSessionIdentitySharedBackend) *OpenAIGatewayService {
	return &OpenAIGatewayService{cache: &codexSessionIdentitySharedStore{backend: backend}}
}

func (s *codexSessionIdentityV2Store) GetCodexSessionIdentity(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return "", ErrCodexSessionIdentityNotFound
	}
	return value, nil
}

func (s *codexSessionIdentityV2Store) SetCodexSessionIdentityIfAbsent(_ context.Context, key, value string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[key]; ok {
		return false, nil
	}
	s.values[key] = value
	s.sets++
	return true, nil
}

func newCodexSessionIdentityV2Context(t *testing.T, userID, apiKeyID int64) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: apiKeyID, UserID: userID})
	return c
}

func codexSessionIdentityV2Account(id string) *Account {
	return &Account{
		ID:          9001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": id},
	}
}

func TestCodexSessionIdentityV2MappingIsStableAndScoped(t *testing.T) {
	store := &codexSessionIdentityV2Store{values: make(map[string]string)}
	svc := &OpenAIGatewayService{cache: store}
	raw := uuid.Must(uuid.NewV7()).String()
	account := codexSessionIdentityV2Account("upstream-a")

	first, err := svc.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), account, raw)
	require.NoError(t, err)
	second, err := svc.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), account, raw)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.True(t, isCodexUUIDv7(first))
	require.NotEqual(t, raw, first)
	require.Equal(t, 1, store.sets)

	otherUser, err := svc.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityV2Context(t, 42, 51), account, raw)
	require.NoError(t, err)
	otherAccount, err := svc.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), codexSessionIdentityV2Account("upstream-b"), raw)
	require.NoError(t, err)
	require.NotEqual(t, first, otherUser)
	require.NotEqual(t, first, otherAccount)
}

func TestCodexSessionIdentityV2ConcurrentCreateUsesOneDurableValue(t *testing.T) {
	store := &codexSessionIdentityV2Store{values: make(map[string]string)}
	svc := &OpenAIGatewayService{cache: store}
	account := codexSessionIdentityV2Account("upstream-race")
	raw := uuid.Must(uuid.NewV7()).String()

	const callers = 16
	results := make(chan string, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mapped, err := svc.resolveCodexMappedSessionIdentity(
				context.Background(),
				newCodexSessionIdentityV2Context(t, 41, 51),
				account,
				raw,
			)
			if err != nil {
				errors <- err
				return
			}
			results <- mapped
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var want string
	for mapped := range results {
		if want == "" {
			want = mapped
		}
		require.Equal(t, want, mapped)
		require.True(t, isCodexUUIDv7(mapped))
	}
	require.Equal(t, 1, store.sets)
}

func TestCodexSessionIdentityV2SharedStoreAcrossIndependentInstances(t *testing.T) {
	backend := &codexSessionIdentitySharedBackend{values: make(map[string]string)}
	instanceA := newCodexSessionIdentitySharedInstance(backend)
	instanceB := newCodexSessionIdentitySharedInstance(backend)
	raw := uuid.Must(uuid.NewV7()).String()
	accountA := codexSessionIdentityV2Account("shared-upstream-a")
	accountB := codexSessionIdentityV2Account("shared-upstream-b")

	// A fresh request context on instance B must read the durable value created
	// by instance A; no per-service staging state is shared here.
	mappedA, err := instanceA.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), accountA, raw,
	)
	require.NoError(t, err)
	mappedB, err := instanceB.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), accountA, raw,
	)
	require.NoError(t, err)
	require.Equal(t, mappedA, mappedB)
	require.True(t, isCodexUUIDv7(mappedA))
	require.Equal(t, 1, backend.sets)

	// User and upstream credential namespaces stay isolated even when the raw
	// session identifier is identical and requests land on different instances.
	otherUser, err := instanceB.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 42, 51), accountA, raw,
	)
	require.NoError(t, err)
	otherAccount, err := instanceA.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), accountB, raw,
	)
	require.NoError(t, err)
	require.NotEqual(t, mappedA, otherUser)
	require.NotEqual(t, mappedA, otherAccount)
	require.Equal(t, 3, backend.sets)

	// A later failover back to account A on instance B recovers A's durable
	// mapping; switching to account B never reuses it.
	accountAAgain, err := instanceB.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), accountA, raw,
	)
	require.NoError(t, err)
	require.Equal(t, mappedA, accountAAgain)
}

func TestCodexSessionIdentityV2SharedRedisAcrossIndependentInstances(t *testing.T) {
	redisServer := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	instanceA := &OpenAIGatewayService{cache: &codexSessionIdentityRedisStore{client: clientA}}
	instanceB := &OpenAIGatewayService{cache: &codexSessionIdentityRedisStore{client: clientB}}
	raw := uuid.Must(uuid.NewV7()).String()
	account := codexSessionIdentityV2Account("shared-redis-upstream")

	mappedA, err := instanceA.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), account, raw,
	)
	require.NoError(t, err)
	mappedB, err := instanceB.resolveCodexMappedSessionIdentity(
		context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), account, raw,
	)
	require.NoError(t, err)
	require.Equal(t, mappedA, mappedB)
	require.True(t, isCodexUUIDv7(mappedA))
	require.Len(t, redisServer.Keys(), 1)

	// Exercise the real Redis SetNX path with independent clients racing to
	// create the same second session mapping.
	rawRace := uuid.Must(uuid.NewV7()).String()
	const callers = 16
	results := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(instance *OpenAIGatewayService) {
			defer wg.Done()
			mapped, resolveErr := instance.resolveCodexMappedSessionIdentity(
				context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), account, rawRace,
			)
			if resolveErr != nil {
				errs <- resolveErr
				return
			}
			results <- mapped
		}(map[bool]*OpenAIGatewayService{true: instanceA, false: instanceB}[i%2 == 0])
	}
	wg.Wait()
	close(results)
	close(errs)
	for resolveErr := range errs {
		require.NoError(t, resolveErr)
	}
	var want string
	for mapped := range results {
		if want == "" {
			want = mapped
		}
		require.Equal(t, want, mapped)
		require.True(t, isCodexUUIDv7(mapped))
	}
	require.Len(t, redisServer.Keys(), 2)
}

func TestCodexSessionIdentityV2SharedStoreConcurrentCreateAcrossInstances(t *testing.T) {
	backend := &codexSessionIdentitySharedBackend{values: make(map[string]string)}
	instances := []*OpenAIGatewayService{
		newCodexSessionIdentitySharedInstance(backend),
		newCodexSessionIdentitySharedInstance(backend),
	}
	account := codexSessionIdentityV2Account("shared-upstream-race")
	raw := uuid.Must(uuid.NewV7()).String()

	const callers = 32
	results := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(instance *OpenAIGatewayService) {
			defer wg.Done()
			mapped, err := instance.resolveCodexMappedSessionIdentity(
				context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), account, raw,
			)
			if err != nil {
				errs <- err
				return
			}
			results <- mapped
		}(instances[i%len(instances)])
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var want string
	for mapped := range results {
		if want == "" {
			want = mapped
		}
		require.Equal(t, want, mapped)
		require.True(t, isCodexUUIDv7(mapped))
	}
	require.Equal(t, 1, backend.sets)
}

func TestCodexSessionIdentityLegacyRollbackPreservesUUIDv7(t *testing.T) {
	store := &codexSessionIdentityV2Store{values: make(map[string]string)}
	svc := &OpenAIGatewayService{cache: store}
	svc.cfg = &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: CodexSessionIdentityMappingLegacy}}
	raw := uuid.Must(uuid.NewV7()).String()

	mapped, err := svc.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityV2Context(t, 41, 51), codexSessionIdentityV2Account("rollback"), raw)
	require.NoError(t, err)
	require.Equal(t, raw, mapped)
	require.Equal(t, 0, store.sets)
}

func TestCodexSessionIdentityV2FinalHTTPCarriersUseOneMappedSession(t *testing.T) {
	store := &codexSessionIdentityV2Store{values: make(map[string]string)}
	svc := &OpenAIGatewayService{cache: store}
	account := codexSessionIdentityV2Account("upstream-http")
	raw := uuid.Must(uuid.NewV7()).String()
	c := newCodexSessionIdentityV2Context(t, 41, 51)
	c.Request.Header.Set("session-id", raw)
	body := []byte(`{"model":"gpt-5.6-codex","stream":true,"prompt_cache_key":"` + raw + `","client_metadata":{"session_id":"` + raw + `","x-codex-turn-metadata":"{\"session_id\":\"` + raw + `\"}"}}`)

	req, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "token", true, raw, true)
	require.NoError(t, err)
	mapped := req.Header.Get("session-id")
	require.True(t, isCodexUUIDv7(mapped))
	require.Equal(t, mapped, req.Header.Get("session_id"))
	upstreamBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, mapped, gjson.GetBytes(upstreamBody, "prompt_cache_key").String())
	require.Equal(t, mapped, gjson.GetBytes(upstreamBody, "client_metadata.session_id").String())
	nested := gjson.Parse(gjson.GetBytes(upstreamBody, "client_metadata.x-codex-turn-metadata").String())
	require.Equal(t, mapped, nested.Get("session_id").String())
}

func TestCodexSessionIdentityV2FinalPassthroughAndWSHeadersUseOneMappedSession(t *testing.T) {
	store := &codexSessionIdentityV2Store{values: make(map[string]string)}
	svc := &OpenAIGatewayService{cache: store}
	account := codexSessionIdentityV2Account("upstream-passthrough-ws")
	raw := uuid.Must(uuid.NewV7()).String()
	c := newCodexSessionIdentityV2Context(t, 41, 51)
	c.Request.Header.Set("session-id", raw)
	c.Request.Header.Set("thread-id", uuid.Must(uuid.NewV7()).String())
	body := []byte(`{"model":"gpt-5.6-codex","stream":true,"prompt_cache_key":"` + raw + `","client_metadata":{"session_id":"` + raw + `"}}`)

	passthrough, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "token")
	require.NoError(t, err)
	mapped := passthrough.Header.Get("session-id")
	require.True(t, isCodexUUIDv7(mapped))
	require.Equal(t, mapped, passthrough.Header.Get("session_id"))

	wsHeaders, _, err := svc.buildOpenAIWSHeaders(
		context.Background(), c, account, "token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true, "", "", raw, "gpt-5.6-codex", "",
	)
	require.NoError(t, err)
	_, identity, changed, err := normalizeCodexOutboundIdentityRawWithSessionMapper(
		wsHeaders,
		body,
		raw,
		func(value string) (string, error) {
			return svc.resolveCodexMappedSessionIdentity(context.Background(), c, account, value)
		},
	)
	require.NoError(t, err)
	require.True(t, changed)
	applyCodexOutboundIdentityToHeaders(wsHeaders, identity)
	require.Equal(t, mapped, wsHeaders.Get("session-id"))
	require.Equal(t, mapped, wsHeaders.Get("session_id"))
}

func TestCodexSessionIdentityV2ParentReferenceSurvivesFinalFingerprintChain(t *testing.T) {
	rawSession := uuid.Must(uuid.NewV7()).String()
	rawParentThread := uuid.Must(uuid.NewV7()).String()
	for _, mode := range []codexFingerprintMode{codexFingerprintOff, codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull} {
		t.Run(string(mode), func(t *testing.T) {
			store := &codexSessionIdentityV2Store{values: make(map[string]string)}
			svc := &OpenAIGatewayService{cache: store}
			account := codexSessionIdentityV2Account("upstream-parent-" + string(mode))
			account.Extra = map[string]any{
				codexFingerprintModeExtraKey: string(mode),
				codexFingerprintSeedExtraKey: uuid.Must(uuid.NewV7()).String(),
			}

			build := func(ctx *gin.Context, threadID, parentID string) (http.Header, []byte) {
				headers := http.Header{"session-id": []string{rawSession}, "thread-id": []string{threadID}}
				if parentID != "" {
					headers.Set("x-codex-parent-thread-id", parentID)
				}
				body := []byte(`{"client_metadata":{"session_id":"` + rawSession + `","thread_id":"` + threadID + `"`)
				if parentID != "" {
					body = append(body, []byte(`,"x-codex-parent-thread-id":"`+parentID+`"}}`)...)
				} else {
					body = append(body, []byte(`}}`)...)
				}
				applyCodexAccountIdentityHeaders(headers, account, 51)
				metadata := map[string]any{}
				require.NoError(t, json.Unmarshal(body, &metadata))
				ids := resolveCodexFingerprintIDs(account, rawSession, mode)
				applyCodexFingerprintClientMetadata(metadata, ids)
				applyCodexFingerprintHeaders(headers, ids)
				normalized, identity, changed, err := normalizeCodexOutboundIdentityRawWithSessionMapper(
					headers,
					mustJSONForSessionIdentityTest(t, metadata),
					rawSession,
					func(raw string) (string, error) {
						return svc.resolveCodexMappedSessionIdentity(context.Background(), ctx, account, raw)
					},
				)
				require.NoError(t, err)
				require.True(t, changed)
				applyCodexOutboundIdentityToHeaders(headers, identity)
				return headers, normalized
			}

			parentCtx := newCodexSessionIdentityV2Context(t, 41, 51)
			childCtx := newCodexSessionIdentityV2Context(t, 41, 51)
			parentHeaders, _ := build(parentCtx, rawParentThread, "")
			childHeaders, _ := build(childCtx, uuid.Must(uuid.NewV7()).String(), rawParentThread)
			require.Equal(t, parentHeaders.Get("thread-id"), childHeaders.Get("x-codex-parent-thread-id"))
		})
	}
}

func mustJSONForSessionIdentityTest(t *testing.T, value map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}
