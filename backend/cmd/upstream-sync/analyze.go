package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type analyzeOptions struct {
	UpstreamFrom string
	UpstreamTo   string
	LocalFrom    string
	LocalTo      string
	Simulate     bool
	BuildContext bool
	OutputDir    string
}

func analyzeImpact(repoRoot string, opts analyzeOptions) (ImpactReport, error) {
	repo := gitRepo{root: repoRoot}
	baseline, err := loadBaseline(repoRoot)
	if err != nil {
		return ImpactReport{}, err
	}
	if opts.UpstreamFrom == "" {
		opts.UpstreamFrom = baseline.VerifiedUpstream
	}
	if opts.UpstreamTo == "" {
		opts.UpstreamTo = baseline.TargetUpstream
	}
	if opts.LocalFrom == "" {
		opts.LocalFrom = baseline.MergeCommit
	}
	if opts.LocalTo == "" {
		opts.LocalTo = baseline.ProductHead
	}
	for label, ref := range map[string]string{
		"upstream from": opts.UpstreamFrom,
		"upstream to":   opts.UpstreamTo,
		"local from":    opts.LocalFrom,
		"local to":      opts.LocalTo,
	} {
		resolved, resolveErr := repo.resolve(ref)
		if resolveErr != nil {
			return ImpactReport{}, fmt.Errorf("resolve %s %q: %w", label, ref, resolveErr)
		}
		switch label {
		case "upstream from":
			opts.UpstreamFrom = resolved
		case "upstream to":
			opts.UpstreamTo = resolved
		case "local from":
			opts.LocalFrom = resolved
		case "local to":
			opts.LocalTo = resolved
		}
	}
	if !repo.ancestor(opts.UpstreamFrom, opts.UpstreamTo) {
		return ImpactReport{}, fmt.Errorf("verified upstream %s is not an ancestor of target %s; full-history fallback required", shortSHA(opts.UpstreamFrom), shortSHA(opts.UpstreamTo))
	}
	features, err := loadFeatures(repoRoot)
	if err != nil {
		return ImpactReport{}, err
	}
	idx, err := newIndexer(repo)
	if err != nil {
		return ImpactReport{}, err
	}
	upstream, err := analyzeRange(repo, idx, opts.UpstreamFrom, opts.UpstreamTo)
	if err != nil {
		return ImpactReport{}, fmt.Errorf("upstream range: %w", err)
	}
	local, err := analyzeRange(repo, idx, opts.LocalFrom, opts.LocalTo)
	if err != nil {
		return ImpactReport{}, fmt.Errorf("local range: %w", err)
	}
	upstream.ParsedBlobs = idx.parsed
	upstream.ReusedBlobs = idx.reused

	simulation := Simulation{Clean: true}
	if opts.Simulate {
		simulation = repo.simulate(opts.LocalTo, opts.UpstreamTo)
	}
	impacts, overlap := matchFeatures(repo, idx, features, upstream, local, opts.LocalTo, simulation)
	tests := collectTests(impacts)
	var lowRisk, codex []string
	for _, impact := range impacts {
		if impact.Risk == "low" {
			lowRisk = append(lowRisk, impact.ID)
		}
		if impact.Risk == "review" || impact.Risk == "blocker" {
			codex = append(codex, impact.ID)
		}
	}
	baseline.VerifiedUpstream = opts.UpstreamFrom
	baseline.TargetUpstream = opts.UpstreamTo
	baseline.MergeCommit = opts.LocalFrom
	baseline.ProductHead = opts.LocalTo
	report := ImpactReport{
		SchemaVersion: 1,
		Analyzer:      analyzerVersion,
		CreatedAt:     nowRFC3339(),
		Baseline:      baseline,
		Upstream:      upstream,
		Local:         local,
		OverlapFiles:  overlap,
		Features:      impacts,
		Tests:         tests,
		AutoLowRisk:   unique(lowRisk),
		CodexReview:   unique(codex),
		Simulation:    simulation,
	}
	if opts.OutputDir == "" {
		opts.OutputDir, err = newRunDir(repo)
		if err != nil {
			return ImpactReport{}, err
		}
	}
	report.OutputDir = opts.OutputDir
	report.FullReport = filepath.Join(opts.OutputDir, "impact.json")
	report.OverviewPath = filepath.Join(opts.OutputDir, "context", "overview.md")
	if opts.BuildContext {
		if err := buildContextPacks(repoRoot, &report, features); err != nil {
			return ImpactReport{}, err
		}
	}
	if err := writeReport(report); err != nil {
		return ImpactReport{}, err
	}
	return report, nil
}

