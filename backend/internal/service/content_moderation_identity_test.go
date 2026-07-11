package service

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModerationIdentityGolden(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	in := ModerationIdentityInput{KeyVersion: 7, FeedbackEpoch: 9, Provider: "zhipu", Model: "moderation", AuditScope: "all_context", PolicyScope: "legacy-v1:abc", ChunkerVersion: "zhipu-text-v1", ContextFrame: []byte{0, 1, 2}, NormalizedText: "中😀"}
	message, digest, err := BuildModerationChunkIdentity(key, in)
	require.NoError(t, err)
	require.Equal(t, "0000000000000007000000057a686970750000000a6d6f6465726174696f6e0000000b616c6c5f636f6e746578740000000d6c65676163792d76313a6162630000000d7a686970752d746578742d763100000000000000090000000300010200000007e4b8adf09f9880", hex.EncodeToString(message))
	require.Equal(t, "94830ee88dcde5a6b39a33077568762e101498701965562c7f3ae7d627c0b460", hex.EncodeToString(digest))
}

func TestCanonicalLegacyModerationPolicyScopeDeterministicAndExcludesOperations(t *testing.T) {
	a := LegacyModerationPolicy{Provider: " zhipu ", BaseURL: "https://Example.COM/paas/v4/", Model: " moderation ", AuditScope: " ALL_CONTEXT ", Thresholds: map[string]float64{"b": .90, "a": .1}, Rules: []LegacyModerationRule{{Keyword: " Z ", Action: "BLOCK"}, {Keyword: "a", Action: "review"}}, EngineMode: " HYBRID ", ModelFilters: []string{" z ", "a"}, GroupFilters: []int64{9, 2}, FailurePolicy: " CLOSED ", AdapterVersion: "zhipu-v1", ExtractorVersion: "extract-v1", ChunkerVersion: "zhipu-text-v1", FeedbackEpoch: 4, Credential: "secret", CacheTTLSeconds: 2, Workers: 3, Retries: 4}
	b := a
	b.Credential, b.CacheTTLSeconds, b.Workers, b.Retries = "other", 999, 99, 99
	b.Thresholds = map[string]float64{"a": .10, "b": .9}
	b.Rules = []LegacyModerationRule{a.Rules[1], a.Rules[0]}
	jsonA, scopeA, err := CanonicalLegacyModerationPolicyScope(a)
	require.NoError(t, err)
	jsonB, scopeB, err := CanonicalLegacyModerationPolicyScope(b)
	require.NoError(t, err)
	require.Equal(t, jsonA, jsonB)
	require.Equal(t, scopeA, scopeB)
	require.Equal(t, `{"adapter_version":"zhipu-v1","audit_scope":"all_context","base_host_path":"example.com/paas/v4","chunker_version":"zhipu-text-v1","engine_mode":"hybrid","extractor_version":"extract-v1","failure_policy":"closed","feedback_epoch":4,"group_filters":[2,9],"model":"moderation","model_filters":["a","z"],"provider":"zhipu","rules":[{"keyword":"Z","category":"","severity":"","action":"block","enabled":false},{"keyword":"a","category":"","severity":"","action":"review","enabled":false}],"thresholds":{"a":0.1,"b":0.9}}`, string(jsonA))
	require.Equal(t, "legacy-v1:fb65c13756a9f5f8c78023e50122fcd6d6f4ddf5ee99166efab060d24e6d1106", scopeA)
	require.NotContains(t, string(jsonA), "secret")
}
