package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type clientSetupAPIKeyCreatorStub struct {
	lastUserID      int64
	lastReq         service.CreateAPIKeyRequest
	created         *service.APIKey
	err             error
	availableGroups []service.Group
}

type clientSetupMemoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *clientSetupAPIKeyCreatorStub) Create(ctx context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error) {
	s.lastUserID = userID
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	if s.created != nil {
		return s.created, nil
	}
	return &service.APIKey{
		ID:        42,
		UserID:    userID,
		Key:       "sk-client-setup-test",
		Name:      req.Name,
		Status:    service.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (s *clientSetupAPIKeyCreatorStub) GetAvailableGroups(ctx context.Context, userID int64) ([]service.Group, error) {
	return s.availableGroups, nil
}

func (s *clientSetupMemoryStore) Get(ctx context.Context, key string) *redis.StringCmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := redis.NewStringCmd(ctx, "get", key)
	value, ok := s.values[key]
	if !ok {
		cmd.SetErr(redis.Nil)
		return cmd
	}
	cmd.SetVal(value)
	return cmd
}

func (s *clientSetupMemoryStore) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := redis.NewStatusCmd(ctx, "set", key)
	switch v := value.(type) {
	case []byte:
		s.values[key] = string(v)
	case string:
		s.values[key] = v
	default:
		raw, _ := json.Marshal(v)
		s.values[key] = string(raw)
	}
	cmd.SetVal("OK")
	return cmd
}

func newClientSetupTestStore() *clientSetupMemoryStore {
	return &clientSetupMemoryStore{values: map[string]string{}}
}

func newClientSetupTestRouter(t *testing.T, store clientSetupStore, creator *clientSetupAPIKeyCreatorStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewClientSetupHandler(store, creator)

	v1 := router.Group("/api/v1")
	clientSetup := v1.Group("/client-setup")
	clientSetup.POST("/sessions", h.CreateSession)
	clientSetup.GET("/sessions/:setup_id", h.GetSession)
	clientSetup.POST("/exchange", h.Exchange)
	authenticated := clientSetup.Group("")
	authenticated.Use(gin.HandlerFunc(middleware2.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 123})
		c.Next()
	})))
	authenticated.POST("/sessions/:setup_id/approve", h.ApproveSession)
	return router
}

func postJSON(t *testing.T, router *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func responseData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	data, ok := envelope["data"].(map[string]any)
	require.True(t, ok, "response data should be object: %s", w.Body.String())
	return data
}

func TestClientSetupCreateApproveExchangeOnce(t *testing.T) {
	creator := &clientSetupAPIKeyCreatorStub{
		availableGroups: []service.Group{
			{
				ID:               13,
				Name:             "Codex 高速",
				Platform:         service.PlatformOpenAI,
				SubscriptionType: service.SubscriptionTypeStandard,
				RateMultiplier:   1.3,
				Status:           service.StatusActive,
			},
		},
	}
	router := newClientSetupTestRouter(t, newClientSetupTestStore(), creator)

	create := postJSON(t, router, "/api/v1/client-setup/sessions", map[string]any{
		"client":       "codex",
		"redirect_uri": "http://127.0.0.1:38291/callback",
	})
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	createData := responseData(t, create)
	setupID := createData["setup_id"].(string)
	deviceCode := createData["device_code"].(string)
	pollToken := createData["poll_token"].(string)
	require.NotEmpty(t, setupID)
	require.NotEmpty(t, deviceCode)
	require.NotEmpty(t, pollToken)
	require.Contains(t, createData["verify_url"].(string), "/client-setup")

	pending := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/client-setup/sessions/"+setupID, nil)
	router.ServeHTTP(pending, req)
	require.Equal(t, http.StatusOK, pending.Code, pending.Body.String())
	require.Equal(t, "pending", responseData(t, pending)["status"])

	approve := postJSON(t, router, "/api/v1/client-setup/sessions/"+setupID+"/approve", map[string]any{
		"device_code": deviceCode,
		"client":      "codex",
	})
	require.Equal(t, http.StatusOK, approve.Code, approve.Body.String())
	approveData := responseData(t, approve)
	require.Equal(t, "approved", approveData["status"])
	setupToken := approveData["setup_token"].(string)
	require.NotEmpty(t, setupToken)
	require.Equal(t, int64(123), creator.lastUserID)
	require.Contains(t, creator.lastReq.Name, "codex")

	publicApproved := httptest.NewRecorder()
	publicReq := httptest.NewRequest(http.MethodGet, "/api/v1/client-setup/sessions/"+setupID, nil)
	router.ServeHTTP(publicApproved, publicReq)
	require.Equal(t, http.StatusOK, publicApproved.Code, publicApproved.Body.String())
	require.NotContains(t, publicApproved.Body.String(), setupToken)

	polledApproved := httptest.NewRecorder()
	polledReq := httptest.NewRequest(http.MethodGet, "/api/v1/client-setup/sessions/"+setupID+"?poll_token="+pollToken, nil)
	router.ServeHTTP(polledApproved, polledReq)
	require.Equal(t, http.StatusOK, polledApproved.Code, polledApproved.Body.String())
	require.Contains(t, polledApproved.Body.String(), setupToken)

	exchange := postJSON(t, router, "/api/v1/client-setup/exchange", map[string]any{
		"setup_id":    setupID,
		"setup_token": setupToken,
	})
	require.Equal(t, http.StatusOK, exchange.Code, exchange.Body.String())
	exchangeData := responseData(t, exchange)
	require.Equal(t, "sk-client-setup-test", exchangeData["api_key"])
	require.Equal(t, "codex", exchangeData["client"])

	again := postJSON(t, router, "/api/v1/client-setup/exchange", map[string]any{
		"setup_id":    setupID,
		"setup_token": setupToken,
	})
	require.Equal(t, http.StatusGone, again.Code, again.Body.String())
}