func analyzeRange(repo gitRepo, idx *indexer, from, to string) (RangeSummary, error) {
	files, err := repo.changedFiles(from, to)
	if err != nil {
		return RangeSummary{}, err
	}
	summary := RangeSummary{
		From:        from,
		To:          to,
		CommitCount: repo.countCommits(from, to),
		Files:       files,
	}
	for fileIndex := range summary.Files {
		file := &summary.Files[fileIndex]
		switch file.Status {
		case "A":
			summary.Added++
		case "D":
			summary.Deleted++
		case "R", "C":
			summary.Renamed++
		default:
			summary.Modified++
		}
		oldPath := file.Path
		if file.OldPath != "" {
			oldPath = file.OldPath
		}
		oldIndex, _ := idx.atBlob(from, oldPath, file.OldBlob)
		newIndex, _ := idx.atBlob(to, file.Path, file.NewBlob)
		file.ChangedSymbols, file.AddedSymbols, file.DeletedSymbols, file.Contracts = compareIndexes(oldIndex, newIndex)
	}
	return summary, nil
}

func matchFeatures(repo gitRepo, idx *indexer, features []Feature, upstream, local RangeSummary, localRef string, simulation Simulation) ([]FeatureImpact, []string) {
	localByPath := make(map[string]ChangedFile)
	for _, file := range local.Files {
		localByPath[file.Path] = file
		if file.OldPath != "" {
			localByPath[file.OldPath] = file
		}
	}
	var overlap []string
	for _, file := range upstream.Files {
		if _, ok := localByPath[file.Path]; ok {
			overlap = append(overlap, file.Path)
		}
		if file.OldPath != "" {
			if _, ok := localByPath[file.OldPath]; ok {
				overlap = append(overlap, file.OldPath)
			}
		}
	}

	decisions := loadDecisions(repo.root)
	var impacts []FeatureImpact
	for _, feature := range features {
		impact := FeatureImpact{
			ID:   feature.ID,
			Name: feature.Name,
			Risk: "none",
		}
		featureContractSet := featureContracts(feature)
		anchorNames := normalizeSymbols(feature.Symbols)
		for _, upstreamFile := range upstream.Files {
			pathMatch := featureMatchesPath(feature, upstreamFile.Path) || featureMatchesPath(feature, upstreamFile.OldPath)
			symbolHits := intersectNormalized(append(append([]string{}, upstreamFile.ChangedSymbols...), upstreamFile.DeletedSymbols...), anchorNames)
			contractHits := matchContracts(upstreamFile.Contracts, featureContractSet)
			localFile, localOverlap := localByPath[upstreamFile.Path]
			contextFile := false
			sharedSymbols := []string{}
			if localOverlap {
				sharedSymbols = intersectNormalized(
					append(append([]string{}, upstreamFile.ChangedSymbols...), upstreamFile.DeletedSymbols...),
					append(append([]string{}, localFile.ChangedSymbols...), localFile.DeletedSymbols...),
				)
			}
			if !pathMatch && len(contractHits) == 0 && len(symbolHits) == 0 {
				if featureDependsOnChangedSymbol(repo, idx, feature, localRef, upstreamFile.ChangedSymbols) {
					impact.Risk = maxRisk(impact.Risk, "review")
					impact.Reasons = append(impact.Reasons, "one-hop symbol dependency changed: "+upstreamFile.Path)
					impact.Files = append(impact.Files, upstreamFile.Path)
					impact.ContextFiles = append(impact.ContextFiles, upstreamFile.Path)
					impact.UpstreamSymbols = append(impact.UpstreamSymbols, upstreamFile.ChangedSymbols...)
				}
				continue
			}
			impact.Files = append(impact.Files, upstreamFile.Path)
			impact.UpstreamSymbols = append(impact.UpstreamSymbols, symbolHits...)
			impact.Contracts = append(impact.Contracts, contractHits...)
			switch {
			case len(sharedSymbols) > 0:
				impact.Risk = maxRisk(impact.Risk, "blocker")
				contextFile = true
				impact.Reasons = append(impact.Reasons, "upstream and local delta changed the same symbol: "+strings.Join(sharedSymbols, ", "))
				impact.LocalSymbols = append(impact.LocalSymbols, sharedSymbols...)
			case hasDeletedAnchor(upstreamFile.DeletedSymbols, anchorNames):
				impact.Risk = maxRisk(impact.Risk, "blocker")
				contextFile = true
				impact.Reasons = append(impact.Reasons, "declared local anchor was deleted: "+strings.Join(intersectNormalized(upstreamFile.DeletedSymbols, anchorNames), ", "))
			case upstreamFile.Status == "D" && pathMatch:
				impact.Risk = maxRisk(impact.Risk, "blocker")
				contextFile = true
				impact.Reasons = append(impact.Reasons, "declared feature path was deleted: "+upstreamFile.Path)
			case upstreamFile.Status == "R" && pathMatch:
				impact.Risk = maxRisk(impact.Risk, "review")
				contextFile = true
				impact.Reasons = append(impact.Reasons, "declared feature path was renamed: "+upstreamFile.OldPath+" -> "+upstreamFile.Path)
			case len(contractHits) > 0:
				level := "review"
				if criticalContractHit(contractHits) {
					level = "blocker"
				}
				impact.Risk = maxRisk(impact.Risk, level)
				contextFile = true
				impact.Reasons = append(impact.Reasons, "declared contract changed: "+strings.Join(contractHits, ", "))
			case len(symbolHits) > 0:
				impact.Risk = maxRisk(impact.Risk, "review")
				contextFile = true
				impact.Reasons = append(impact.Reasons, "declared symbol changed: "+strings.Join(symbolHits, ", "))
			case pathMatch:
				impact.Risk = maxRisk(impact.Risk, "low")
				impact.Reasons = append(impact.Reasons, "same feature file but no declared symbol or contract intersection: "+upstreamFile.Path)
			}
			if contextFile {
				impact.ContextFiles = append(impact.ContextFiles, upstreamFile.Path)
			}
		}
		for _, conflict := range simulation.Conflicts {
			conflictPath := conflictFile(conflict)
			for _, featurePath := range feature.Paths {
				if conflictPath != "" && matchPathPattern(featurePath, conflictPath) {
					impact.Risk = "blocker"
					impact.Reasons = append(impact.Reasons, "virtual merge conflict intersects feature: "+conflict)
					impact.ContextFiles = append(impact.ContextFiles, conflictPath)
				}
			}
		}
		if impact.Risk == "none" {
			continue
		}
		impact.Reasons = unique(impact.Reasons)
		impact.Files = unique(impact.Files)
		impact.ContextFiles = unique(impact.ContextFiles)
		impact.UpstreamSymbols = unique(impact.UpstreamSymbols)
		impact.LocalSymbols = unique(impact.LocalSymbols)
		impact.Contracts = unique(impact.Contracts)
		impact.Tests = feature.Tests
		for _, decision := range decisions {
			if decision.FeatureID == feature.ID {
				exact := decision.UpstreamSymbolFingerprint == hashText(strings.Join(impact.UpstreamSymbols, "\n")) &&
					decision.LocalSymbolFingerprint == hashText(strings.Join(impact.LocalSymbols, "\n")) &&
					decision.InvariantVersion == hashText(strings.Join(feature.Invariants, "\n")) &&
					decision.AnalyzerVersion == analyzerVersion
				reuse := "prior"
				if exact {
					reuse = "exact-fingerprint"
				}
				impact.HistoricalRecords = append(impact.HistoricalRecords, reuse+" "+decision.ID+": "+decision.Resolution)
			}
		}
		impact.HistoricalRecords = unique(impact.HistoricalRecords)
		impacts = append(impacts, impact)
	}
	sort.Slice(impacts, func(i, j int) bool {
		left, right := riskRank(impacts[i].Risk), riskRank(impacts[j].Risk)
		if left == right {
			return impacts[i].ID < impacts[j].ID
		}
		return left > right
	})
	return impacts, unique(overlap)
}

