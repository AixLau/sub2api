package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// AddCodexCLIOnlyUserToBlacklist adds a user to the Codex restricted-account
// blacklist. The read/merge/write is serialized with policy reloads so two
// simultaneous upstream policy errors cannot overwrite each other's user.
func (s *SettingService) AddCodexCLIOnlyUserToBlacklist(ctx context.Context, userID int64) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("setting repository unavailable")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user ID %d", userID)
	}

	s.codexRestrictionPolicyMu.Lock()
	defer s.codexRestrictionPolicyMu.Unlock()

	raw, err := s.settingRepo.GetValue(ctx, SettingKeyCodexCLIOnlyUserBlacklist)
	if errors.Is(err, ErrSettingNotFound) {
		raw = ""
	} else if err != nil {
		return fmt.Errorf("read %s: %w", SettingKeyCodexCLIOnlyUserBlacklist, err)
	}
	ids, err := ParseCodexCLIOnlyUserBlacklist(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", SettingKeyCodexCLIOnlyUserBlacklist, err)
	}
	if _, exists := ids[userID]; exists {
		return nil
	}
	ids[userID] = struct{}{}

	ordered := make([]int64, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	values := make([]string, len(ordered))
	for i, id := range ordered {
		values[i] = strconv.FormatInt(id, 10)
	}
	encoded := strings.Join(values, ",")
	if err := s.settingRepo.Set(ctx, SettingKeyCodexCLIOnlyUserBlacklist, encoded); err != nil {
		return fmt.Errorf("write %s: %w", SettingKeyCodexCLIOnlyUserBlacklist, err)
	}
	s.codexRestrictionPolicySF.Forget("codex_restriction_policy")
	s.codexRestrictionPolicyCache.Store(&cachedCodexRestrictionPolicy{})
	return nil
}
