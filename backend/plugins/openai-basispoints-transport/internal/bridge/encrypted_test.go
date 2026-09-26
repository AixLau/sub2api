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

func TestEncryptedReplayCopyRecognitionAndSafeDiagnostics(t *testing.T) {
	for _, output := range []any{"", []any{map[string]any{"type": "input_text", "text": ""}}, []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="}}, []any{map[string]any{"type": "input_file", "file_data": "aGVsbG8="}}} {
		raw := mustJSON(t, map[string]any{"input": []any{map[string]any{"type": "function_call_output", "output": output, "encrypted_content": "secret ciphertext"}}})
		out, err := StripEncryptedContent(raw)
		require.NoError(t, err)
		require.NotContains(t, string(out), "secret ciphertext")
		require.Contains(t, string(raw), "secret ciphertext", "original input remains intact")
		var result map[string][]map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(out, &result))
		require.JSONEq(t, string(mustJSON(t, output)), string(result["input"][0]["output"]))
	}
	raw := mustJSON(t, map[string]any{"input": []any{map[string]any{"type": "function_call_output", "output": []any{map[string]any{"type": "private arbitrary type", "encrypted_content": "secret ciphertext"}}}}})
	out, err := StripEncryptedContent(raw)
	require.Nil(t, out)
	require.ErrorIs(t, err, ErrUnsafeEncryptedReplay)
	var detail *EncryptedReplayError
	require.ErrorAs(t, err, &detail)
	require.Equal(t, "input[0].output[0]", detail.Path)
	require.Equal(t, "unknown", detail.ItemType)
	require.NotContains(t, err.Error(), "private arbitrary type")
	require.NotContains(t, err.Error(), "secret ciphertext")
	require.NotContains(t, err.Error(), "唯一副本")
}

func TestEncryptedReplayNullAndEmptyMetadata(t *testing.T) {
	for _, cipher := range []any{nil, ""} {
		raw := mustJSON(t, map[string]any{"input": []any{map[string]any{"type": "message", "content": "preserved", "encrypted_content": cipher}}})
		out, err := StripEncryptedContent(raw)
		require.NoError(t, err)
		require.Contains(t, string(out), "preserved")
		require.NotContains(t, string(out), "encrypted_content")
	}
	for _, output := range []any{nil, []any{}, []any{map[string]any{"type": "input_image", "file_id": "file_private"}}} {
		raw := mustJSON(t, map[string]any{"input": []any{map[string]any{"type": "function_call_output", "output": output, "encrypted_content": "secret"}}})
		_, err := StripEncryptedContent(raw)
		require.ErrorIs(t, err, ErrUnsafeEncryptedReplay)
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
	prepared, err := Prepare(ctx, raw, "session-a", store, nil, 256<<20)
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
