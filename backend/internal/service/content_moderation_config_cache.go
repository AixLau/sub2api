package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type contentModerationConfigSnapshot struct {
	// Published configs are immutable so request paths can share their slices
	// and prepared matchers without per-request locking or cloning.
	config           *ContentModerationConfig
	digest           [sha256.Size]byte
	loadedAt         time.Time
	revisionDisabled string
	revisionEnabled  string
}

func newContentModerationConfigSnapshot(cfg *ContentModerationConfig, digest [sha256.Size]byte, loadedAt time.Time) *contentModerationConfigSnapshot {
	if cfg == nil {
		return nil
	}
	return &contentModerationConfigSnapshot{
		config:           cfg,
		digest:           digest,
		loadedAt:         loadedAt,
		revisionDisabled: contentModerationPolicyRevision(false, cfg),
		revisionEnabled:  contentModerationPolicyRevision(true, cfg),
	}
}

func (s *contentModerationConfigSnapshot) policyRevision(riskEnabled bool) string {
	if s == nil {
		return ""
	}
	if riskEnabled {
		return s.revisionEnabled
	}
	return s.revisionDisabled
}

func (s *ContentModerationService) loadConfig(ctx context.Context) (*ContentModerationConfig, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("content moderation setting repository unavailable")
	}
	now := time.Now()
	if snapshot := s.configSnapshot.Load(); snapshot != nil {
		if now.Sub(snapshot.loadedAt) < s.configSnapshotTTL() {
			return snapshot.config, nil
		}
		s.triggerConfigSnapshotRefresh()
		return snapshot.config, nil
	}

	s.configRefreshMu.Lock()
	defer s.configRefreshMu.Unlock()
	if snapshot := s.configSnapshot.Load(); snapshot != nil {
		return snapshot.config, nil
	}
	return s.refreshConfigSnapshot(ctx)
}

func (s *ContentModerationService) loadConfigFresh(ctx context.Context) (*ContentModerationConfig, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("content moderation setting repository unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContentModerationConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return parseContentModerationConfig("")
		}
		return nil, fmt.Errorf("get content moderation config: %w", err)
	}
	return parseContentModerationConfig(raw)
}

func (s *ContentModerationService) loadConfigWithRevision(ctx context.Context, riskEnabled bool) (*ContentModerationConfig, string, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	if snapshot := s.configSnapshot.Load(); snapshot != nil && snapshot.config == cfg {
		if revision := snapshot.policyRevision(riskEnabled); revision != "" {
			return cfg, revision, nil
		}
	}
	return cfg, contentModerationPolicyRevision(riskEnabled, cfg), nil
}

func (s *ContentModerationService) configSnapshotTTL() time.Duration {
	if s != nil && s.configCacheTTL > 0 {
		return s.configCacheTTL
	}
	return contentModerationConfigCacheTTL
}

func (s *ContentModerationService) triggerConfigSnapshotRefresh() {
	if s == nil || s.configRefreshDeferred() || !s.configRefreshMu.TryLock() {
		return
	}
	if s.configRefreshDeferred() {
		s.configRefreshMu.Unlock()
		return
	}
	go func() {
		defer s.configRefreshMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), contentModerationConfigRefreshTimeout)
		defer cancel()
		if _, err := s.refreshConfigSnapshot(ctx); err != nil {
			s.configRefreshRetryAt.Store(time.Now().Add(s.configSnapshotTTL()).UnixNano())
			slog.Warn("content_moderation.config_snapshot_refresh_failed", "error", err)
		}
	}()
}

func (s *ContentModerationService) configRefreshDeferred() bool {
	return s != nil && time.Now().UnixNano() < s.configRefreshRetryAt.Load()
}

func (s *ContentModerationService) refreshConfigSnapshot(ctx context.Context) (*ContentModerationConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContentModerationConfig)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			return nil, fmt.Errorf("get content moderation config: %w", err)
		}
		raw = ""
	}
	digest := sha256.Sum256([]byte(raw))
	if current := s.configSnapshot.Load(); current != nil && current.digest == digest {
		refreshed := *current
		refreshed.loadedAt = time.Now()
		s.configSnapshot.Store(&refreshed)
		s.configRefreshRetryAt.Store(0)
		return current.config, nil
	}
	cfg, err := parseContentModerationConfig(raw)
	if err != nil {
		return nil, err
	}
	if err := s.storeConfigSnapshot(newContentModerationConfigSnapshot(cfg, digest, time.Now())); err != nil {
		return nil, fmt.Errorf("apply content moderation config: %w", err)
	}
	return cfg, nil
}

func (s *ContentModerationService) publishConfigSnapshot(cfg *ContentModerationConfig, raw []byte) error {
	if s == nil || cfg == nil {
		return nil
	}
	s.configRefreshMu.Lock()
	defer s.configRefreshMu.Unlock()
	published := cloneContentModerationConfig(cfg)
	return s.storeConfigSnapshot(newContentModerationConfigSnapshot(published, sha256.Sum256(raw), time.Now()))
}

func (s *ContentModerationService) storeConfigSnapshot(snapshot *contentModerationConfigSnapshot) error {
	if s == nil || snapshot == nil || snapshot.config == nil {
		return nil
	}
	if s.resourceProtection != nil {
		if err := s.resourceProtection.Update(snapshot.config.ResourceProtectionConfig); err != nil {
			return err
		}
	}
	s.configSnapshot.Store(snapshot)
	s.configRefreshRetryAt.Store(0)
	return nil
}
