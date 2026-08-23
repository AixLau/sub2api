package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAIGatewayPipelineCheckCyberSessionBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true},
	}
	pipeline := newOpenAIGatewayPipeline(nil)
	pipeline.cyberSessionChecker = checker

	result := pipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})

	require.NotNil(t, result)
	require.True(t, result.Blocked)
	require.Equal(t, blockKey, result.BlockKey)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Contains(t, w.Body.String(), cyberSessionBlockedClientMessage(service.PlatformOpenAI))
	require.Equal(t, []string{blockKey}, checker.checkedKeys)
}

func TestOpenAIGatewayPipelineCheckCyberSessionAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","messages":[{"role":"user","content":"hello"}]}`)
	apiKey := &service.APIKey{ID: 7}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/chat/completions", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: false},
	}
	pipeline := newOpenAIGatewayPipeline(nil)
	pipeline.cyberSessionChecker = checker

	result := pipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatChat,
	})

	require.NotNil(t, result)
	require.False(t, result.Blocked)
	require.Empty(t, result.BlockKey)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, []string{blockKey}, checker.checkedKeys)
}

func TestOpenAIGatewayPipelineCheckCyberSessionExplicitBlockKeyStable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7}
	c1, _ := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	c2, _ := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c1, body)
	otherBlockKey := service.CyberSessionExplicitBlockKey(8, c1, body)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true, otherBlockKey: true},
	}
	pipeline := newOpenAIGatewayPipeline(nil)
	pipeline.cyberSessionChecker = checker

	result1 := pipeline.CheckCyberSession(c1, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})
	result2 := pipeline.CheckCyberSession(c2, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: apiKey,
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})

	require.NotEmpty(t, result1.BlockKey)
	require.Equal(t, result1.BlockKey, result2.BlockKey)
	require.Equal(t, blockKey, result1.BlockKey)

	otherAPIKeyResult := pipeline.CheckCyberSession(c1, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey: &service.APIKey{ID: 8},
		Model:  "gpt-5.1",
		Body:   body,
		Format: cyberBlockFormatResponses,
	})
	require.NotEqual(t, result1.BlockKey, otherAPIKeyResult.BlockKey)
}

func TestOpenAIGatewayPipelineCheckCyberSessionNilFallbacksDoNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)

	var nilPipeline *OpenAIGatewayPipeline
	var result *OpenAICyberStageResult
	require.NotPanics(t, func() {
		result = nilPipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
			APIKey: apiKey,
			Model:  "gpt-5.1",
			Body:   body,
			Format: cyberBlockFormatResponses,
		})
	})
	require.NotNil(t, result)
	require.False(t, result.Blocked)
	require.Equal(t, http.StatusOK, w.Code)

	pipeline := newOpenAIGatewayPipeline(nil)
	require.NotPanics(t, func() {
		result = pipeline.CheckCyberSession(c, zap.NewNop(), openAIGatewayCyberSessionInput{
			APIKey: nil,
			Model:  "gpt-5.1",
			Body:   body,
			Format: cyberBlockFormatResponses,
		})
	})
	require.NotNil(t, result)
	require.False(t, result.Blocked)
}

func TestOpenAIGatewayHandlerCheckCyberSessionWithPipelineEnqueuesOpsEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetOpsErrorLoggerStateForTest(t)
	t.Cleanup(func() {
		resetOpsErrorLoggerStateForTest(t)
	})
	opsErrorLogOnce.Do(func() {})

	opsErrorLogMu.Lock()
	opsErrorLogQueue = make(chan opsErrorLogJob, 1)
	opsErrorLogMu.Unlock()

	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"hello"}`)
	apiKey := &service.APIKey{ID: 7, Name: "gateway-key", Key: "sk-test-cyber-pipeline"}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true},
	}
	h := &OpenAIGatewayHandler{
		pipeline:   &OpenAIGatewayPipeline{cyberSessionChecker: checker},
		opsService: service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}

	blocked := h.checkCyberSessionWithPipeline(c, zap.NewNop(), openAIGatewayCyberSessionInput{
		APIKey:   apiKey,
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model:    "gpt-5.1",
		Body:     body,
		Format:   cyberBlockFormatResponses,
	})

	require.True(t, blocked)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, int64(1), OpsErrorLogEnqueuedTotal())

	opsErrorLogMu.RLock()
	queue := opsErrorLogQueue
	opsErrorLogMu.RUnlock()
	require.Len(t, queue, 1)
	job := <-queue
	require.NotNil(t, job.entry)
	require.Equal(t, "cyber_policy_session_blocked", job.entry.ErrorType)
	require.Equal(t, "session_block_key="+blockKey, job.entry.ErrorBody)
}

func TestOpenAIGatewayHTTPPipelineCyberSessionBlockRecordsRiskAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-session","input":"blocked retry"}`)
	groupID := int64(2)
	apiKey := &service.APIKey{
		ID:      7,
		Name:    "gateway-key",
		GroupID: &groupID,
		User:    &service.User{ID: 11, Email: "risk@example.com"},
		Group:   &service.Group{ID: groupID, Name: "Codex"},
	}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	c.Writer.Header().Set("X-Request-Id", "req-session-blocked")
	blockKey := service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true},
	}
	repo := &openAIGatewayPipelineRiskAuditRepo{}
	cfg, err := json.Marshal(&service.ContentModerationConfig{})
	require.NoError(t, err)
	moderationSvc := service.NewContentModerationService(
		&openAIGatewayPipelineRiskAuditSettingRepo{values: map[string]string{
			service.SettingKeyRiskControlEnabled:      "true",
			service.SettingKeyContentModerationConfig: string(cfg),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	moderationSvc.SetRawRequestSnapshotStore(repo, openAIGatewayPipelineRiskAuditEncryptor{})
	h := &OpenAIGatewayHandler{
		pipeline:                 &OpenAIGatewayPipeline{cyberSessionChecker: checker, httpPreForwardStages: []openAIHTTPGatewayStage{OpenAIHTTPCyberStage{}}},
		contentModerationService: moderationSvc,
	}

	result := h.pipeline.RunHTTPPreForward(h, c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{
		APIKey:      apiKey,
		Protocol:    service.ContentModerationProtocolOpenAIResponses,
		Model:       "gpt-5.1",
		Body:        body,
		CyberBody:   body,
		CyberFormat: cyberBlockFormatResponses,
	})

	require.True(t, result.Blocked)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Eventually(t, func() bool {
		return len(repo.logsSnapshot()) == 1
	}, time.Second, 10*time.Millisecond)
	logs := repo.logsSnapshot()
	require.Equal(t, service.ContentModerationActionCyberPolicySessionBlocked, logs[0].Action)
	require.Equal(t, "req-session-blocked", logs[0].RequestID)
	require.True(t, logs[0].RawRequestAvailable)
	require.Equal(t, len(body), logs[0].RawRequestBytes)
	require.Equal(t, "risk@example.com", logs[0].UserEmail)

	raw, err := moderationSvc.GetRawRequestSnapshot(context.Background(), logs[0].ID)
	require.NoError(t, err)
	require.Equal(t, string(body), raw.Body)
}

func newOpenAIGatewayPipelineCyberContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(string(body)))
	return c, w
}

