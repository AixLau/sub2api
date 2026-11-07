package migrations

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropPlatformCheckConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("242_drop_platform_check_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;")
	require.Contains(t, sql,
		"ALTER TABLE composite_model_routes DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;")
	require.NotContains(t, sql, "ADD CONSTRAINT")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitors_provider_check")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check")
}

// Platform membership is validated against the application catalog after 242.
// Shipped migrations are checksum-protected and cannot be edited, so a later
// migration that re-adds a constraint (243 did) must be followed by one that
// drops it again: after all migrations run, the last statement touching each
// constraint has to be a DROP.
func TestLaterMigrationsDoNotRestorePlatformCheckConstraints(t *testing.T) {
	const droppedAt = "242_drop_platform_check_constraints.sql"
	entries, err := fs.ReadDir(FS, ".")
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") && entry.Name() > droppedAt {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names) // the migration runner applies files in filename order

	comments := regexp.MustCompile(`(?ms)/\*.*?\*/|--[^\n]*`)
	statement := regexp.MustCompile(`(?i)\b(ADD|DROP)\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?"?(user_platform_quotas_platform_check|composite_model_routes_target_platform_check)\b`)
	lastAction := map[string]string{}
	lastFile := map[string]string{}
	for _, name := range names {
		content, err := FS.ReadFile(name)
		require.NoError(t, err)
		sql := comments.ReplaceAll(content, nil)
		for _, match := range statement.FindAllSubmatch(sql, -1) {
			constraint := string(match[2])
			lastAction[constraint] = strings.ToUpper(string(match[1]))
			lastFile[constraint] = name
		}
	}
	for constraint, action := range lastAction {
		require.Equal(t, "DROP", action,
			"%s is re-added by %s without a later DROP; platform CHECK constraints removed in 242 must remain managed by application validation",
			constraint, lastFile[constraint])
	}
}

func TestRedropPlatformCheckConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("251_redrop_platform_check_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;")
	require.Contains(t, sql,
		"ALTER TABLE composite_model_routes DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;")
	require.NotContains(t, sql, "ADD CONSTRAINT")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitors_provider_check")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check")
}
