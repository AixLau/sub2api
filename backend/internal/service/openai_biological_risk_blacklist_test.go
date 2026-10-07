package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestAutoBlacklistCodexBiologicalRiskUser(t *testing.T) {
	settings, repo := newCodexBlacklistSettings()
	svc := &OpenAIGatewayService{settingService: settings}
	account := codexBlacklistAccount()
	c, ctx := codexBlacklistContext(42, "codex_cli_rs/0.146.0", "")

	svc.maybeBlacklistCodexBiologicalRiskUser(ctx, c, &account,
		"This content was flagged for possible biological risk. If this seems wrong, try rephrasing your request.", nil)
	require.Equal(t, "42", repo.values[SettingKeyCodexCLIOnlyUserBlacklist])
	require.Contains(t, settings.GetCodexRestrictionPolicy(ctx).DeniedUserIDs, int64(42))

	c, ctx = codexBlacklistContext(7, "codex_cli_rs/0.146.0", "")
	svc.maybeBlacklistCodexBiologicalRiskUser(ctx, c, &account, "CONTENT WAS FLAGGED FOR POSSIBLE BIOLOGICAL RISK", nil)
	require.Equal(t, "7,42", repo.values[SettingKeyCodexCLIOnlyUserBlacklist])
	svc.maybeBlacklistCodexBiologicalRiskUser(ctx, c, &account, "content was flagged for possible biological risk", nil)
	require.Equal(t, "7,42", repo.values[SettingKeyCodexCLIOnlyUserBlacklist])
}

func TestAutoBlacklistCodexBiologicalRiskTargetsAllOpenAIAccountTypes(t *testing.T) {
	settings, repo := newCodexBlacklistSettings()
	svc := &OpenAIGatewayService{settingService: settings}
	c, ctx := codexBlacklistContext(99, "codex_cli_rs/0.146.0", "")
	message := "This content was flagged for possible biological risk"

	ordinaryOAuth := codexBlacklistAccount()
	ordinaryOAuth.Extra = nil
	svc.maybeBlacklistCodexBiologicalRiskUser(ctx, c, &ordinaryOAuth, message, nil)
	require.Contains(t, repo.values[SettingKeyCodexCLIOnlyUserBlacklist], "99")

	c, ctx = codexBlacklistContext(100, "codex_cli_rs/0.146.0", "")
	apiKeyAccount := codexBlacklistAccount()
	apiKeyAccount.Type = AccountTypeAPIKey
	svc.maybeBlacklistCodexBiologicalRiskUser(ctx, c, &apiKeyAccount, message, nil)
	require.Contains(t, repo.values[SettingKeyCodexCLIOnlyUserBlacklist], "100")
}

func TestAddCodexCLIOnlyUserToBlacklistReadWriteErrors(t *testing.T) {
	settings, repo := newCodexBlacklistSettings()
	repo.readErr = errors.New("read failed")
	require.Error(t, settings.AddCodexCLIOnlyUserToBlacklist(context.Background(), 99))
	repo.readErr = nil
	repo.values[SettingKeyCodexCLIOnlyUserBlacklist] = "invalid"
	require.Error(t, settings.AddCodexCLIOnlyUserToBlacklist(context.Background(), 99))
	require.Error(t, settings.AddCodexCLIOnlyUserToBlacklist(context.Background(), 0))

	broken := &codexBlacklistSettingsRepo{codexPolicyMigrationRepoStub: codexPolicyMigrationRepoStub{values: map[string]string{}}, readErr: errors.New("read failed")}
	brokenSvc := NewSettingService(broken, &config.Config{})
	require.Error(t, brokenSvc.AddCodexCLIOnlyUserToBlacklist(context.Background(), 99))
}

func TestBiologicalRiskMessageMatcher(t *testing.T) {
	require.True(t, isOpenAIBiologicalRiskMessage("This content was flagged for possible biological risk"))
	require.False(t, isOpenAIBiologicalRiskMessage("This content was flagged for possible cyber risk"))
	account := codexBlacklistAccount()
	c := newCodexDetectorTestContext("codex_cli_rs/0.146.0", "")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.UserID, int64(42)))
	require.True(t, account.IsCodexCLIOnlyEnabled())
}

func TestBiologicalRiskUpstreamErrorAutoBlacklistsUser(t *testing.T) {
	settings, repo := newCodexBlacklistSettings()
	svc := &OpenAIGatewayService{settingService: settings}
	account := codexBlacklistAccount()
	c, ctx := codexBlacklistContext(99, "codex_cli_rs/0.146.0", "")
	body := []byte(`{"error":{"message":"This content was flagged for possible biological risk. If this seems wrong, try rephrasing your request."}}`)
	resp := &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}
	_, _ = svc.handleErrorResponse(ctx, resp, c, &account, nil, "gpt-5.6-sol")
	require.Contains(t, repo.values[SettingKeyCodexCLIOnlyUserBlacklist], "99")
}

func TestCyberPolicyUpstreamErrorAutoBlacklistsUser(t *testing.T) {
	settings, repo := newCodexBlacklistSettings()
	svc := &OpenAIGatewayService{settingService: settings}
	account := codexBlacklistAccount()
	c, ctx := codexBlacklistContext(101, "codex_cli_rs/0.146.0", "")
	body := []byte(`{"error":{"code":"cyber_policy","message":"This request was flagged for cyber policy."}}`)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}
	_, _ = svc.handleErrorResponse(ctx, resp, c, &account, nil, "gpt-5.6-sol")
	require.Contains(t, repo.values[SettingKeyCodexCLIOnlyUserBlacklist], "101")
}
