package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "upstream-sync:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	repoRoot, args, err := parseRepo(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "catalog":
		if len(args) == 2 && args[1] == "lint" {
			return lintCatalog(repoRoot)
		}
	case "baseline":
		if len(args) == 2 && args[1] == "show" {
			baseline, err := loadBaseline(repoRoot)
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(baseline, "", "  ")
			fmt.Println(string(data))
			return nil
		}
	case "index":
		if len(args) >= 2 && args[1] == "update" {
			opts, err := parseAnalyzeFlags(args[2:])
			if err != nil {
				return err
			}
			opts.BuildContext = false
			opts.Simulate = false
			report, err := analyzeImpact(repoRoot, opts)
			if err != nil {
				return err
			}
			fmt.Printf("index updated: parsed=%d reused=%d upstream_files=%d local_files=%d\n",
				report.Upstream.ParsedBlobs, report.Upstream.ReusedBlobs, len(report.Upstream.Files), len(report.Local.Files))
			return nil
		}
	case "impact":
		if len(args) >= 2 && args[1] == "analyze" {
			opts, err := parseAnalyzeFlags(args[2:])
			if err != nil {
				return err
			}
			opts.BuildContext = true
			report, err := analyzeImpact(repoRoot, opts)
			if err != nil {
				return err
			}
			fmt.Print(renderTextSummary(report))
			return nil
		}
	case "merge":
		if len(args) >= 2 && args[1] == "simulate" {
			return commandMergeSimulate(repoRoot, args[2:])
		}
	case "context":
		if len(args) >= 2 && args[1] == "build" {
			opts, err := parseAnalyzeFlags(args[2:])
			if err != nil {
				return err
			}
			opts.BuildContext = true
			opts.Simulate = true
			report, err := analyzeImpact(repoRoot, opts)
			if err != nil {
				return err
			}
			fmt.Printf("context built: %d bytes, overview %s\n", report.ContextBytes, report.OverviewPath)
			return nil
		}
	case "verify":
		return commandVerify(repoRoot, args[1:])
	case "record":
		return commandRecord(repoRoot, args[1:])
	case "preflight":
		opts, err := parseAnalyzeFlags(args[1:])
		if err != nil {
			return err
		}
		if err := lintCatalog(repoRoot); err != nil {
			return err
		}
		opts.BuildContext = true
		opts.Simulate = true
		report, err := analyzeImpact(repoRoot, opts)
		if err != nil {
			return err
		}
		fmt.Print(renderTextSummary(report))
		return nil
	}
	return usageError()
}

func parseRepo(args []string) (string, []string, error) {
	repoRoot := ""
	var remaining []string
	for index := 0; index < len(args); index++ {
		if args[index] == "--repo" {
			if index+1 >= len(args) {
				return "", nil, fmt.Errorf("--repo requires a path")
			}
			repoRoot = args[index+1]
			index++
			continue
		}
		remaining = append(remaining, args[index])
	}
	if repoRoot == "" {
		current, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		repo := gitRepo{root: current}
		root, err := repo.run("rev-parse", "--show-toplevel")
		if err != nil {
			return "", nil, err
		}
		repoRoot = strings.TrimSpace(root)
	}
	absolute, err := filepath.Abs(repoRoot)
	return absolute, remaining, err
}

func parseAnalyzeFlags(args []string) (analyzeOptions, error) {
	flags := flag.NewFlagSet("analyze", flag.ContinueOnError)
	var opts analyzeOptions
	flags.StringVar(&opts.UpstreamFrom, "from", "", "previous verified upstream ref")
	flags.StringVar(&opts.UpstreamTo, "target", "", "new upstream target ref")
	flags.StringVar(&opts.LocalFrom, "local-from", "", "previous verified merge commit")
	flags.StringVar(&opts.LocalTo, "head", "", "product head")
	flags.StringVar(&opts.OutputDir, "output", "", "output directory (defaults under .git)")
	flags.BoolVar(&opts.Simulate, "simulate", true, "run virtual merge")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	return opts, nil
}