func TestClientSetupApproveBindsCodexSubscriptionGroupBeforeDefaultHighSpeedGroup(t *testing.T) {
	creator := &clientSetupAPIKeyCreatorStub{
		availableGroups: []service.Group{
			{
				ID:               2,
				Name:             "Codex 高速",
				Platform:         service.PlatformOpenAI,
				SubscriptionType: service.SubscriptionTypeStandard,
				RateMultiplier:   1.3,
				Status:           service.StatusActive,
			},
			{
				ID:               22,
				Name:             "Codex 订阅",
				Platform:         service.PlatformOpenAI,
				SubscriptionType: service.SubscriptionTypeSubscription,
				RateMultiplier:   1,
				Status:           service.StatusActive,
			},
		},
	}
	router := newClientSetupTestRouter(t, newClientSetupTestStore(), creator)

	create := postJSON(t, router, "/api/v1/client-setup/sessions", map[string]any{
		"client": "codex",
	})
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	createData := responseData(t, create)

	approve := postJSON(t, router, "/api/v1/client-setup/sessions/"+createData["setup_id"].(string)+"/approve", map[string]any{
		"device_code": createData["device_code"].(string),
		"client":      "codex",
	})
	require.Equal(t, http.StatusOK, approve.Code, approve.Body.String())
	require.NotNil(t, creator.lastReq.GroupID)
	require.Equal(t, int64(22), *creator.lastReq.GroupID)
}

func TestClientSetupApproveBindsCodexDefaultHighSpeedGroupWhenNoSubscription(t *testing.T) {
	creator := &clientSetupAPIKeyCreatorStub{
		availableGroups: []service.Group{
			{
				ID:               9,
				Name:             "OpenAI 普通",
				Platform:         service.PlatformOpenAI,
				SubscriptionType: service.SubscriptionTypeStandard,
				RateMultiplier:   1,
				IsExclusive:      false,
				Status:           service.StatusActive,
			},
			{
				ID:               2,
				Name:             "Codex 高速",
				Platform:         service.PlatformOpenAI,
				SubscriptionType: service.SubscriptionTypeStandard,
				RateMultiplier:   1.3,
				IsExclusive:      false,
				Status:           service.StatusActive,
			},
		},
	}
	router := newClientSetupTestRouter(t, newClientSetupTestStore(), creator)

	create := postJSON(t, router, "/api/v1/client-setup/sessions", map[string]any{
		"client": "codex",
	})
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	createData := responseData(t, create)

	approve := postJSON(t, router, "/api/v1/client-setup/sessions/"+createData["setup_id"].(string)+"/approve", map[string]any{
		"device_code": createData["device_code"].(string),
		"client":      "codex",
	})
	require.Equal(t, http.StatusOK, approve.Code, approve.Body.String())
	require.NotNil(t, creator.lastReq.GroupID)
	require.Equal(t, int64(2), *creator.lastReq.GroupID)
}

func TestClientSetupApproveBindsClaudeAnthropicGroup(t *testing.T) {
	creator := &clientSetupAPIKeyCreatorStub{
		availableGroups: []service.Group{
			{
				ID:               8,
				Name:             "Antigravity",
				Platform:         service.PlatformAntigravity,
				SubscriptionType: service.SubscriptionTypeStandard,
				Status:           service.StatusActive,
			},
			{
				ID:               9,
				Name:             "Claude",
				Platform:         service.PlatformAnthropic,
				SubscriptionType: service.SubscriptionTypeStandard,
				Status:           service.StatusActive,
			},
		},
	}
	router := newClientSetupTestRouter(t, newClientSetupTestStore(), creator)

	create := postJSON(t, router, "/api/v1/client-setup/sessions", map[string]any{
		"client": "claude",
	})
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	createData := responseData(t, create)

	approve := postJSON(t, router, "/api/v1/client-setup/sessions/"+createData["setup_id"].(string)+"/approve", map[string]any{
		"device_code": createData["device_code"].(string),
		"client":      "claude",
	})
	require.Equal(t, http.StatusOK, approve.Code, approve.Body.String())
	require.NotNil(t, creator.lastReq.GroupID)
	require.Equal(t, int64(9), *creator.lastReq.GroupID)
}

func TestClientSetupApproveRejectsWrongDeviceCode(t *testing.T) {
	router := newClientSetupTestRouter(t, newClientSetupTestStore(), &clientSetupAPIKeyCreatorStub{})

	create := postJSON(t, router, "/api/v1/client-setup/sessions", map[string]any{
		"client": "claude",
	})
	require.Equal(t, http.StatusOK, create.Code, create.Body.String())
	setupID := responseData(t, create)["setup_id"].(string)

	approve := postJSON(t, router, "/api/v1/client-setup/sessions/"+setupID+"/approve", map[string]any{
		"device_code": "WRONG-CODE",
		"client":      "claude",
	})
	require.Equal(t, http.StatusForbidden, approve.Code, approve.Body.String())
}