func featureContracts(feature Feature) map[string][]string {
	return map[string][]string{
		"route":    feature.Routes,
		"key":      feature.ConfigKeys,
		"env":      feature.Environment,
		"json":     feature.JSONFields,
		"field":    append(append([]string{}, feature.JSONFields...), feature.APIFields...),
		"database": feature.DatabaseFields,
		"table":    feature.DatabaseFields,
		"i18n":     feature.I18nKeys,
		"api":      feature.Routes,
	}
}

func matchContracts(changed, declared map[string][]string) []string {
	var hits []string
	for kind, values := range changed {
		for _, value := range values {
			for _, declaredKind := range compatibleContractKinds(kind) {
				for _, expected := range declared[declaredKind] {
					if contractEquivalent(value, expected, kind) {
						hits = append(hits, kind+":"+value)
					}
				}
			}
		}
	}
	return unique(hits)
}

func contractEquivalent(actual, expected, kind string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	expected = strings.ToLower(strings.TrimSpace(expected))
	if actual == expected {
		return true
	}
	if kind == "i18n" || kind == "route" || kind == "api" || kind == "env" || kind == "key" {
		return false
	}
	actualTail := actual[strings.LastIndex(actual, ".")+1:]
	expectedTail := expected[strings.LastIndex(expected, ".")+1:]
	return len(actualTail) >= 4 && actualTail == expectedTail
}

