package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	// DefaultCodexClientConfigCacheTTL bounds how long a stale Redis entry can
	// survive if an explicit invalidation is lost. Updates still write through
	// (or delete) immediately; the TTL is only the last-resort safety net.
	DefaultCodexClientConfigCacheTTL = 2 * time.Minute
)

// CodexClientAccountState is the small, cacheable part of an account that
// decides whether a request may use it for a given client type.
type CodexClientAccountState struct {
	Enabled          bool `json:"enabled"`
	AppServerAllowed bool `json:"app_server_allowed,omitempty"`
}

// CodexClientAccountStateFromAccount extracts the codex_cli_only flags. The
// caller owns the account struct; this helper never mutates it.
func CodexClientAccountStateFromAccount(account *Account) CodexClientAccountState {
	if account == nil {
		return CodexClientAccountState{}
	}
	return CodexClientAccountState{
		Enabled:          account.IsCodexCLIOnlyEnabled(),
		AppServerAllowed: account.IsCodexCLIOnlyAppServerAllowed(),
	}
}

// CodexClientConfigCache caches the per-account Codex-client support flag and
// the global Codex client User-Agent whitelist. Implementations must return
// found=false for a miss and a non-nil error for an unavailable cache so that
// callers can fall back to the database instead of trusting stale data.
type CodexClientConfigCache interface {
	GetCodexClientAccountState(ctx context.Context, accountID int64) (CodexClientAccountState, bool, error)
	SetCodexClientAccountState(ctx context.Context, accountID int64, state CodexClientAccountState, ttl time.Duration) error
	DeleteCodexClientAccountState(ctx context.Context, accountID int64) error

	GetCodexClientWhitelist(ctx context.Context) ([]openai.AllowedClientEntry, bool, error)
	SetCodexClientWhitelist(ctx context.Context, entries []openai.AllowedClientEntry, ttl time.Duration) error
	DeleteCodexClientWhitelist(ctx context.Context) error
}
