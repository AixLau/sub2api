package migrations

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/image/webp"
)

func TestWelcomeRewardSkinMigrationEmbedsValidLegacyImages(t *testing.T) {
	content, err := FS.ReadFile("194_seed_welcome_reward_skins.sql")
	require.NoError(t, err)

	pattern := regexp.MustCompile(`(?s)'([0-9a-f]{64})',\s*DECODE\('([^']+)', 'base64'\)`)
	matches := pattern.FindAllStringSubmatch(string(content), -1)
	require.Len(t, matches, 3)

	expectedSizes := map[string]int{
		"3edc56d44c36dea6ca8a485334d47be93956b4cce1275f5ad1110fa4264794a9": 149596,
		"5d0f5b9fbeccd24b47a54fcdda46341dc6212b33f107822c4bf3d8199fd9d171": 83232,
		"9a0788b03bdbf5d9c2c999a266f8cf0c40006f64c80115f7c89839754de7d8e5": 50104,
	}

	for _, match := range matches {
		expectedHash := match[1]
		expectedSize, ok := expectedSizes[expectedHash]
		require.True(t, ok, "unexpected embedded skin hash %s", expectedHash)

		imageBytes, err := base64.StdEncoding.DecodeString(match[2])
		require.NoError(t, err)
		require.Len(t, imageBytes, expectedSize)

		actualHash := sha256.Sum256(imageBytes)
		require.Equal(t, expectedHash, hex.EncodeToString(actualHash[:]))

		config, err := webp.DecodeConfig(bytes.NewReader(imageBytes))
		require.NoError(t, err)
		require.Equal(t, 1320, config.Width)
		require.Equal(t, 500, config.Height)
	}
}

func TestWelcomeRewardSkinMigrationVersionsUnskinnedSystemCampaigns(t *testing.T) {
	content, err := FS.ReadFile("194_seed_welcome_reward_skins.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ON CONFLICT (sha256) DO NOTHING")
	require.Contains(t, sql, "WHERE system_key IN ('system_welcome', 'system_surprise')")
	require.Contains(t, sql, "FOR UPDATE")
	require.Contains(t, sql, "JSONB_SET(current_version.config, '{skin_weights}', skins.weights, TRUE)")
	require.Contains(t, sql, "JSONB_ARRAY_LENGTH(current_version.config->'skin_weights') = 0")
	require.Contains(t, sql, "INSERT INTO reward_campaign_versions")
	require.Contains(t, sql, "SET current_version_id = version.id")
}
