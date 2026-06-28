package admin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type adminAuditCoverageManifest struct {
	SchemaVersion int                       `json:"schema_version"`
	Entries       []adminAuditCoverageEntry `json:"entries"`
}

type adminAuditCoverageEntry struct {
	Method          string   `json:"method"`
	Path            string   `json:"path"`
	Handler         string   `json:"handler"`
	Action          string   `json:"action"`
	TargetType      string   `json:"target_type"`
	RiskLevel       string   `json:"risk_level"`
	Status          string   `json:"status"`
	EventPhases     []string `json:"event_phases"`
	SensitiveFields []string `json:"sensitive_fields"`
	Implementation  string   `json:"implementation"`
}

func TestAdminAuditCoverageManifestDefinesP0AdminWriteSurface(t *testing.T) {
	manifest := loadAdminAuditCoverageManifest(t)
	if manifest.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("coverage manifest has no entries")
	}

	byAction := make(map[string]adminAuditCoverageEntry, len(manifest.Entries))
	for i, entry := range manifest.Entries {
		if strings.TrimSpace(entry.Method) == "" {
			t.Fatalf("entries[%d] missing method", i)
		}
		if strings.TrimSpace(entry.Path) == "" {
			t.Fatalf("entries[%d] missing path", i)
		}
		if strings.TrimSpace(entry.Handler) == "" {
			t.Fatalf("entries[%d] missing handler", i)
		}
		if strings.TrimSpace(entry.Action) == "" {
			t.Fatalf("entries[%d] missing action", i)
		}
		if strings.TrimSpace(entry.TargetType) == "" {
			t.Fatalf("entries[%d] missing target_type", i)
		}
		if !validAdminAuditRiskLevel(entry.RiskLevel) {
			t.Fatalf("entries[%d] action %q has invalid risk_level %q", i, entry.Action, entry.RiskLevel)
		}
		if !validAdminAuditCoverageStatus(entry.Status) {
			t.Fatalf("entries[%d] action %q has invalid status %q", i, entry.Action, entry.Status)
		}
		if len(entry.EventPhases) == 0 {
			t.Fatalf("entries[%d] action %q missing event_phases", i, entry.Action)
		}
		if entry.Status == "covered" && strings.TrimSpace(entry.Implementation) == "" {
			t.Fatalf("entries[%d] action %q is covered but missing implementation", i, entry.Action)
		}
		if previous, ok := byAction[entry.Action]; ok {
			t.Fatalf("duplicate action %q for paths %q and %q", entry.Action, previous.Path, entry.Path)
		}
		byAction[entry.Action] = entry
	}

	requiredActions := []string{
		"settings.update",
		"users.create",
		"users.update",
		"users.delete",
		"users.balance.update",
		"users.group.replace",
		"api_keys.group.update",
		"groups.create",
		"groups.update",
		"groups.delete",
		"accounts.create",
		"accounts.update",
		"accounts.delete",
		"accounts.credentials.batch_update",
		"risk_control.config.update",
		"payment.config.update",
		"payment.orders.cancel",
		"payment.orders.retry",
		"payment.orders.refund",
		"payment.plans.create",
		"payment.plans.update",
		"payment.plans.delete",
		"payment.providers.create",
		"payment.providers.update",
		"payment.providers.delete",
		"subscriptions.assign",
		"subscriptions.bulk_assign",
		"subscriptions.extend",
		"subscriptions.revoke",
		"channels.create",
		"channels.update",
		"channels.delete",
		"proxies.create",
		"proxies.update",
		"proxies.delete",
		"redeem_codes.generate",
		"redeem_codes.delete",
		"promo_codes.create",
		"promo_codes.update",
		"promo_codes.delete",
	}

	for _, action := range requiredActions {
		entry, ok := byAction[action]
		if !ok {
			t.Fatalf("required P0 admin audit action %q missing from coverage manifest", action)
		}
		if entry.Status != "covered" && entry.Status != "planned" {
			t.Fatalf("required P0 admin audit action %q has status %q, want covered or planned", action, entry.Status)
		}
	}

	if byAction["settings.update"].Status != "covered" {
		t.Fatalf("settings.update status = %q, want covered", byAction["settings.update"].Status)
	}
}

func loadAdminAuditCoverageManifest(t *testing.T) adminAuditCoverageManifest {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	path := filepath.Join(repoRoot, "docs", "risk-control", "admin-audit-coverage.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var manifest adminAuditCoverageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return manifest
}

func validAdminAuditRiskLevel(value string) bool {
	switch strings.TrimSpace(value) {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func validAdminAuditCoverageStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "covered", "planned", "intentional_no_audit":
		return true
	default:
		return false
	}
}