func commandMergeSimulate(repoRoot string, args []string) error {
	flags := flag.NewFlagSet("merge simulate", flag.ContinueOnError)
	target := flags.String("target", "", "upstream target")
	head := flags.String("head", "", "local product head")
	if err := flags.Parse(args); err != nil {
		return err
	}
	baseline, err := loadBaseline(repoRoot)
	if err != nil {
		return err
	}
	if *target == "" {
		*target = baseline.TargetUpstream
	}
	if *head == "" {
		*head = baseline.ProductHead
	}
	simulation := (gitRepo{root: repoRoot}).simulate(*head, *target)
	data, _ := json.MarshalIndent(simulation, "", "  ")
	fmt.Println(string(data))
	if !simulation.Clean {
		return fmt.Errorf("virtual merge has %d conflicts", len(simulation.Conflicts))
	}
	return nil
}

func commandVerify(repoRoot string, args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	impactPath := flags.String("impact", "", "impact.json path")
	runTests := flags.Bool("run", false, "execute selected tests serially")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *impactPath == "" {
		latest, err := latestImpact(repoRoot)
		if err != nil {
			return err
		}
		*impactPath = latest
	} else if !filepath.IsAbs(*impactPath) {
		*impactPath = filepath.Join(repoRoot, *impactPath)
	}
	report, err := readImpact(*impactPath)
	if err != nil {
		return err
	}
	if !*runTests {
		fmt.Printf("verification plan: %d commands\n", len(report.Tests))
		for _, test := range report.Tests {
			fmt.Printf("- %s\n", test.Command)
		}
		return nil
	}
	for _, test := range report.Tests {
		fmt.Printf("RUN %s\n", test.Command)
		command := exec.Command("/bin/sh", "-lc", test.Command)
		command.Dir = repoRoot
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if runtime.GOOS != "windows" {
			command = exec.Command("nice", "-n", "10", "/bin/sh", "-lc", test.Command)
			command.Dir = repoRoot
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
		}
		if err := command.Run(); err != nil {
			return fmt.Errorf("verification failed: %s: %w", test.Command, err)
		}
	}
	return nil
}

func commandRecord(repoRoot string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: record {decision|baseline}")
	}
	if args[0] == "baseline" {
		return recordBaseline(repoRoot, args[1:])
	}
	if args[0] != "decision" {
		return fmt.Errorf("usage: record decision --feature ID --resolution TEXT --rationale TEXT [--result TEXT] [--commit SHA]")
	}
	flags := flag.NewFlagSet("record decision", flag.ContinueOnError)
	featureID := flags.String("feature", "", "feature id")
	resolution := flags.String("resolution", "", "chosen resolution")
	rationale := flags.String("rationale", "", "resolution rationale")
	result := flags.String("result", "not-run", "test result")
	finalCommit := flags.String("commit", "", "final merge commit")
	impactPath := flags.String("impact", "", "impact.json path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *featureID == "" || *resolution == "" || *rationale == "" {
		return errors.New("feature, resolution, and rationale are required")
	}
	if *impactPath == "" {
		latest, err := latestImpact(repoRoot)
		if err != nil {
			return err
		}
		*impactPath = latest
	} else if !filepath.IsAbs(*impactPath) {
		*impactPath = filepath.Join(repoRoot, *impactPath)
	}
	report, err := readImpact(*impactPath)
	if err != nil {
		return err
	}
	features, err := loadFeatures(repoRoot)
	if err != nil {
		return err
	}
	var feature Feature
	var impact FeatureImpact
	found := false
	for _, candidate := range features {
		if candidate.ID == *featureID {
			feature = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown feature %s", *featureID)
	}
	for _, candidate := range report.Features {
		if candidate.ID == *featureID {
			impact = candidate
			break
		}
	}
	decision := Decision{
		SchemaVersion:               1,
		RecordType:                  "decision",
		ID:                          "D-" + strings.ReplaceAll(nowRFC3339(), ":", "") + "-" + feature.ID,
		FeatureID:                   feature.ID,
		UpstreamFrom:                report.Upstream.From,
		UpstreamTo:                  report.Upstream.To,
		ConflictFiles:               report.Simulation.Conflicts,
		ConflictPreimageFingerprint: hashText(strings.Join(report.Simulation.Conflicts, "\n")),
		UpstreamSymbolFingerprint:   hashText(strings.Join(impact.UpstreamSymbols, "\n")),
		LocalSymbolFingerprint:      hashText(strings.Join(impact.LocalSymbols, "\n")),
		InvariantVersion:            hashText(strings.Join(feature.Invariants, "\n")),
		AnalyzerVersion:             analyzerVersion,
		Resolution:                  *resolution,
		Rationale:                   *rationale,
		ProtectedInvariants:         feature.Invariants,
		Tests:                       impact.Tests,
		TestResult:                  *result,
		FinalCommit:                 *finalCommit,
		RecordedAt:                  nowRFC3339(),
	}
	data, _ := json.Marshal(decision)
	path := filepath.Join(repoRoot, "upstream-sync", "decisions.ndjson")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	if _, err := writer.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Printf("recorded %s for %s\n", decision.ID, feature.ID)
	return nil
}

