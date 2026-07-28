package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func lintCatalog(repoRoot string) error {
	baseline, err := loadBaseline(repoRoot)
	if err != nil {
		return err
	}
	repo := gitRepo{root: repoRoot}
	for label, ref := range map[string]string{
		"verified_upstream": baseline.VerifiedUpstream,
		"target_upstream":   baseline.TargetUpstream,
		"merge_commit":      baseline.MergeCommit,
		"product_head":      baseline.ProductHead,
	} {
		if _, err := repo.resolve(ref); err != nil {
			return fmt.Errorf("%s does not resolve: %w", label, err)
		}
	}
	if !repo.ancestor(baseline.VerifiedUpstream, baseline.TargetUpstream) {
		return fmt.Errorf("verified_upstream is not an ancestor of target_upstream")
	}
	features, err := loadFeatures(repoRoot)
	if err != nil {
		return err
	}
	if len(features) == 0 {
		return fmt.Errorf("no feature manifests")
	}
	ids := make(map[string]string)
	validID := regexp.MustCompile(`^[A-Z][A-Z0-9-]+$`)
	trackedBlobs := repo.trackedBlobs(baseline.ProductHead)
	tracked := make([]string, 0, len(trackedBlobs))
	for trackedPath := range trackedBlobs {
		tracked = append(tracked, trackedPath)
	}
	idx, err := newIndexer(repo)
	if err != nil {
		return err
	}
	for _, feature := range features {
		if feature.SchemaVersion != 1 {
			return fmt.Errorf("%s: unsupported schema_version %d", feature.SourceFile, feature.SchemaVersion)
		}
		if !validID.MatchString(feature.ID) {
			return fmt.Errorf("%s: invalid id %q", feature.SourceFile, feature.ID)
		}
		if previous, exists := ids[feature.ID]; exists {
			return fmt.Errorf("duplicate feature id %s in %s and %s", feature.ID, previous, feature.SourceFile)
		}
		ids[feature.ID] = feature.SourceFile
		if feature.Name == "" || feature.Purpose == "" || feature.Risk == "" || feature.MergeStrategy == "" {
			return fmt.Errorf("%s: name, purpose, risk, and merge_strategy are required", feature.SourceFile)
		}
		if len(feature.Paths) == 0 || len(feature.Invariants) == 0 || len(feature.Tests) == 0 || len(feature.IntroducedBy) == 0 {
			return fmt.Errorf("%s: paths, invariants, tests, and introduced_by are required", feature.SourceFile)
		}
		matchedPath := false
		for _, pattern := range feature.Paths {
			for _, trackedPath := range tracked {
				if matchPathPattern(pattern, trackedPath) {
					matchedPath = true
					break
				}
			}
		}
		if !matchedPath {
			return fmt.Errorf("%s: no declared path exists at product_head", feature.SourceFile)
		}
		if len(feature.Symbols) > 0 {
			var indexedSymbols []string
			for _, trackedPath := range tracked {
				if !featureMatchesPath(feature, trackedPath) || !isIndexableSource(trackedPath) {
					continue
				}
				index, indexErr := idx.atBlob(baseline.ProductHead, trackedPath, trackedBlobs[trackedPath])
				if indexErr != nil {
					continue
				}
				for _, symbol := range index.Symbols {
					indexedSymbols = append(indexedSymbols, symbol.Name)
				}
			}
			for _, anchor := range feature.Symbols {
				if len(intersectNormalized([]string{anchor}, indexedSymbols)) == 0 {
					return fmt.Errorf("%s: symbol anchor %q does not exist at product_head", feature.SourceFile, anchor)
				}
			}
		}
		for _, commit := range feature.IntroducedBy {
			if _, err := repo.resolve(commit); err != nil {
				return fmt.Errorf("%s: introduced_by commit %s does not resolve", feature.SourceFile, commit)
			}
		}
		for _, test := range feature.Tests {
			if strings.TrimSpace(test.Command) == "" {
				return fmt.Errorf("%s: test command is empty", feature.SourceFile)
			}
			for _, testFile := range test.Files {
				if _, err := os.Stat(filepath.Join(repoRoot, testFile)); err != nil {
					return fmt.Errorf("%s: test file %s does not exist", feature.SourceFile, testFile)
				}
			}
		}
	}
	fmt.Printf("catalog ok: %d features, baseline %s, product %s@%s\n", len(features), baseline.UpstreamVersion, baseline.ProductBranch, shortSHA(baseline.ProductHead))
	return nil
}

func isIndexableSource(path string) bool {
	for _, extension := range []string{".go", ".vue", ".ts", ".tsx", ".js", ".jsx"} {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return false
}
