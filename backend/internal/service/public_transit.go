package service

import (
	"context"
	"time"
)

const (
	PublicTransitSchemaVersion = "ai-transit.v1"
	PublicTransitSystem        = "sub2api"
	PublicTransitWellKnownPath = "/.well-known/ai-transit.json"
	PublicTransitSnapshotPath  = "/api/public/transit/v1/snapshot"
)

type PublicTransitDiscovery struct {
	SchemaVersion string `json:"schema_version"`
	System        string `json:"system"`
	SnapshotURL   string `json:"snapshot_url"`
	GeneratedAt   string `json:"generated_at"`
}
type PublicTransitSnapshot struct {
	SchemaVersion string `json:"schema_version"`
	System        string `json:"system"`
	GeneratedAt   string `json:"generated_at"`
	Monitoring    any    `json:"monitoring,omitempty"`
}
type PublicTransitService struct{}

func (s *SettingService) PublicTransitEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return true
	}
	v, err := s.settingRepo.GetValue(ctx, SettingKeyPublicTransitEnabled)
	return err != nil || !isFalseSettingValue(v)
}

func NewPublicTransitService() *PublicTransitService { return &PublicTransitService{} }
func (s *PublicTransitService) Discovery(context.Context, string) (*PublicTransitDiscovery, error) {
	return &PublicTransitDiscovery{PublicTransitSchemaVersion, PublicTransitSystem, PublicTransitSnapshotPath, time.Now().UTC().Format(time.RFC3339)}, nil
}
func (s *PublicTransitService) Snapshot(context.Context, string) (*PublicTransitSnapshot, error) {
	return &PublicTransitSnapshot{PublicTransitSchemaVersion, PublicTransitSystem, time.Now().UTC().Format(time.RFC3339), nil}, nil
}
