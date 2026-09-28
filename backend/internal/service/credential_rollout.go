package service

import (
	"errors"
	"strings"
)

// ResolveCredentialMigrationProfile uses the existing identity implementation.
// Unknown legacy identity is blocked, never regenerated during migration.
func ResolveCredentialMigrationProfile(account *Account) (installation, seed, source string, err error) {
	if account == nil || !account.IsOpenAIOAuthLike() {
		return "", "", "", ErrCredentialUnverified
	}
	if mode := account.GetCodexFingerprintMode(); mode == codexFingerprintSession || mode == codexFingerprintFull {
		return "", "", "", errors.New("SESSION_FULL_MIGRATION_OUT_OF_SCOPE")
	}
	seed, _ = codexFingerprintSeed(account.Extra)
	installation = resolveConvergedInstallationID(account, seed)
	if strings.TrimSpace(installation) == "" {
		return "", "", "", errors.New("LEGACY_INSTALLATION_UNVERIFIED")
	}
	source = "LEGACY_SEED"
	if account.GetOpenAIDeviceID() != "" {
		source = "IMPORTED_CLIENT"
	}
	return
}
