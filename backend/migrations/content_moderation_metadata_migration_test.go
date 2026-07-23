package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationMetadataMigrationsKeepErrorContractExplicit(t *testing.T) {
	schema, err := FS.ReadFile("186_content_moderation_metadata.sql")
	require.NoError(t, err)
	schemaSQL := string(schema)
	require.Contains(t, schemaSQL, "ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, schemaSQL, "jsonb_typeof(metadata) = 'object'")

	backfill, err := FS.ReadFile("187_content_moderation_metadata_backfill.sql")
	require.NoError(t, err)
	backfillSQL := string(backfill)
	require.Contains(t, backfillSQL, "content_moderation_metadata_backfill_audit")
	require.Contains(t, backfillSQL, "invalid_json_count")
	require.Contains(t, backfillSQL, "availability_failure")
	require.Contains(t, backfillSQL, "semantic_review_verdict")
	require.Contains(t, backfillSQL, "SET metadata = v_metadata")
	require.Contains(t, backfillSQL, "error = ''")
}
