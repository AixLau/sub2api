package migrate

import (
	"testing"

	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/stretchr/testify/require"
)

func TestUsageLogsActorForeignKeysCascadeOnDelete(t *testing.T) {
	for _, column := range []string{"user_id", "api_key_id"} {
		fk := findForeignKeyByColumn(t, UsageLogsTable, column)
		require.Equal(t, entschema.Cascade, fk.OnDelete, "unexpected ON DELETE action for usage_logs.%s", column)
	}
}
