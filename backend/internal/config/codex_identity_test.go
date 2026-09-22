package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadCodexIdentityHistoryRetention(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		days int
	}{
		{name: "default", days: 180},
		{name: "configured", env: "365", days: 365},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			if tc.env != "" {
				t.Setenv("GATEWAY_CODEX_IDENTITY_HISTORY_RETENTION_DAYS", tc.env)
			}
			cfg, err := Load()
			require.NoError(t, err)
			require.Equal(t, tc.days, cfg.Gateway.CodexIdentity.HistoryRetentionDays)
			require.Equal(t, time.Duration(tc.days)*24*time.Hour, cfg.Gateway.CodexIdentity.HistoryRetention())
		})
	}
}

func TestLoadCodexIdentityHistoryRetentionRejectsInvalid(t *testing.T) {
	for _, days := range []int{-1, 0, 36501} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("GATEWAY_CODEX_IDENTITY_HISTORY_RETENTION_DAYS", fmt.Sprint(days))
			_, err := Load()
			require.ErrorContains(t, err, "gateway.codex_identity.history_retention_days")
		})
	}
}

func TestCodexIdentityHistoryRetentionZeroValue(t *testing.T) {
	// Services and focused tests may construct Config without the Viper loader.
	// This must keep bounded retention instead of accidentally writing TTL=0.
	require.Equal(t, 180*24*time.Hour, (CodexIdentityConfig{}).HistoryRetention())
}