type openAIGatewayPipelineCyberCheckerStub struct {
	enabled     bool
	blocked     map[string]bool
	checkedKeys []string
}

func (s *openAIGatewayPipelineCyberCheckerStub) FindCyberSessionBlockedForRequest(_ context.Context, apiKeyID int64, c *gin.Context, body []byte, _, _ string) string {
	key := service.CyberSessionExplicitBlockKey(apiKeyID, c, body)
	s.checkedKeys = append(s.checkedKeys, key)
	if s.enabled && s.blocked[key] {
		return key
	}
	return ""
}

type openAIGatewayPipelineRiskAuditSettingRepo struct {
	values map[string]string
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) Get(ctx context.Context, key string) (*service.Setting, error) {
	if value, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) Set(ctx context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *openAIGatewayPipelineRiskAuditSettingRepo) Delete(ctx context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type openAIGatewayPipelineRiskAuditRepo struct {
	mu   sync.Mutex
	logs []service.ContentModerationLog
	raw  map[int64]service.ContentModerationRawRequestSnapshot
	next int64
}

func (r *openAIGatewayPipelineRiskAuditRepo) CreateLog(ctx context.Context, log *service.ContentModerationLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	log.ID = r.next
	r.logs = append(r.logs, *log)
	return nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) ListLogs(ctx context.Context, filter service.ContentModerationLogFilter) ([]service.ContentModerationLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	return 0, nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*service.ContentModerationCleanupResult, error) {
	return &service.ContentModerationCleanupResult{}, nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error {
	return nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) UpdateLogViolationCountByDecisionID(ctx context.Context, decisionID string, count int) error {
	return nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) UpdateLogAccountActionByDecisionID(ctx context.Context, decisionID string, violationCount int, autoBanned bool) error {
	return nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) UpdateLogEmailSentByDecisionID(ctx context.Context, decisionID string, sent bool) error {
	return nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) ReviewLog(ctx context.Context, id int64, input service.ContentModerationLogReviewInput) (*service.ContentModerationLog, error) {
	return &service.ContentModerationLog{ID: id, ReviewStatus: input.Status, ReviewNote: input.Note}, nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) CreateRawRequestSnapshot(ctx context.Context, snapshot *service.ContentModerationRawRequestSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.raw == nil {
		r.raw = map[int64]service.ContentModerationRawRequestSnapshot{}
	}
	cp := *snapshot
	r.raw[cp.LogID] = cp
	for idx := range r.logs {
		if r.logs[idx].ID == cp.LogID {
			r.logs[idx].RawRequestAvailable = true
			r.logs[idx].RawRequestBytes = cp.BodyBytes
			r.logs[idx].RawRequestTruncated = cp.Truncated
		}
	}
	return nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) GetRawRequestSnapshotByLogID(ctx context.Context, logID int64) (*service.ContentModerationRawRequestSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, ok := r.raw[logID]
	if !ok {
		return nil, service.ErrSettingNotFound
	}
	return &snapshot, nil
}

func (r *openAIGatewayPipelineRiskAuditRepo) logsSnapshot() []service.ContentModerationLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]service.ContentModerationLog, len(r.logs))
	copy(out, r.logs)
	return out
}

type openAIGatewayPipelineRiskAuditEncryptor struct{}

func (openAIGatewayPipelineRiskAuditEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (openAIGatewayPipelineRiskAuditEncryptor) Decrypt(ciphertext string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, "enc:"))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
