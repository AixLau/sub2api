package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexBlacklistSettingsRepo struct {
	codexPolicyMigrationRepoStub
	readErr error
}

func (r *codexBlacklistSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == SettingKeyCodexCLIOnlyUserBlacklist && r.readErr != nil {
		return "", r.readErr
	}
	return r.codexPolicyMigrationRepoStub.GetValue(ctx, key)
}

func (r *codexBlacklistSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *codexBlacklistSettingsRepo) SetMultiple(_ context.Context, updates map[string]string) error {
	for key, value := range updates {
		r.values[key] = value
	}
	return nil
}

func (r *codexBlacklistSettingsRepo) GetAll(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func newCodexBlacklistSettings() (*SettingService, *codexBlacklistSettingsRepo) {
	repo := &codexBlacklistSettingsRepo{codexPolicyMigrationRepoStub: codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyCodexCLIOnlyUserBlacklist: "42",
		SettingKeyCodexCLIOnlyWhitelist:     `[{"originator":"trusted","ua_contains":["trusted/"],"skip_engine_fingerprint":true}]`,
	}}}
	return NewSettingService(repo, &config.Config{}), repo
}

func codexBlacklistContext(userID int64, ua, originator string) (*gin.Context, context.Context) {
	c := newCodexDetectorTestContext(ua, originator)
	c.Request.Header.Set("x-codex-session-id", "fingerprint")
	c.Set("api_key", &APIKey{ID: 10, UserID: userID})
	ctx := context.WithValue(c.Request.Context(), ctxkey.UserID, userID)
	ctx = WithCodexRestrictionRequest(ctx, c, []byte(`{"model":"gpt-5.1"}`))
	c.Request = c.Request.WithContext(ctx)
	return c, ctx
}

func codexBlacklistAccount() Account {
	return Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 0,
		Extra: map[string]any{"codex_cli_only": true, "openai_oauth_responses_websockets_v2_enabled": true}}
}

func TestCodexUserBlacklistPriority(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ua         string
		originator string
		force      bool
		appServer  bool
	}{
		{"official", "codex_cli_rs/0.146.0", "", false, false},
		{"whitelist", "trusted/1.0", "trusted", false, false},
		{"force", "curl/8.0", "", true, false},
		{"app_server", "third-party/1.0", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, _ := newCodexBlacklistSettings()
			policy := settings.GetCodexRestrictionPolicy(context.Background())
			policy.AllowAppServerClients = tc.appServer
			detector := NewOpenAICodexClientRestrictionDetector(&config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: tc.force}})
			account := codexBlacklistAccount()
			c, _ := codexBlacklistContext(42, tc.ua, tc.originator)
			result := detector.Detect(c, &account, policy, []byte(`{"user":99,"metadata":{"user_id":99}}`))
			require.Equal(t, CodexClientRestrictionReasonUserBlacklisted, result.Reason)
			require.False(t, result.Matched)
			c, _ = codexBlacklistContext(99, tc.ua, tc.originator)
			require.True(t, detector.Detect(c, &account, policy, nil).Matched)
			account.Extra = nil
			c, _ = codexBlacklistContext(42, tc.ua, tc.originator)
			require.False(t, detector.Detect(c, &account, policy, nil).Enabled)
		})
	}
	c, _ := codexBlacklistContext(99, "curl/8.0", "")
	account := codexBlacklistAccount()
	settings, _ := newCodexBlacklistSettings()
	require.False(t, NewOpenAICodexClientRestrictionDetector(nil).Detect(c, &account, settings.GetCodexRestrictionPolicy(context.Background()), nil).Matched)
}

func TestCodexUserBlacklistSettingsRoundTrip(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	settings, repo := newCodexBlacklistSettings()
	ctx := context.Background()
	require.Contains(t, settings.GetCodexRestrictionPolicy(ctx).DeniedUserIDs, int64(42))
	require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{CodexCLIOnlyUserBlacklist: "12, 34\n12"}))
	require.Equal(t, map[int64]struct{}{12: {}, 34: {}}, settings.GetCodexRestrictionPolicy(ctx).DeniedUserIDs)
	loaded, err := settings.GetAllSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "12, 34\n12", loaded.CodexCLIOnlyUserBlacklist)
	for _, invalid := range []string{"12, invalid", "0", "-1", "1.5", "9223372036854775808"} {
		require.Error(t, settings.UpdateSettings(ctx, &SystemSettings{CodexCLIOnlyUserBlacklist: invalid}))
		require.Equal(t, "12, 34\n12", repo.values[SettingKeyCodexCLIOnlyUserBlacklist])
	}
	require.NoError(t, settings.UpdateSettingsOmitting(ctx, &SystemSettings{}, OmittedSettingKeys{SettingKeyCodexCLIOnlyUserBlacklist: {}}))
	require.Contains(t, settings.GetCodexRestrictionPolicy(ctx).DeniedUserIDs, int64(12))
	require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{CodexCLIOnlyUserBlacklist: ""}))
	require.Empty(t, settings.GetCodexRestrictionPolicy(ctx).DeniedUserIDs)
}

func TestCodexUserBlacklistSettingsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		err  error
	}{
		{"database_failure", "42", errors.New("database unavailable")},
		{"corrupt_storage", "invalid", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, repo := newCodexBlacklistSettings()
			repo.values[SettingKeyCodexCLIOnlyUserBlacklist] = tc.raw
			repo.readErr = tc.err
			account := codexBlacklistAccount()
			c, _ := codexBlacklistContext(42, "codex_cli_rs/0.146.0", "")
			policy := settings.GetCodexRestrictionPolicy(c.Request.Context())
			result := NewOpenAICodexClientRestrictionDetector(&config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: true}}).Detect(c, &account, policy, nil)
			require.Equal(t, CodexClientRestrictionReasonPolicyUnavailable, result.Reason)
			require.False(t, result.Matched)
		})
	}
}

func TestCodexUserBlacklistScheduling(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, identity := range []string{"official", "whitelist", "no_gin_context"} {
			t.Run(identity+map[bool]string{false: "/legacy", true: "/advanced"}[advanced], func(t *testing.T) {
				settings, _ := newCodexBlacklistSettings()
				restricted := codexBlacklistAccount()
				unrestricted := restricted
				unrestricted.ID, unrestricted.Priority, unrestricted.Extra = 2, 1, nil
				cache := &stubGatewayCache{}
				concurrency := schedulerTestConcurrencyCache{acquiredIDs: &[]int64{}}
				svc := &OpenAIGatewayService{settingService: settings, accountRepo: stubOpenAIAccountRepo{accounts: []Account{restricted, unrestricted}},
					cache: cache, concurrencyService: NewConcurrencyService(concurrency), cfg: newOpenAIWSV2TestConfig()}
				ua, originator := "codex_cli_rs/0.146.0", ""
				if identity == "whitelist" {
					ua, originator = "trusted/1.0", "trusted"
				}
				_, ctx := codexBlacklistContext(42, ua, originator)
				if identity == "no_gin_context" {
					ctx = context.WithValue(context.Background(), ctxkey.UserID, int64(42))
				}
				require.NoError(t, svc.BindStickySession(ctx, nil, "sticky", restricted.ID))
				var selection *AccountSelectionResult
				var err error
				if advanced {
					scheduler := newDefaultOpenAIAccountScheduler(svc, newOpenAIAccountRuntimeStats())
					selection, _, err = scheduler.Select(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "sticky", RequestedModel: "gpt-5.1"})
				} else {
					selection, err = svc.SelectAccountWithLoadAwareness(ctx, nil, "sticky", "gpt-5.1", nil, 42)
				}
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, unrestricted.ID, selection.Account.ID)
				require.NotContains(t, *concurrency.acquiredIDs, restricted.ID)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				allowed, reason := svc.codexAccountAllowedForScheduling(ctx, &restricted)
				require.False(t, allowed)
				require.Equal(t, CodexClientRestrictionReasonUserBlacklisted, reason)
				gateway := &GatewayService{settingService: settings, accountRepo: svc.accountRepo}
				require.False(t, gateway.isAccountSchedulableForModelSelection(ctx, &restricted, "gpt-5.1"))
				require.True(t, gateway.isAccountSchedulableForModelSelection(ctx, &unrestricted, "gpt-5.1"))
				store := svc.getOpenAIWSStateStore()
				require.NoError(t, store.BindResponseAccount(ctx, 0, "resp_blacklisted", restricted.ID, time.Hour))
				selection, err = svc.SelectAccountByPreviousResponseID(ctx, nil, "resp_blacklisted", "gpt-5.1", nil, false)
				require.NoError(t, err)
				require.Nil(t, selection)
				selection, err = svc.SelectAccountWithLoadAwareness(ctx, nil, "", "gpt-5.1", map[int64]struct{}{2: {}}, 42)
				require.Nil(t, selection)
				require.Error(t, err)
			})
		}
	}
}