func recordBaseline(repoRoot string, args []string) error {
	flags := flag.NewFlagSet("record baseline", flag.ContinueOnError)
	upstream := flags.String("upstream", "", "verified upstream commit")
	mergeCommit := flags.String("merge", "", "verified upstream merge commit")
	productHead := flags.String("head", "HEAD", "current product head")
	version := flags.String("version", "", "upstream version")
	branch := flags.String("branch", "", "product branch")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *upstream == "" || *mergeCommit == "" || *version == "" {
		return errors.New("upstream, merge, and version are required")
	}
	repo := gitRepo{root: repoRoot}
	resolvedUpstream, err := repo.resolve(*upstream)
	if err != nil {
		return fmt.Errorf("resolve upstream: %w", err)
	}
	resolvedMerge, err := repo.resolve(*mergeCommit)
	if err != nil {
		return fmt.Errorf("resolve merge: %w", err)
	}
	resolvedHead, err := repo.resolve(*productHead)
	if err != nil {
		return fmt.Errorf("resolve head: %w", err)
	}
	if !repo.ancestor(resolvedUpstream, resolvedMerge) {
		return errors.New("upstream commit is not contained in merge commit")
	}
	if !repo.ancestor(resolvedMerge, resolvedHead) {
		return errors.New("merge commit is not contained in product head")
	}
	baseline, err := loadBaseline(repoRoot)
	if err != nil {
		return err
	}
	if *branch == "" {
		currentBranch, branchErr := repo.run("branch", "--show-current")
		if branchErr != nil {
			return branchErr
		}
		*branch = strings.TrimSpace(currentBranch)
	}
	baseline.VerifiedUpstream = resolvedUpstream
	baseline.TargetUpstream = resolvedUpstream
	baseline.MergeCommit = resolvedMerge
	baseline.ProductHead = resolvedHead
	baseline.ProductBranch = *branch
	baseline.UpstreamVersion = *version
	baseline.ProductVersion = *version + "-local"
	baseline.RecordedAt = nowRFC3339()
	data, err := yaml.Marshal(baseline)
	if err != nil {
		return err
	}
	path := filepath.Join(repoRoot, "upstream-sync", "baseline.yaml")
	temp, err := os.CreateTemp(filepath.Dir(path), ".baseline-*.yaml")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	fmt.Printf("baseline updated: %s at %s, product %s@%s\n", *version, shortSHA(resolvedUpstream), *branch, shortSHA(resolvedHead))
	return nil
}

func usageError() error {
	return errors.New("usage: upstream-sync {catalog lint|baseline show|index update|impact analyze|merge simulate|context build|verify|record decision|record baseline|preflight}")
}
