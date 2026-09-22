package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexHTTPThreadEpochGraphHistory(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			account := newTestOAuthAccount(7630, map[string]any{codexFingerprintModeExtraKey: "session"})
			account.Credentials = map[string]any{"chatgpt_account_id": "thread-epoch-account"}
			seed, _ := codexFingerprintSeed(account.Extra)
			period := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), time.Now())
			boundary := period.expiresAt.Add(-codexSessionPeriodGrace)
			before, after := boundary.Add(-time.Second), boundary.Add(time.Second)
			forward := func(fixture string, now time.Time) codexTopologyOutbound {
				t.Helper()
				// Every request uses a new gateway instance and the shared durable
				// store, so no in-process state can preserve the graph by accident.
				return buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, fixture, now, 1, 11, passthrough)
			}
			assertParent := func(child, parent codexTopologyOutbound) {
				t.Helper()
				require.Equal(t, parent.session(), child.session())
				require.Equal(t, parent.cache(), child.cache())
				require.NotEqual(t, parent.thread(), child.thread())
				require.Equal(t, parent.thread(), child.headers.Get("x-codex-parent-thread-id"))
				require.Equal(t, parent.thread(), child.body.Get("client_metadata.x-codex-parent-thread-id").String())
				for _, carrier := range child.carriers() {
					require.Equal(t, parent.thread(), carrier.Get("parent_thread_id").String())
				}
			}
			assertFork := func(side, source codexTopologyOutbound) {
				t.Helper()
				require.NotEqual(t, source.session(), side.session())
				require.NotEqual(t, source.thread(), side.thread())
				require.NotEqual(t, source.cache(), side.cache())
				for _, carrier := range side.carriers() {
					require.Equal(t, source.thread(), carrier.Get("forked_from_thread_id").String())
				}
				require.Equal(t, int64(33), side.carriers()[2].Get("forked_from_ordinal_exclusive").Int())
			}
			assertSameIdentity := func(previous, current codexTopologyOutbound) {
				t.Helper()
				require.Equal(t, previous.session(), current.session())
				require.Equal(t, previous.thread(), current.thread())
				require.Equal(t, previous.cache(), current.cache())
			}

			root1 := forward(codexRootTopology, before)
			child1 := forward(codexChildTopology, before)
			assertParent(child1, root1)
			side1 := forward(codexSideTopology, before)
			sideChild1 := forward(codexSideChildTopology, before)
			assertFork(side1, root1)
			assertParent(sideChild1, side1)

			// A new side can cross the period boundary before the ordinary
			// parent is revisited. Its fork must target the actual old thread.
			sideBeforeRootFixture := strings.ReplaceAll(codexSideTopology, "000000000003", "000000000023")
			sideBeforeRoot := forward(sideBeforeRootFixture, after)
			assertFork(sideBeforeRoot, root1)
			require.NotEqual(t, side1.session(), sideBeforeRoot.session())

			// Ordinary tasks belong to their new period session, including a
			// new thread namespace and a new shared root/child cache root.
			root2 := forward(codexRootTopology, after)
			child2 := forward(codexChildTopology, after)
			assertParent(child2, root2)
			for _, pair := range [][2]codexTopologyOutbound{{root1, root2}, {child1, child2}} {
				require.NotEqual(t, pair[0].session(), pair[1].session())
				require.NotEqual(t, pair[0].thread(), pair[1].thread())
				require.NotEqual(t, pair[0].cache(), pair[1].cache())
			}
			sideAfterRootFixture := strings.ReplaceAll(codexSideTopology, "000000000003", "000000000033")
			sideAfterRoot := forward(sideAfterRootFixture, after)
			assertFork(sideAfterRoot, root2)
			require.NotEqual(t, sideBeforeRoot.session(), sideAfterRoot.session())

			// Existing side trees are independent of the ordinary period. An
			// explicit fork marker on a retry cannot move a side to root2.
			side1Retry := forward(codexSideTopology, after)
			assertSameIdentity(side1, side1Retry)
			assertFork(side1Retry, root1)
			sideBeforeRootRetry := forward(sideBeforeRootFixture, after)
			assertSameIdentity(sideBeforeRoot, sideBeforeRootRetry)
			assertFork(sideBeforeRootRetry, root1)
			assertSameIdentity(side1, forward(codexSideContinuationTopology, after))
			sideChild2 := forward(codexSideChildTopology, after)
			assertSameIdentity(sideChild1, sideChild2)
			assertParent(sideChild2, side1)

			// Resolve an old-epoch request after root2 was registered. This
			// deliberately reverses processing order, as a delayed attempt can,
			// and must not regress the latest historical fork reference.
			late := newCodexSessionIdentityV2Context(t, 1, 99)
			stageCodexSessionIdentityInputMap(late, map[string]any{
				"client_metadata": map[string]any{openAIWSTurnMetadataHeader: codexRootTopology},
			})
			lateIDs, err := newCodexPeriodRedisService(t, server).resolveCodexHTTPFingerprintIDs(context.Background(), late, account, before)
			require.NoError(t, err)
			require.Equal(t, root1.session(), lateIDs.sessionID)
			require.Equal(t, root1.thread(), lateIDs.threadID)
			sideAfterLateFixture := strings.ReplaceAll(codexSideTopology, "000000000003", "000000000043")
			sideAfterLate := forward(sideAfterLateFixture, after)
			assertFork(sideAfterLate, root2)
		})
	}
}

