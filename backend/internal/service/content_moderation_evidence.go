package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type contentModerationSelectionMetadata struct {
	SchemaVersion          int
	CandidateKind          string
	CandidateKeyword       string
	CandidateCategory      string
	CandidateSeverity      string
	Route                  string
	SourceOrigin           string
	SelectedSource         string
	SelectedSourceRole     string
	SelectedFragmentRunes  int
	ReviewKind             string
	EvidenceComplete       bool
	EvidenceRunes          int
	EvidenceRevision       string
	EvidenceDigest         string
	EvidenceWindowed       bool
	EvidenceWindows        int
	EvidenceMatchesTotal   int
	EvidenceMatchesCovered int
}

func (metadata contentModerationSelectionMetadata) mapValue() map[string]any {
	return map[string]any{
		"selection_schema_version": metadata.SchemaVersion,
		"candidate_kind":           metadata.CandidateKind,
		"candidate_keyword":        metadata.CandidateKeyword,
		"candidate_category":       metadata.CandidateCategory,
		"candidate_severity":       metadata.CandidateSeverity,
		"selection_route":          metadata.Route,
		"source_origin":            metadata.SourceOrigin,
		"selected_source":          metadata.SelectedSource,
		"selected_source_role":     metadata.SelectedSourceRole,
		"selected_fragment_runes":  metadata.SelectedFragmentRunes,
		"review_kind":              metadata.ReviewKind,
		"evidence_complete":        metadata.EvidenceComplete,
		"evidence_runes":           metadata.EvidenceRunes,
		"evidence_revision":        metadata.EvidenceRevision,
		"evidence_digest":          metadata.EvidenceDigest,
		"evidence_windowed":        metadata.EvidenceWindowed,
		"evidence_windows":         metadata.EvidenceWindows,
		"evidence_matches_total":   metadata.EvidenceMatchesTotal,
		"evidence_matches_covered": metadata.EvidenceMatchesCovered,
	}
}

func marshalContentModerationMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(raw)
}

// ContentModerationEvidenceSnapshot contains the exact bounded payload sent
// to the selected reviewer. PayloadEncrypted is never returned in a list API.
type ContentModerationEvidenceSnapshot struct {
	ID               int64          `json:"id"`
	LogID            int64          `json:"log_id"`
	RequestID        string         `json:"request_id"`
	Selection        map[string]any `json:"selection"`
	PayloadEncrypted string         `json:"-"`
	PayloadHMAC      string         `json:"payload_hmac"`
	PayloadRunes     int            `json:"payload_runes"`
	CreatedAt        time.Time      `json:"created_at"`
}

type ContentModerationEvidenceView struct {
	LogID        int64          `json:"log_id"`
	RequestID    string         `json:"request_id"`
	Selection    map[string]any `json:"selection"`
	Payload      string         `json:"payload"`
	PayloadHMAC  string         `json:"payload_hmac"`
	PayloadRunes int            `json:"payload_runes"`
	CreatedAt    time.Time      `json:"created_at"`
}

type ContentModerationEvidenceStore interface {
	CreateEvidenceSnapshot(ctx context.Context, snapshot *ContentModerationEvidenceSnapshot) error
	GetEvidenceSnapshotByLogID(ctx context.Context, logID int64) (*ContentModerationEvidenceSnapshot, error)
}

func (s *ContentModerationService) SetEvidenceStore(store ContentModerationEvidenceStore, encryptor SecretEncryptor) {
	if s == nil {
		return
	}
	s.evidenceStore = store
	if encryptor != nil {
		s.rawRequestEncryptor = encryptor
	}
}

func (s *ContentModerationService) storeCandidateEvidence(ctx context.Context, log *ContentModerationLog, selection contentModerationCandidateSelection, payloadHMAC string) {
	if s == nil || log == nil || log.ID <= 0 || s.evidenceStore == nil || s.rawRequestEncryptor == nil {
		return
	}
	payload := strings.TrimSpace(selection.Fragment)
	if log.DecisionSource == contentModerationDecisionSourceSemantic &&
		selection.ReviewKind == contentModerationReviewKindPromptInjection &&
		strings.TrimSpace(selection.ReviewText) != "" {
		payload = strings.TrimSpace(redactContentModerationSecrets(selection.ReviewText))
	}
	if payload == "" {
		return
	}
	encrypted, err := s.rawRequestEncryptor.Encrypt(payload)
	if err != nil {
		slog.Warn("content_moderation.evidence_encrypt_failed", "log_id", log.ID, "error", err)
		return
	}
	selectionMetadata := selection.metadata().mapValue()
	selectionMetadata["decision_source"] = log.DecisionSource
	selectionMetadata["moderation_provider"] = log.ModerationProvider
	selectionMetadata["moderation_model"] = log.ModerationModel
	selectionMetadata["user_violation_eligible"] = log.UserViolationEligible
	selectionMetadata["source_truncated"] = selection.Source.Truncated
	selectionMetadata["truncate_reasons"] = append([]string(nil), selection.Source.TruncateReasons...)
	snapshot := &ContentModerationEvidenceSnapshot{
		LogID:            log.ID,
		RequestID:        log.RequestID,
		Selection:        selectionMetadata,
		PayloadEncrypted: encrypted,
		PayloadHMAC:      payloadHMAC,
		PayloadRunes:     len([]rune(payload)),
		CreatedAt:        time.Now(),
	}
	if err := s.evidenceStore.CreateEvidenceSnapshot(ctx, snapshot); err != nil {
		slog.Warn("content_moderation.evidence_store_failed", "log_id", log.ID, "error", err)
		return
	}
	log.EvidenceAvailable = true
}

func (s *ContentModerationService) GetEvidenceSnapshot(ctx context.Context, logID int64) (*ContentModerationEvidenceView, error) {
	if logID <= 0 {
		return nil, fmt.Errorf("invalid content moderation log id")
	}
	if s == nil || s.evidenceStore == nil || s.rawRequestEncryptor == nil {
		return nil, fmt.Errorf("content moderation evidence is unavailable")
	}
	snapshot, err := s.evidenceStore.GetEvidenceSnapshotByLogID(ctx, logID)
	if err != nil {
		return nil, err
	}
	payload, err := s.rawRequestEncryptor.Decrypt(snapshot.PayloadEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt content moderation evidence: %w", err)
	}
	return &ContentModerationEvidenceView{
		LogID:        snapshot.LogID,
		RequestID:    snapshot.RequestID,
		Selection:    snapshot.Selection,
		Payload:      payload,
		PayloadHMAC:  snapshot.PayloadHMAC,
		PayloadRunes: snapshot.PayloadRunes,
		CreatedAt:    snapshot.CreatedAt,
	}, nil
}
