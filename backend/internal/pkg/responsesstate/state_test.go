package responsesstate

import "testing"

func TestInputKeysClassifiesHTTPAccountBoundHistory(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"ordinary_history", `{"input":[{"type":"message","content":[{"type":"input_text","text":"hello"}]}]}`, false},
		{"encrypted_reasoning", `{"input":[{"type":"reasoning","encrypted_content":"cipher"}]}`, true},
		{"encrypted_compaction", `{"input":[{"type":"compaction","encrypted_content":"cipher"}]}`, true},
		{"bps_call", `{"input":[{"type":"function_call","call_id":"call_bps_123","arguments":"{}"}]}`, true},
		{"bps_result", `{"input":[{"type":"function_call_output","call_id":"call_bps_123","output":"ok"}]}`, true},
		{"encrypted_function_args", `{"input":[{"type":"function_call","encrypted_function_args":["opaque"]}]}`, true},
		{"ordinary_tool_call", `{"input":[{"type":"function_call","call_id":"call_123","arguments":"{}"}]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := len(InputKeys([]byte(tt.body))) > 0
			if got != tt.want {
				t.Fatalf("account-bound=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputKeysCapturesEncryptedAndBPSProvenance(t *testing.T) {
	body := []byte(`{"id":"resp_1","output":[{"type":"reasoning","encrypted_content":"cipher"},{"type":"function_call","call_id":"call_bps_123"}]}`)
	if got := len(OutputKeys(body)); got != 2 {
		t.Fatalf("got %d ownership keys, want 2", got)
	}
}

func TestOutputKeysRecordsOpaqueItemReferencesForLaterReplay(t *testing.T) {
	body := []byte(`{"id":"resp_1","output":[{"type":"message","id":"msg_opaque","content":[]}]}`)
	if got := len(OutputKeys(body)); got != 1 {
		t.Fatalf("got %d ownership keys, want 1", got)
	}
	if got := len(InputKeys([]byte(`{"input":[{"type":"item_reference","id":"msg_opaque"}]}`))); got != 1 {
		t.Fatalf("item_reference produced %d keys, want 1", got)
	}
}
