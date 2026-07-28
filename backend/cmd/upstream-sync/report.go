package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func writeReport(report ImpactReport) error {
	if err := os.MkdirAll(report.OutputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(report.FullReport, append(data, '\n'), 0o644); err != nil {
		return err
	}
	summary := renderTextSummary(report)
	return os.WriteFile(filepath.Join(report.OutputDir, "summary.txt"), []byte(summary), 0o644)
}

func renderTextSummary(report ImpactReport) string {
	counts := map[string]int{"blocker": 0, "review": 0, "low": 0}
	for _, feature := range report.Features {
		counts[feature.Risk]++
	}
	return fmt.Sprintf(
		"upstream %s..%s: %d commits, %d files\n"+
			"local %s..%s: %d commits, %d files\n"+
			"overlap files: %d; affected features: %d (blocker=%d review=%d low=%d)\n"+
			"Codex context: %d bytes across %d feature packs\n"+
			"context overview: %s\nfull report: %s\n",
		shortSHA(report.Upstream.From), shortSHA(report.Upstream.To), report.Upstream.CommitCount, len(report.Upstream.Files),
		shortSHA(report.Local.From), shortSHA(report.Local.To), report.Local.CommitCount, len(report.Local.Files),
		len(report.OverlapFiles), len(report.Features), counts["blocker"], counts["review"], counts["low"],
		report.ContextBytes, len(report.CodexReview), report.OverviewPath, report.FullReport,
	)
}

func buildContextPacks(repoRoot string, report *ImpactReport, features []Feature) error {
	repo := gitRepo{root: repoRoot}
	featureByID := make(map[string]Feature)
	for _, feature := range features {
		featureByID[feature.ID] = feature
	}
	contextDir := filepath.Join(report.OutputDir, "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return err
	}
	var overview bytes.Buffer
	fmt.Fprintf(&overview, "# Upstream impact overview\n\n")
	fmt.Fprintf(&overview, "- Upstream: `%s..%s` (%d commits, %d files)\n", shortSHA(report.Upstream.From), shortSHA(report.Upstream.To), report.Upstream.CommitCount, len(report.Upstream.Files))
	fmt.Fprintf(&overview, "- Local: `%s..%s` (%d commits, %d files)\n", shortSHA(report.Local.From), shortSHA(report.Local.To), report.Local.CommitCount, len(report.Local.Files))
	fmt.Fprintf(&overview, "- Direct file overlap: %d\n", len(report.OverlapFiles))
	fmt.Fprintf(&overview, "- Virtual merge clean: %t\n", report.Simulation.Clean)
	fmt.Fprintf(&overview, "- Full machine report: `%s`\n\n", report.FullReport)
	fmt.Fprintf(&overview, "## Affected features\n\n")
	for impactIndex := range report.Features {
		impact := &report.Features[impactIndex]
		fmt.Fprintf(&overview, "- `%s` [%s] %s: %s\n", impact.ID, impact.Risk, impact.Name, strings.Join(impact.Reasons, "; "))
		if impact.Risk != "review" && impact.Risk != "blocker" {
			continue
		}
		feature := featureByID[impact.ID]
		pack := renderFeaturePack(repo, *report, feature, *impact)
		pack = trimBytes(pack, 24576)
		packPath := filepath.Join(contextDir, safeFilename(impact.ID)+".md")
		if err := os.WriteFile(packPath, []byte(pack), 0o644); err != nil {
			return err
		}
		impact.ContextPack = packPath
		if info, err := os.Stat(packPath); err == nil {
			report.ContextBytes += info.Size()
		}
	}
	overviewText := trimBytes(overview.String(), 8192)
	if err := os.WriteFile(report.OverviewPath, []byte(overviewText), 0o644); err != nil {
		return err
	}
	if info, err := os.Stat(report.OverviewPath); err == nil {
		report.ContextBytes += info.Size()
	}
	return nil
}

func renderFeaturePack(repo gitRepo, report ImpactReport, feature Feature, impact FeatureImpact) string {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# %s: %s\n\n", feature.ID, feature.Name)
	fmt.Fprintf(&out, "Risk: **%s**\n\n", impact.Risk)
	fmt.Fprintf(&out, "## Purpose\n\n%s\n\n", feature.Purpose)
	fmt.Fprintf(&out, "## Protected invariants\n\n")
	for _, invariant := range feature.Invariants {
		fmt.Fprintf(&out, "- %s\n", invariant)
	}
	fmt.Fprintf(&out, "\n## Why this feature was selected\n\n")
	for _, reason := range impact.Reasons {
		fmt.Fprintf(&out, "- %s\n", reason)
	}
	if len(impact.UpstreamSymbols) > 0 {
		fmt.Fprintf(&out, "\nUpstream symbols: `%s`\n", strings.Join(impact.UpstreamSymbols, "`, `"))
	}
	if len(impact.LocalSymbols) > 0 {
		fmt.Fprintf(&out, "\nLocal symbols: `%s`\n", strings.Join(impact.LocalSymbols, "`, `"))
	}
	if len(impact.Contracts) > 0 {
		fmt.Fprintf(&out, "\nContracts: `%s`\n", strings.Join(impact.Contracts, "`, `"))
	}
	fmt.Fprintf(&out, "\n## Bounded upstream changes\n\n")
	for _, changedPath := range impact.ContextFiles {
		diff := repo.diffSnippet(report.Upstream.From, report.Upstream.To, changedPath, 4096)
		if diff == "" {
			continue
		}
		fmt.Fprintf(&out, "### `%s`\n\n```diff\n%s\n```\n\n", changedPath, diff)
	}
	fmt.Fprintf(&out, "## Relevant base/local/upstream declarations\n\n")
	for _, changedPath := range impact.ContextFiles {
		if !featureMatchesPath(feature, changedPath) {
			continue
		}
		renderDeclarationSnippets(&out, repo, changedPath, report.Upstream.From, "base", feature.Symbols)
		renderDeclarationSnippets(&out, repo, changedPath, report.Local.To, "local", feature.Symbols)
		renderDeclarationSnippets(&out, repo, changedPath, report.Upstream.To, "upstream", feature.Symbols)
		if report.Simulation.Tree != "" {
			renderDeclarationSnippets(&out, repo, changedPath, report.Simulation.Tree, "merged", feature.Symbols)
		}
	}
	if len(impact.HistoricalRecords) > 0 {
		fmt.Fprintf(&out, "## Historical decisions\n\n")
		for _, record := range impact.HistoricalRecords {
			fmt.Fprintf(&out, "- %s\n", record)
		}
	}
	fmt.Fprintf(&out, "\n## Selected verification\n\n")
	for _, test := range impact.Tests {
		fmt.Fprintf(&out, "- `%s`\n", test.Command)
	}
	fmt.Fprintf(&out, "\n## Codex question\n\n")
	fmt.Fprintf(&out, "Does the bounded upstream change preserve every invariant above after the proposed merge, including call order and declared contracts? Identify any required combined implementation; do not inspect unrelated repository history unless an unresolved dependency is named here.\n")
	return out.String()
}

func renderDeclarationSnippets(out *bytes.Buffer, repo gitRepo, changedPath, ref, label string, anchors []string) {
	content, err := repo.content(ref, changedPath)
	if err != nil {
		return
	}
	index := extractSource(changedPath, repo.blob(ref, changedPath), content)
	anchorNames := normalizeSymbols(anchors)
	var snippets []string
	for _, symbol := range index.Symbols {
		if len(intersectNormalized([]string{symbol.Name}, anchorNames)) == 0 {
			continue
		}
		start := symbol.StartLine
		end := symbol.EndLine
		if end-start+1 > 80 {
			end = start + 79
		}
		snippets = append(snippets, fmt.Sprintf("// %s:%d\n%s", changedPath, start, sourceLines(content, start, end)))
	}
	if len(snippets) == 0 {
		return
	}
	fmt.Fprintf(out, "### %s `%s@%s`\n\n```%s\n%s\n```\n\n", label, changedPath, shortSHA(ref), fenceLanguage(changedPath), strings.Join(snippets, "\n\n"))
}

func fenceLanguage(path string) string {
	switch filepath.Ext(path) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".vue":
		return "vue"
	case ".sql":
		return "sql"
	default:
		return "text"
	}
}

func safeFilename(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func readImpact(path string) (ImpactReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ImpactReport{}, err
	}
	var report ImpactReport
	err = json.Unmarshal(data, &report)
	return report, err
}

func latestImpact(repoRoot string) (string, error) {
	repo := gitRepo{root: repoRoot}
	gitDir, err := repo.run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	matches, err := filepath.Glob(filepath.Join(gitDir, "upstream-sync", "runs", "*", "impact.json"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no impact report found; run impact analyze first")
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}
