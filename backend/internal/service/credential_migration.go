package service

import "context"

type CredentialMigrationPreview struct {
	AccountID               int64  `json:"account_id"`
	CredentialSourceID      int64  `json:"credential_source_id"`
	State                   string `json:"state"`
	HasLegacySeed           bool   `json:"has_legacy_seed"`
	HasInstallationOverride bool   `json:"has_installation_override"`
	GroupCount              int    `json:"group_count"`
	ExistingConcurrency     int    `json:"existing_concurrency"`
	RequiresExplicitTotal   bool   `json:"requires_explicit_total"`
}
type CredentialMigrationStore interface {
	ShadowCredentialDecision(context.Context, int64) ([]CredentialDemand, error)
	PreviewCredentialMigration(context.Context, []int64) ([]CredentialMigrationPreview, error)
	PrepareCredentialRollback(context.Context, int64, int64, int64) error
}
