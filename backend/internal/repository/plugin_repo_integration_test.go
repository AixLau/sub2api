//go:build integration

package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPluginRepositoryLifecycleIsAtomicAndOptimistic(t *testing.T) {
	ctx := context.Background()
	repo := &pluginRepository{db: integrationDB}
	pluginKey := "local.test.repository-" + strings.ToLower(time.Now().Format("150405.000000000"))
	defer func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM sub2api_plugin_installations WHERE plugin_key = $1`, pluginKey)
	}()
	var accountID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type, credentials, extra, status)
		VALUES ($1, $2, $3, '{}'::jsonb, '{}'::jsonb, 'active')
		RETURNING id
	`, "plugin-scope-"+pluginKey, service.PlatformOpenAI, service.AccountTypeOAuth).Scan(&accountID)
	require.NoError(t, err)
	defer func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	}()
	require.NoError(t, repo.ValidateOpenAIOAuthAccounts(ctx, []int64{accountID}))
	err = repo.ValidateOpenAIOAuthAccounts(ctx, []int64{accountID + 9000000000})
	require.ErrorContains(t, err, "不存在或不是 OpenAI OAuth")

	manifest := service.PluginManifest{SchemaVersion: 1, ID: pluginKey, Name: "测试插件", Version: "1.0.0"}
	first := &service.PluginInstallation{
		PluginKey: pluginKey, Name: "测试插件", Version: "1.0.0", Manifest: manifest,
		ArtifactData: []byte("first-package"), ArtifactPath: "/tmp/first.s2plugin",
		InstallPath: "/tmp/first", BinaryPath: "/tmp/first/plugin",
		BinarySHA256: strings.Repeat("a", 64), SignatureStatus: service.PluginSignatureTrusted,
		State: service.PluginStateDisabled,
	}
	bindings := []service.PluginBinding{{
		Capability: service.PluginCapabilityOpenAIOAuthOutbound,
		Platform:   service.PlatformOpenAI, AccountType: service.AccountTypeOAuth,
		AccountIDs: []int64{accountID},
	}}
	installed, err := repo.Install(ctx, first, bindings)
	require.NoError(t, err)
	require.Equal(t, []int64{accountID}, installed.Bindings[0].AccountIDs)
	artifact, err := repo.GetArtifact(ctx, installed.ID)
	require.NoError(t, err)
	require.Equal(t, first.ArtifactData, artifact)

	require.NoError(t, repo.BeginEnable(ctx, installed.ID, first.BinarySHA256, service.PluginStateDisabled))
	bindings[0].Enabled = true
	now := time.Now()
	require.NoError(t, repo.UpdateBindingsAndState(
		ctx, installed.ID, bindings, service.PluginStateEnabled, "", &now,
		service.PluginStateStarting, first.BinarySHA256,
	))
	enabled, err := repo.GetByID(ctx, installed.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{accountID}, enabled.Bindings[0].AccountIDs)

	second := *first
	second.Version = "1.1.0"
	second.Manifest.Version = second.Version
	second.BinarySHA256 = strings.Repeat("b", 64)
	second.ArtifactData = []byte("second-package")
	_, err = repo.Install(ctx, &second, bindings)
	require.ErrorIs(t, err, service.ErrPluginStateChanged)

	bindings[0].Enabled = false
	require.NoError(t, repo.UpdateBindingsAndState(
		ctx, installed.ID, bindings, service.PluginStateDisabled, "", nil, "", first.BinarySHA256,
	))
	replaced, err := repo.Install(ctx, &second, bindings)
	require.NoError(t, err)
	require.Equal(t, installed.ID, replaced.ID)

	err = repo.Delete(ctx, replaced.ID, first.BinarySHA256)
	require.True(t, errors.Is(err, service.ErrPluginStateChanged))
	require.NoError(t, repo.Delete(ctx, replaced.ID, second.BinarySHA256))
}

func TestPluginAccountScopeIsRemovedWhenAccountIsSoftDeleted(t *testing.T) {
	ctx := context.Background()
	repo := &pluginRepository{db: integrationDB}
	pluginKey := "local.test.soft-delete-" + strings.ToLower(time.Now().Format("150405.000000000"))

	var accountID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type, credentials, extra, status)
		VALUES ($1, $2, $3, '{}'::jsonb, '{}'::jsonb, 'active')
		RETURNING id
	`, "plugin-soft-delete-"+pluginKey, service.PlatformOpenAI, service.AccountTypeOAuth).Scan(&accountID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM sub2api_plugin_installations WHERE plugin_key = $1`, pluginKey)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
	})

	installed, err := repo.Install(ctx, &service.PluginInstallation{
		PluginKey: pluginKey,
		Name:      "软删除绑定测试插件",
		Version:   "1.0.0",
		Manifest: service.PluginManifest{
			SchemaVersion: 1,
			ID:            pluginKey,
			Name:          "软删除绑定测试插件",
			Version:       "1.0.0",
		},
		ArtifactData:    []byte("package"),
		ArtifactPath:    "/tmp/plugin-soft-delete.s2plugin",
		InstallPath:     "/tmp/plugin-soft-delete",
		BinaryPath:      "/tmp/plugin-soft-delete/plugin",
		BinarySHA256:    strings.Repeat("c", 64),
		SignatureStatus: service.PluginSignatureTrusted,
		State:           service.PluginStateDisabled,
	}, []service.PluginBinding{{
		Capability:  service.PluginCapabilityOpenAIOAuthOutbound,
		Platform:    service.PlatformOpenAI,
		AccountType: service.AccountTypeOAuth,
		AccountIDs:  []int64{accountID},
	}})
	require.NoError(t, err)
	require.Equal(t, []int64{accountID}, installed.Bindings[0].AccountIDs)

	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, accountID)
	require.NoError(t, err)

	updated, err := repo.GetByID(ctx, installed.ID)
	require.NoError(t, err)
	require.Empty(t, updated.Bindings[0].AccountIDs)
}
