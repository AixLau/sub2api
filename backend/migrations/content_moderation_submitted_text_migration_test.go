package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationSubmittedTextMigrationIsIdempotent(t *testing.T) {
	schema, err := FS.ReadFile("240_content_moderation_submitted_text.sql")
	require.NoError(t, err)
	schemaSQL := string(schema)

	for _, column := range []string{
		"ADD COLUMN IF NOT EXISTS submitted_text TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS submitted_runes INT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS submitted_max_runes INT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS submitted_truncated BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS submitted_truncate_reasons JSONB NOT NULL DEFAULT '[]'::jsonb",
	} {
		require.Contains(t, schemaSQL, column)
	}
	require.Contains(t, schemaSQL, "content_moderation_logs_submitted_truncate_reasons_array_check")
	require.Contains(t, schemaSQL, "CHECK (jsonb_typeof(submitted_truncate_reasons) = 'array') NOT VALID")
	require.Contains(t, schemaSQL, "VALIDATE CONSTRAINT content_moderation_logs_submitted_truncate_reasons_array_check")
}