func TestCodexHTTPThreadEpochRetainsExistingSideEvidence(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			account := newTestOAuthAccount(7632, map[string]any{codexFingerprintModeExtraKey: "session"})
			accountScope := codexSessionIdentityUpstreamScope(account)
			rawRoot := gjson.Get(codexRootTopology, "thread_id").String()
			rawSide := gjson.Get(codexSideTopology, "session_id").String()
			rawChild := gjson.Get(codexSideChildTopology, "thread_id").String()
			oldRoot, oldSide, oldChild := uuid.NewString(), uuid.NewString(), uuid.NewString()
			sideSession := uuid.Must(uuid.NewV7()).String()
			for key, value := range map[string]string{
				codexHTTPIdentityMappingKey("thread", "user:1", accountScope, rawRoot):       oldRoot,
				codexHTTPIdentityMappingKey("thread", "user:1", accountScope, rawSide):       oldSide,
				codexHTTPIdentityMappingKey("thread", "user:1", accountScope, rawChild):      oldChild,
				codexHTTPIdentityMappingKey("side-session", "user:1", accountScope, rawSide): sideSession,
			} {
				require.NoError(t, server.Set("openai_codex_session_identity:"+key, value))
			}
			now := time.Now()
			forward := func(fixture string) codexTopologyOutbound {
				return buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, fixture, now, 1, 99, passthrough)
			}
			// Older records still prove a fork target but cannot choose an
			// ordinary thread in the newly selected session namespace.
			currentRoot := forward(codexRootTopology)
			require.NotEqual(t, oldRoot, currentRoot.thread())
			continued := forward(codexSideContinuationTopology)
			require.Equal(t, sideSession, continued.session())
			require.Equal(t, oldSide, continued.thread())
			child := forward(codexSideChildTopology)
			require.Equal(t, sideSession, child.session())
			require.Equal(t, oldChild, child.thread())
			require.Equal(t, oldSide, child.headers.Get("x-codex-parent-thread-id"))
			sideRetry := forward(codexSideTopology)
			require.Equal(t, oldRoot, sideRetry.body.Get("client_metadata.forked_from_thread_id").String())
			newSide := forward(strings.ReplaceAll(codexSideTopology, "000000000003", "000000000053"))
			require.Equal(t, currentRoot.thread(), newSide.body.Get("client_metadata.forked_from_thread_id").String())
		})
	}
}

func TestCodexHTTPThreadEpochCurrentRecordsExpireButHistorySurvives(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	account := newTestOAuthAccount(7633, map[string]any{codexFingerprintModeExtraKey: "session"})
	now := time.Now()
	root := buildCodexTopologyAt(t, svc, account, codexRootTopology, now, 1, 11, false)
	seed, _ := codexFingerprintSeed(account.Extra)
	accountScope := codexSessionIdentityUpstreamScope(account)
	period := resolveCodexSessionPeriod(seed, "user:1", accountScope, now)
	raw := gjson.Get(codexRootTopology, "thread_id").String()
	currentKey := "openai_codex_session_identity:" + codexHTTPThreadKey("thread-current", "user:1", accountScope, root.session(), raw)
	historyKey := "openai_codex_session_identity:" + codexHTTPThreadKey("thread-history", "user:1", accountScope, "", raw)
	ttl := server.TTL(currentKey)
	require.InDelta(t, period.expiresAt.Sub(now).Milliseconds(), ttl.Milliseconds(), 1)
	require.Greater(t, server.TTL(historyKey), 179*24*time.Hour)
	server.FastForward(time.Minute)
	buildCodexTopologyAt(t, svc, account, codexRootTopology, now.Add(time.Minute), 1, 22, false)
	require.Equal(t, ttl-time.Minute, server.TTL(currentKey), "current-thread presence never slides the period deadline")
	server.FastForward(ttl)
	require.False(t, server.Exists(currentKey))
	require.True(t, server.Exists(historyKey), "historical fork evidence is independent of current-session presence")
}

