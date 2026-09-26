// Package bridge adapts client tool calls to the Basis Points Responses wire
// protocol. It never executes code or tools; the downstream client owns execution.
package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	MaxResponseBytes = 64 << 20  // Response buffering is independent of the host request policy.
	MaxItemBytes     = 240 << 10 // HostService KV values are limited to 256 KiB.
	MaxIterations    = 512       // Runaway guard, not a product limit: real agent turns exceed 64 tool rounds.
	StateTTLSeconds  = 24 * 60 * 60
	callPrefix       = "call_bps_"
)

// Store must be shared across plugin processes and restarts. Production uses
// the host KV RPC; there is intentionally no process-local state fallback.
type Store interface {
	Get(context.Context, string) ([]byte, bool, error)
	Put(context.Context, string, []byte) error
}

type Turn struct {
	ID        string `json:"turn_id"`
	Iteration int    `json:"agent_iteration"`
}

type callRecord struct {
	FeedbackID string          `json:"feedback_id,omitempty"`
	Original   json.RawMessage `json:"original"`
	Client     json.RawMessage `json:"client"`
	Turn       Turn            `json:"turn"`
}

func digest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Scope includes the host-isolated session (when supplied), credential account,
// and endpoint. Neither credentials nor session identifiers appear in KV keys.
func Scope(accountID, endpoint, session string) string {
	return digest(accountID, endpoint, session)
}

func stateKey(scope, kind, id string) string { return digest(scope, kind, id) }

// functionItemID derives the item id used by replayed tool results. The
// upstream validates item ids as fc_ + [A-Za-z0-9_-] and rejects anything
// else, so ids are concatenated only when the call id is already clean and not
// itself fc_ prefixed; otherwise a stable hash is used.
func functionItemID(callID string) string {
	const maxLength = 64
	if callID != "" && !strings.HasPrefix(callID, "fc_") && isCleanItemID(callID) {
		if id := "fc_" + callID; len(id) <= maxLength {
			return id
		}
	}
	sum := sha256.Sum256([]byte(callID))
	return "fc_" + hex.EncodeToString(sum[:])[:maxLength-len("fc_")]
}

func isCleanItemID(id string) bool {
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func loadCall(ctx context.Context, store Store, scope, id string) (*callRecord, error) {
	if store == nil {
		return nil, errors.New("工具回放需要宿主 KV 服务")
	}
	raw, found, err := store.Get(ctx, stateKey(scope, "call", id))
	if err != nil {
		return nil, errors.New("读取工具回放状态失败")
	}
	if !found {
		return nil, errors.New("工具回放状态不存在或已过期；请开始新的会话")
	}
	var record callRecord
	if json.Unmarshal(raw, &record) != nil || !json.Valid(record.Original) || !json.Valid(record.Client) || record.Turn.ID == "" {
		return nil, errors.New("工具回放状态无效")
	}
	return &record, nil
}

func putState(ctx context.Context, store Store, key string, value any) error {
	if store == nil {
		return errors.New("工具桥接需要宿主 KV 服务")
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxItemBytes {
		return errors.New("工具回放状态超过宿主 KV 大小限制")
	}
	if err := store.Put(ctx, key, raw); err != nil {
		return errors.New("保存工具回放状态失败")
	}
	return nil
}

type object map[string]json.RawMessage

func parseObject(raw []byte) (object, error) {
	var obj object
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, errors.New("协议字段必须是 JSON 对象")
	}
	return obj, nil
}

func stringValue(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func encoded(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