func compatibleContractKinds(kind string) []string {
	switch kind {
	case "json", "field", "database", "table", "prop":
		return []string{"json", "field", "database", "table"}
	case "route", "api":
		return []string{"route", "api"}
	default:
		return []string{kind}
	}
}

func normalizeSymbols(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if index := strings.Index(value, ":"); index >= 0 {
			value = value[index+1:]
		}
		value = strings.TrimPrefix(value, "*")
		out = append(out, value)
		if index := strings.LastIndex(value, "."); index >= 0 {
			out = append(out, value[index+1:])
		}
	}
	return unique(out)
}

func intersectNormalized(left, right []string) []string {
	rightSet := make(map[string]struct{})
	for _, value := range normalizeSymbols(right) {
		rightSet[value] = struct{}{}
	}
	var out []string
	for _, value := range normalizeSymbols(left) {
		if _, ok := rightSet[value]; ok {
			out = append(out, value)
		}
	}
	return unique(out)
}

func featureMatchesPath(feature Feature, changedPath string) bool {
	if changedPath == "" {
		return false
	}
	for _, pattern := range feature.Paths {
		if matchPathPattern(pattern, changedPath) {
			return true
		}
	}
	return false
}

func matchPathPattern(patternValue, value string) bool {
	patternValue = filepath.ToSlash(patternValue)
	value = filepath.ToSlash(value)
	if patternValue == value {
		return true
	}
	if strings.Contains(patternValue, "**") {
		var expression strings.Builder
		expression.WriteString("^")
		for index := 0; index < len(patternValue); {
			switch {
			case strings.HasPrefix(patternValue[index:], "**/"):
				expression.WriteString("(?:.*/)?")
				index += 3
			case strings.HasPrefix(patternValue[index:], "**"):
				expression.WriteString(".*")
				index += 2
			case patternValue[index] == '*':
				expression.WriteString("[^/]*")
				index++
			case patternValue[index] == '?':
				expression.WriteString("[^/]")
				index++
			default:
				expression.WriteString(regexp.QuoteMeta(patternValue[index : index+1]))
				index++
			}
		}
		expression.WriteString("$")
		return regexp.MustCompile(expression.String()).MatchString(value)
	}
	matched, _ := path.Match(patternValue, value)
	return matched
}

func featureDependsOnChangedSymbol(repo gitRepo, idx *indexer, feature Feature, ref string, changed []string) bool {
	if len(changed) == 0 {
		return false
	}
	changedNames := normalizeSymbols(changed)
	anchorNames := normalizeSymbols(feature.Symbols)
	for _, featurePath := range feature.Paths {
		if strings.ContainsAny(featurePath, "*?[") {
			continue
		}
		index, err := idx.at(ref, featurePath)
		if err != nil {
			continue
		}
		for _, symbol := range index.Symbols {
			if len(intersectNormalized([]string{symbol.Name}, anchorNames)) == 0 {
				continue
			}
			if len(intersectNormalized(append(symbol.Calls, symbol.Refs...), changedNames)) > 0 {
				return true
			}
		}
	}
	return false
}

func hasDeletedAnchor(deleted, anchors []string) bool {
	return len(intersectNormalized(deleted, anchors)) > 0
}

func criticalContractHit(hits []string) bool {
	for _, hit := range hits {
		if strings.HasPrefix(hit, "route:") ||
			strings.HasPrefix(hit, "json:") ||
			strings.HasPrefix(hit, "database:") ||
			strings.HasPrefix(hit, "table:") ||
			strings.HasPrefix(hit, "env:") {
			return true
		}
	}
	return false
}

func collectTests(impacts []FeatureImpact) []FeatureTest {
	seen := make(map[string]struct{})
	var tests []FeatureTest
	for _, impact := range impacts {
		if impact.Risk == "none" {
			continue
		}
		for _, test := range impact.Tests {
			if _, ok := seen[test.Command]; ok {
				continue
			}
			seen[test.Command] = struct{}{}
			tests = append(tests, test)
		}
	}
	return tests
}

func riskRank(risk string) int {
	switch risk {
	case "blocker":
		return 4
	case "review":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

func maxRisk(left, right string) string {
	if riskRank(right) > riskRank(left) {
		return right
	}
	return left
}

func newRunDir(repo gitRepo) (string, error) {
	gitDir, err := repo.run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repo.root, gitDir)
	}
	dir := filepath.Join(gitDir, "upstream-sync", "runs", strings.ReplaceAll(nowRFC3339(), ":", ""))
	if err := os.MkdirAll(filepath.Join(dir, "context"), 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func conflictFile(conflict string) string {
	const marker = "Merge conflict in "
	index := strings.Index(conflict, marker)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(conflict[index+len(marker):])
}