func TestCodexHTTPThreadEpochNewSideCannotImportUnrelatedParent(t *testing.T) {
	server := miniredis.RunT(t)
	svc := newCodexPeriodRedisService(t, server)
	account := newTestOAuthAccount(7634, map[string]any{codexFingerprintModeExtraKey: "session"})
	now := time.Now()
	root := buildCodexTopologyAt(t, svc, account, codexRootTopology, now, 1, 11, false)
	rawRoot := gjson.Get(codexRootTopology, "thread_id").String()
	key := codexHTTPIdentityMappingKey("thread", "user:1", codexSessionIdentityUpstreamScope(account), rawRoot)
	require.NoError(t, server.Set("openai_codex_session_identity:"+key, root.thread()))
	before := server.Keys()
	c := newCodexSessionIdentityV2Context(t, 1, 11)
	stageCodexSessionIdentityInputMap(c, map[string]any{
		"client_metadata": map[string]any{
			"session_id": "new-side", "thread_id": "new-side",
			"parent_thread_id": rawRoot, "forked_from_thread_id": rawRoot,
			"forked_from_ordinal_exclusive": 33,
		},
	})
	_, err := svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
	require.ErrorIs(t, err, ErrCodexSessionIdentityNotFound)
	require.Equal(t, before, server.Keys(), "invalid side root must not leave side registration or a pinned fork")

	buildCodexTopologyAt(t, svc, account, codexSideTopology, now, 1, 11, false)
	c = newCodexSessionIdentityV2Context(t, 1, 11)
	stageCodexSessionIdentityInputMap(c, map[string]any{
		"client_metadata": map[string]any{
			"session_id": gjson.Get(codexSideTopology, "session_id").String(),
			"thread_id":  "invalid-child", "parent_thread_id": rawRoot,
		},
	})
	_, err = svc.resolveCodexHTTPFingerprintIDs(context.Background(), c, account, now)
	require.ErrorIs(t, err, ErrCodexSessionIdentityNotFound, "new side parent must belong to that side, even when v3 knows the raw ID")
}

func TestCodexHTTPThreadEpochChildRequiresCurrentParent(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			server := miniredis.RunT(t)
			account := newTestOAuthAccount(7631, map[string]any{codexFingerprintModeExtraKey: "session"})
			seed, _ := codexFingerprintSeed(account.Extra)
			period := resolveCodexSessionPeriod(seed, "user:1", codexSessionIdentityUpstreamScope(account), time.Now())
			boundary := period.expiresAt.Add(-codexSessionPeriodGrace)
			before, after := boundary.Add(-time.Second), boundary.Add(time.Second)
			forward := func(fixture string, now time.Time) codexTopologyOutbound {
				t.Helper()
				return buildCodexTopologyAt(t, newCodexPeriodRedisService(t, server), account, fixture, now, 1, 11, passthrough)
			}
			root1 := forward(codexRootTopology, before)
			child1 := forward(codexChildTopology, before)
			require.Equal(t, root1.thread(), child1.headers.Get("x-codex-parent-thread-id"))

			// A historical fork target is not an ordinary parent in the new
			// session. Do not silently attach this child to the old session tree.
			c := newCodexSessionIdentityV2Context(t, 1, 99)
			stageCodexSessionIdentityInputMap(c, map[string]any{
				"client_metadata": map[string]any{openAIWSTurnMetadataHeader: codexChildTopology},
			})
			ids, err := newCodexPeriodRedisService(t, server).resolveCodexHTTPFingerprintIDs(context.Background(), c, account, after)
			require.ErrorIs(t, err, ErrCodexSessionIdentityNotFound)
			require.Nil(t, ids)

			root2 := forward(codexRootTopology, after)
			child2 := forward(codexChildTopology, after)
			require.NotEqual(t, root1.session(), root2.session())
			require.NotEqual(t, root1.thread(), root2.thread())
			require.NotEqual(t, child1.thread(), child2.thread())
			require.Equal(t, root2.session(), child2.session())
			require.Equal(t, root2.cache(), child2.cache())
			require.Equal(t, root2.thread(), child2.headers.Get("x-codex-parent-thread-id"))
			for _, carrier := range child2.carriers() {
				require.Equal(t, root2.thread(), carrier.Get("parent_thread_id").String())
			}
		})
	}
}
