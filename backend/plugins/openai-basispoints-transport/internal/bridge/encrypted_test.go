package bridge

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEncryptedReplayPreservesBusinessData(t *testing.T) {
	raw := mustJSON(t, map[string]any{"input": []any{
		map[string]any{"type": "reasoning", "encrypted_content": "optional"},
		map[string]any{"type": "function_call", "arguments": map[string]any{"encrypted_content": "business value"}},
		map[string]any{"type": "function_call_output", "output": "{\"encrypted_content\":\"business text\"}"},
	}})
	out, err := StripEncryptedContent(raw)
	require.NoError(t, err)
	require.Contains(t, string(out), "business value")
	require.Contains(t, string(out), "business text")
	require.NotContains(t, string(out), "optional")
}

func TestEncryptedReplayRejectsOpaqueRequiredState(t *testing.T) {
	for _, item := range []any{
		map[string]any{"type": "compaction", "encrypted_content": "only copy"},
		map[string]any{"type": "function_call_output", "encrypted_content": "only copy"},
		map[string]any{"type": "function_call_output", "output": []any{map[string]any{"type": "input_text", "encrypted_content": "only copy"}}},
	} {
		raw := mustJSON(t, map[string]any{"input": []any{map[string]any{"type": "reasoning", "encrypted_content": "optional"}, item}})
		out, err := StripEncryptedContent(raw)
		require.ErrorIs(t, err, ErrUnsafeEncryptedReplay)
		require.Nil(t, out)
	}
}

func TestEncryptedReplayCleansRestoredKVItem(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	r := prepareWeather(t, store)
	original, _ := parseObject(officeItem("get_weather", map[string]any{"city": "Tokyo"}, false))
	original["encrypted_content"] = encoded("restored secret")
	converted, err := r.convertCall(ctx, encoded(original))
	require.NoError(t, err)
	call, _ := parseObject(converted)
	raw := mustJSON(t, map[string]any{"tools": json.RawMessage(weatherTool), "input": []any{map[string]any{"type": "function_call_output", "call_id": stringValue(call["call_id"]), "output": "sunny"}}})
	prepared, err := Prepare(ctx, raw, "session-a", store, nil)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "restored secret")
	require.Contains(t, string(prepared.Body), "restored secret")
	cleaned, err := StripEncryptedContent(prepared.Body)
	require.NoError(t, err)
	require.NotContains(t, string(cleaned), "restored secret")
	require.Contains(t, string(cleaned), "sunny")
	require.Contains(t, string(cleaned), r.Turn.ID)
	record, err := loadCall(ctx, store, "session-a", stringValue(call["call_id"]))
	require.NoError(t, err)
	require.Contains(t, string(record.Original), "restored secret", "recovery must not mutate shared replay records")
}
