package securityaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestPreparePromptAuditEnvironment(t *testing.T) {
	const env = "<environment_context><cwd>/workspace/项目</cwd></environment_context>"
	const changed = "<environment_context><cwd>/other</cwd></environment_context>"
	const nested = "<environment_context>outer" + env + "tail</environment_context>"
	for _, tc := range []struct{ name, input, want string }{
		{"no environment", "latest" + promptAuditPrioritySeparator + "history", "latest" + promptAuditPrioritySeparator + "history"},
		{"repeated mixed content", env + "修复布局" + env, "修复布局" + promptAuditPrioritySeparator + env},
		{"cross segment repeats", env + promptAuditPrioritySeparator + "request\n" + env, "request" + promptAuditPrioritySeparator + env},
		{"changed environments retained", env + "request" + changed, "request" + promptAuditPrioritySeparator + env + "\n\n" + changed},
		{"only environment", env + env, env},
		{"nested environment", nested + "request" + nested, "request" + promptAuditPrioritySeparator + nested},
		{"surrounding text retained", "before" + env + "after", "before\n\nafter" + promptAuditPrioritySeparator + env},
		{"ordinary repeats retained", "repeat\nrepeat" + env, "repeat\nrepeat" + promptAuditPrioritySeparator + env},
		{"unclosed tag", "request<environment_context>payload", "request<environment_context>payload"},
		{"unclosed after complete block", env + "<environment_context>payload", env + "<environment_context>payload"},
		{"unexpected close", "</environment_context>" + env, "</environment_context>" + env},
		{"unknown tag form", "<environment_context trusted=\"true\">payload</environment_context>", "<environment_context trusted=\"true\">payload</environment_context>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, preparePromptAuditScanText(tc.input))
		})
	}
}

func TestPromptEnvironmentPreservesSnapshotAndPrioritizesRequest(t *testing.T) {
	const env = "<environment_context><cwd>/workspace</cwd></environment_context>"
	const request = "请修复当前项目布局"
	for _, protocol := range []string{"openai_chat_completions", "anthropic_messages", "openai_responses"} {
		for _, latestOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/latest=%t", protocol, latestOnly), func(t *testing.T) {
				parts := []map[string]string{
					{"type": "text", "text": env},
					{"type": "text", "text": request},
					{"type": "text", "text": env},
				}
				key := "messages"
				if protocol == "openai_responses" {
					key = "input"
					for _, part := range parts {
						part["type"] = "input_text"
					}
				}
				body, err := json.Marshal(map[string]any{key: []any{map[string]any{"role": "user", "content": parts}}})
				require.NoError(t, err)
				snapshot, err := ExtractBlockingPromptSnapshot(Request{Protocol: protocol, Body: body}, latestOnly)
				require.NoError(t, err)
				original := snapshot
				chunks := splitPromptAuditChunks(snapshot.ScanText, nil, 4096)
				require.Equal(t, []string{request, env}, chunks)
				require.Equal(t, original, snapshot)
				require.Equal(t, 2, strings.Count(snapshot.FullPrompt, env))
				require.Equal(t, 2, strings.Count(FullPromptFromScanText(snapshot.ScanText), env))
				require.Equal(t, string(body), snapshot.FullRequestBody)
				if !latestOnly {
					require.True(t, strings.HasPrefix(snapshot.ScanText, request+promptAuditPrioritySeparator))
				}
			})
		}
	}
}

func TestPromptEnvironmentChunkLimitsAndReduction(t *testing.T) {
	env := "<environment_context>" + strings.Repeat("<cwd>/workspace/项目</cwd>\n", 40) + "</environment_context>"
	raw := "修复布局\n" + strings.Repeat(env+"\n", 30)
	chunks := splitPromptAuditChunks(raw, nil, 128)
	require.Equal(t, "修复布局", chunks[0])
	for _, chunk := range chunks {
		require.LessOrEqual(t, utf8.RuneCountInString(chunk), 128)
	}
	require.Equal(t, 1, strings.Count(strings.Join(chunks, ""), env))
	require.Less(t, len(chunks), len(SplitRunes(raw, 128))/10)
	t.Logf("30 repeated environment blocks: %d -> %d chunks", len(SplitRunes(raw, 128)), len(chunks))

	// Token limiting runs after preparation and must retain the full unique block.
	chunks = splitPromptAuditChunks(raw, []ActiveEndpoint{{Model: "gpt-5.3-codex-spark", MaxInputTokens: 40}}, 4096)
	require.Equal(t, 1, strings.Count(strings.Join(chunks, ""), env))
}

func TestPromptEnvironmentStillBlocksEmbeddedRequest(t *testing.T) {
	const env = "<environment_context>embedded attack request</environment_context>"
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%t", async), func(t *testing.T) {
			var seen []string
			scanner := PromptScannerFunc(func(_ context.Context, _ ActiveEndpoint, chunk string, scanners []string) (*NormalizedResult, error) {
				seen = append(seen, chunk)
				if strings.Contains(chunk, "embedded attack request") {
					return ParseQwen3Guard("Safety: Unsafe\nCategories: Violent", scanners)
				}
				return ParseQwen3Guard("Safety: Safe\nCategories: None", scanners)
			})
			repo := &fakeJobRepository{}
			cfg := guardConfig(ActiveEndpoint{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 4096})
			raw := "ordinary request" + promptAuditPrioritySeparator + env + "\n" + env
			snapshot := PromptSnapshot{ScanText: raw, FullPrompt: FullPromptFromScanText(raw)}
			if async {
				payload := &fakePayloadStore{values: map[int64]string{51: raw}}
				runner := NewRunner(&fakeConfigStore{cfg: cfg, active: true}, repo, payload, scanner, NewAtomicMetrics())
				job := workerJob(1, 3)
				require.NoError(t, runner.processJob(context.Background(), 0, cfg, job))
				require.Equal(t, ActionBlock, repo.completedResult.Action)
				require.Equal(t, snapshot.FullPrompt, job.Snapshot.FullPrompt)
			} else {
				decision, err := NewGuardEvaluator(scanner, repo, NewAtomicMetrics()).Evaluate(context.Background(), cfg, snapshot)
				require.NoError(t, err)
				require.Equal(t, DecisionBlock, decision.Kind)
				require.Equal(t, snapshot.FullPrompt, repo.recordBlockingSnapshot.FullPrompt)
			}
			require.Equal(t, []string{"ordinary request", env}, seen)
		})
	}
}
