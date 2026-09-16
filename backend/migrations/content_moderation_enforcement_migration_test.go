package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationEnforcementMigrationIsIdempotent(t *testing.T) {
	schema, err := FS.ReadFile("241_content_moderation_enforcement.sql")
	require.NoError(t, err)
	schemaSQL := string(schema)

	require.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS enforcement TEXT NOT NULL DEFAULT ''")
	require.Contains(t, schemaSQL, "content_moderation_logs_enforcement_values_check")
	require.Contains(t, schemaSQL, "CHECK (enforcement IN ('', 'allowed', 'blocked', 'error')) NOT VALID")
	require.Contains(t, schemaSQL, "VALIDATE CONSTRAINT content_moderation_logs_enforcement_values_check")
}

func TestContentModerationSubmittedTextSHA256MigrationIsIdempotent(t *testing.T) {
	schema, err := FS.ReadFile("242_content_moderation_submitted_text_sha256.sql")
	require.NoError(t, err)
	schemaSQL := string(schema)

	require.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS submitted_text_sha256 TEXT NOT NULL DEFAULT ''")
	require.Contains(t, schemaSQL, "content_moderation_logs_submitted_text_sha256_format_check")
	require.Contains(t, schemaSQL, "CHECK (submitted_text_sha256 = '' OR submitted_text_sha256 ~ '^[0-9a-f]{64}$') NOT VALID")
	require.Contains(t, schemaSQL, "VALIDATE CONSTRAINT content_moderation_logs_submitted_text_sha256_format_check")
}
