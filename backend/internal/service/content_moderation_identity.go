package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
)

type ModerationIdentityInput struct {
	KeyVersion     uint64
	FeedbackEpoch  uint64
	Provider       string
	Model          string
	AuditScope     string
	PolicyScope    string
	ChunkerVersion string
	ContextFrame   []byte
	NormalizedText string
}

func BuildModerationChunkIdentity(key []byte, in ModerationIdentityInput) ([]byte, []byte, error) {
	if len(key) != sha256.Size {
		return nil, nil, errors.New("moderation identity key must be 32 bytes")
	}
	message := make([]byte, 0, 128+len(in.ContextFrame)+len(in.NormalizedText))
	message = binary.BigEndian.AppendUint64(message, in.KeyVersion)
	for _, value := range []string{in.Provider, in.Model, in.AuditScope, in.PolicyScope, in.ChunkerVersion} {
		message = appendLengthPrefixed(message, []byte(value))
	}
	message = binary.BigEndian.AppendUint64(message, in.FeedbackEpoch)
	message = appendLengthPrefixed(message, in.ContextFrame)
	message = appendLengthPrefixed(message, []byte(in.NormalizedText))
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return message, mac.Sum(nil), nil
}

func appendLengthPrefixed(dst, value []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

type LegacyModerationRule struct {
	Keyword  string `json:"keyword"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Action   string `json:"action"`
	Enabled  bool   `json:"enabled"`
}
type LegacyModerationPolicy struct {
	Provider, BaseURL, Model, AuditScope                            string
	Thresholds                                                      map[string]float64
	Rules                                                           []LegacyModerationRule
	EngineMode                                                      string
	ModelFilters                                                    []string
	GroupFilters                                                    []int64
	FailurePolicy, AdapterVersion, ExtractorVersion, ChunkerVersion string
	FeedbackEpoch                                                   uint64
	Credential                                                      string
	CacheTTLSeconds, Workers, Retries                               int
}

func CanonicalLegacyModerationPolicyScope(in LegacyModerationPolicy) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimSpace(in.BaseURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", errors.New("invalid moderation base URL")
	}
	hostPath := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		hostPath += ":" + port
	}
	hostPath += strings.TrimRight(u.EscapedPath(), "/")
	rules := append([]LegacyModerationRule(nil), in.Rules...)
	for i := range rules {
		rules[i].Keyword = strings.TrimSpace(rules[i].Keyword)
		rules[i].Category = strings.ToLower(strings.TrimSpace(rules[i].Category))
		rules[i].Severity = strings.ToLower(strings.TrimSpace(rules[i].Severity))
		rules[i].Action = strings.ToLower(strings.TrimSpace(rules[i].Action))
	}
	sort.Slice(rules, func(i, j int) bool {
		a, _ := json.Marshal(rules[i])
		b, _ := json.Marshal(rules[j])
		return string(a) < string(b)
	})
	models := normalizeSortedStrings(in.ModelFilters)
	groups := append([]int64(nil), in.GroupFilters...)
	sort.Slice(groups, func(i, j int) bool { return groups[i] < groups[j] })
	payload := map[string]any{"adapter_version": strings.TrimSpace(in.AdapterVersion), "audit_scope": strings.ToLower(strings.TrimSpace(in.AuditScope)), "base_host_path": hostPath, "chunker_version": strings.TrimSpace(in.ChunkerVersion), "engine_mode": strings.ToLower(strings.TrimSpace(in.EngineMode)), "extractor_version": strings.TrimSpace(in.ExtractorVersion), "failure_policy": strings.ToLower(strings.TrimSpace(in.FailurePolicy)), "feedback_epoch": in.FeedbackEpoch, "group_filters": groups, "model": strings.TrimSpace(in.Model), "model_filters": models, "provider": strings.ToLower(strings.TrimSpace(in.Provider)), "rules": rules, "thresholds": in.Thresholds}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(canonical)
	return canonical, "legacy-v1:" + hex.EncodeToString(digest[:]), nil
}

func normalizeSortedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if v := strings.ToLower(strings.TrimSpace(value)); v != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