func TestCodexUserBlacklistForwardingBoundaries(t *testing.T) {
	for name, forward := range map[string]func(*OpenAIGatewayService, context.Context, *gin.Context, *Account, []byte) error{
		"responses": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			_, err := s.Forward(ctx, c, a, b)
			return err
		},
		"chat": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			_, err := s.ForwardAsChatCompletions(ctx, c, a, b, "", "")
			return err
		},
		"messages": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			_, err := s.ForwardAsAnthropic(ctx, c, a, b, "", "")
			return err
		},
		"images": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			_, err := s.ForwardImages(ctx, c, a, b, &OpenAIImagesRequest{}, "")
			return err
		},
		"search": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			_, err := s.ForwardAlphaSearch(ctx, c, a, b)
			return err
		},
		"embeddings": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			_, err := s.ForwardEmbeddings(ctx, c, a, b, "")
			return err
		},
		"count_tokens": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			return s.ForwardCountTokensAsAnthropic(ctx, c, a, b, "")
		},
		"input_tokens": func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, a *Account, b []byte) error {
			return s.ForwardResponsesInputTokens(ctx, c, a, b)
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, ua := range []string{"codex_cli_rs/0.146.0", "trusted/1.0"} {
				settings, _ := newCodexBlacklistSettings()
				svc := &OpenAIGatewayService{settingService: settings}
				account := codexBlacklistAccount()
				c, ctx := codexBlacklistContext(42, ua, "trusted")
				err := forward(svc, ctx, c, &account, []byte(`{"model":"gpt-5.1","messages":[],"input":[]}`))
				require.Error(t, err)
				require.Equal(t, http.StatusForbidden, c.Writer.Status())
			}
		})
	}
}

func TestCodexUserBlacklistWebSocketBoundary(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	settings, _ := newCodexBlacklistSettings()
	account := codexBlacklistAccount()
	c, ctx := codexBlacklistContext(42, "codex_cli_rs/0.146.0", "")
	svc := &OpenAIGatewayService{settingService: settings}
	err := svc.ProxyResponsesWebSocketFromClient(ctx, c, &coderws.Conn{}, &account, "", []byte(`{"type":"response.create"}`), nil)
	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
	// A newly saved blacklist also applies to the next turn of an admitted socket.
	c, ctx = codexBlacklistContext(99, "codex_cli_rs/0.146.0", "")
	require.NoError(t, svc.codexWebSocketRestrictionError(ctx, c, &account, nil))
	require.NoError(t, settings.UpdateSettings(ctx, &SystemSettings{CodexCLIOnlyUserBlacklist: "99"}))
	require.Error(t, svc.codexWebSocketRestrictionError(ctx, c, &account, nil))
}

func TestCodexUserBlacklistWebSocketRejectsNextTurn(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.WithValue(context.Background(), ctxkey.UserID, int64(99)))
	defer cancel(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_blacklist_1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.ForceCodexCLI = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	account := codexBlacklistAccount()
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
	svc := newPassthroughLifecycleService(cfg, upstream)
	settings, _ := newCodexBlacklistSettings()
	svc.settingService = settings
	server, serverErr := startPassthroughHookRecordingServer(t, ctx, svc, &account, nil)
	defer server.Close()
	conn := dialPassthroughLifecycleClient(t, server)
	defer conn.CloseNow()
	requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
	_, err := readPassthroughLifecycleFrame(t, conn, 3*time.Second)
	require.NoError(t, err)

	settings.codexRestrictionPolicyCache.Store(&cachedCodexRestrictionPolicy{
		value:     CodexRestrictionPolicy{DeniedUserIDs: map[int64]struct{}{99: {}}},
		expiresAt: time.Now().Add(time.Minute).UnixNano(),
	})
	writeCtx, cancelWrite := context.WithTimeout(ctx, time.Second)
	defer cancelWrite()
	require.NoError(t, conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`)))
	// Read the policy close so the client acknowledges the closing handshake.
	_, err = readPassthroughLifecycleFrame(t, conn, 3*time.Second)
	require.Error(t, err)
	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
	case <-time.After(5 * time.Second):
		t.Fatal("blacklisted WebSocket turn was not rejected")
	}
	select {
	case payload := <-upstream.writes:
		t.Fatalf("blacklisted turn reached upstream: %s", payload)
	default:
	}
}

func TestCodexUserBlacklistLiveAndModelsBoundaries(t *testing.T) {
	settings, _ := newCodexBlacklistSettings()
	account := codexBlacklistAccount()
	_, ctx := codexBlacklistContext(42, "codex_cli_rs/0.146.0", "")
	svc := &OpenAIGatewayService{settingService: settings, accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
	_, err := svc.createUpstreamLiveCall(ctx, &account, &LiveCallRequest{}, "")
	require.ErrorIs(t, err, ErrNoAllowedCodexAccounts)
	_, err = svc.FetchCodexModelsManifest(ctx, &account, "", "")
	require.Error(t, err)
	_, err = svc.FetchOpenAIModelsList(ctx, &account)
	require.ErrorIs(t, err, ErrNoAllowedCodexAccounts)
	record := &LiveCallRecord{CallID: "blocked_live", CallHash: hashLiveCallID("blocked_live"), AccountID: account.ID, UserID: 42, APIKeyID: 10}
	store := &liveTestStore{}
	require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
	svc.cache = store
	_, err = svc.GetLiveCallForIdentity(ctx, record.CallID, LiveCallIdentity{UserID: 42, APIKeyID: 10})
	require.ErrorIs(t, err, ErrNoAllowedCodexAccounts)
}
