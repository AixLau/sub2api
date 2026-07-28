package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const analyzerVersion = "1.0.1"

type Baseline struct {
	SchemaVersion    int    `yaml:"schema_version" json:"schema_version"`
	VerifiedUpstream string `yaml:"verified_upstream" json:"verified_upstream"`
	TargetUpstream   string `yaml:"target_upstream" json:"target_upstream"`
	MergeCommit      string `yaml:"merge_commit" json:"merge_commit"`
	ProductHead      string `yaml:"product_head" json:"product_head"`
	ProductBranch    string `yaml:"product_branch" json:"product_branch"`
	UpstreamRemote   string `yaml:"upstream_remote" json:"upstream_remote"`
	UpstreamBranch   string `yaml:"upstream_branch" json:"upstream_branch"`
	RecordedAt       string `yaml:"recorded_at" json:"recorded_at"`
	UpstreamVersion  string `yaml:"upstream_version" json:"upstream_version"`
	ProductVersion   string `yaml:"product_version" json:"product_version"`
}

type FeatureTest struct {
	Kind    string   `yaml:"kind" json:"kind"`
	Files   []string `yaml:"files" json:"files,omitempty"`
	Command string   `yaml:"command" json:"command"`
}

type Feature struct {
	SchemaVersion  int           `yaml:"schema_version" json:"schema_version"`
	ID             string        `yaml:"id" json:"id"`
	Name           string        `yaml:"name" json:"name"`
	Purpose        string        `yaml:"purpose" json:"purpose"`
	Risk           string        `yaml:"risk" json:"risk"`
	Paths          []string      `yaml:"paths" json:"paths"`
	Symbols        []string      `yaml:"symbols" json:"symbols"`
	Routes         []string      `yaml:"routes" json:"routes"`
	ConfigKeys     []string      `yaml:"config_keys" json:"config_keys"`
	Environment    []string      `yaml:"environment" json:"environment"`
	JSONFields     []string      `yaml:"json_fields" json:"json_fields"`
	DatabaseFields []string      `yaml:"database_fields" json:"database_fields"`
	APIFields      []string      `yaml:"api_fields" json:"api_fields"`
	I18nKeys       []string      `yaml:"i18n_keys" json:"i18n_keys"`
	Invariants     []string      `yaml:"invariants" json:"invariants"`
	Tests          []FeatureTest `yaml:"tests" json:"tests"`
	IntroducedBy   []string      `yaml:"introduced_by" json:"introduced_by"`
	MergeStrategy  string        `yaml:"merge_strategy" json:"merge_strategy"`
	Watch          []string      `yaml:"watch" json:"watch"`
	SourceFile     string        `yaml:"-" json:"source_file"`
}

type Symbol struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Receiver  string   `json:"receiver,omitempty"`
	Exported  bool     `json:"exported,omitempty"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Signature string   `json:"signature,omitempty"`
	Hash      string   `json:"hash"`
	Calls     []string `json:"calls,omitempty"`
	Refs      []string `json:"refs,omitempty"`
}

type SourceIndex struct {
	Analyzer  string              `json:"analyzer_version"`
	Path      string              `json:"path"`
	Blob      string              `json:"blob"`
	Language  string              `json:"language"`
	Package   string              `json:"package,omitempty"`
	Symbols   []Symbol            `json:"symbols,omitempty"`
	Contracts map[string][]string `json:"contracts,omitempty"`
}

type ChangedFile struct {
	Status         string              `json:"status"`
	OldPath        string              `json:"old_path,omitempty"`
	Path           string              `json:"path"`
	OldBlob        string              `json:"old_blob,omitempty"`
	NewBlob        string              `json:"new_blob,omitempty"`
	Classification []string            `json:"classification,omitempty"`
	ChangedSymbols []string            `json:"changed_symbols,omitempty"`
	DeletedSymbols []string            `json:"deleted_symbols,omitempty"`
	AddedSymbols   []string            `json:"added_symbols,omitempty"`
	Contracts      map[string][]string `json:"changed_contracts,omitempty"`
}

type RangeSummary struct {
	From        string        `json:"from"`
	To          string        `json:"to"`
	CommitCount int           `json:"commit_count"`
	Files       []ChangedFile `json:"files"`
	Added       int           `json:"added"`
	Modified    int           `json:"modified"`
	Deleted     int           `json:"deleted"`
	Renamed     int           `json:"renamed"`
	ParsedBlobs int           `json:"parsed_blobs"`
	ReusedBlobs int           `json:"reused_blobs"`
}

type FeatureImpact struct {
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	Risk              string        `json:"risk"`
	Reasons           []string      `json:"reasons"`
	Files             []string      `json:"files"`
	ContextFiles      []string      `json:"context_files,omitempty"`
	UpstreamSymbols   []string      `json:"upstream_symbols,omitempty"`
	LocalSymbols      []string      `json:"local_symbols,omitempty"`
	Contracts         []string      `json:"contracts,omitempty"`
	Tests             []FeatureTest `json:"tests,omitempty"`
	HistoricalRecords []string      `json:"historical_records,omitempty"`
	ContextPack       string        `json:"context_pack,omitempty"`
}

type Simulation struct {
	Base      string   `json:"base,omitempty"`
	Tree      string   `json:"tree,omitempty"`
	Clean     bool     `json:"clean"`
	Conflicts []string `json:"conflicts,omitempty"`
	RawOutput string   `json:"raw_output,omitempty"`
}

type ImpactReport struct {
	SchemaVersion int             `json:"schema_version"`
	Analyzer      string          `json:"analyzer_version"`
	CreatedAt     string          `json:"created_at"`
	Baseline      Baseline        `json:"baseline"`
	Upstream      RangeSummary    `json:"upstream_delta"`
	Local         RangeSummary    `json:"local_delta"`
	OverlapFiles  []string        `json:"overlap_files"`
	Features      []FeatureImpact `json:"affected_features"`
	Tests         []FeatureTest   `json:"recommended_tests"`
	AutoLowRisk   []string        `json:"auto_low_risk"`
	CodexReview   []string        `json:"codex_review"`
	Simulation    Simulation      `json:"simulation"`
	OutputDir     string          `json:"output_dir"`
	OverviewPath  string          `json:"overview_path"`
	FullReport    string          `json:"full_report_path"`
	ContextBytes  int64           `json:"context_bytes"`
}

type Decision struct {
	SchemaVersion               int           `json:"schema_version"`
	RecordType                  string        `json:"record_type"`
	ID                          string        `json:"id"`
	FeatureID                   string        `json:"feature_id"`
	UpstreamFrom                string        `json:"upstream_from"`
	UpstreamTo                  string        `json:"upstream_to"`
	ConflictFiles               []string      `json:"conflict_files"`
	ConflictPreimageFingerprint string        `json:"conflict_preimage_fingerprint"`
	UpstreamSymbolFingerprint   string        `json:"upstream_symbol_fingerprint"`
	LocalSymbolFingerprint      string        `json:"local_symbol_fingerprint"`
	InvariantVersion            string        `json:"invariant_version"`
	AnalyzerVersion             string        `json:"analyzer_version"`
	Resolution                  string        `json:"resolution"`
	Rationale                   string        `json:"rationale"`
	ProtectedInvariants         []string      `json:"protected_invariants"`
	Tests                       []FeatureTest `json:"tests"`
	TestResult                  string        `json:"test_result"`
	FinalCommit                 string        `json:"final_commit"`
	RecordedAt                  string        `json:"recorded_at"`
}

func loadYAML(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func loadBaseline(repo string) (Baseline, error) {
	var baseline Baseline
	err := loadYAML(filepath.Join(repo, "upstream-sync", "baseline.yaml"), &baseline)
	return baseline, err
}

func loadFeatures(repo string) ([]Feature, error) {
	paths, err := filepath.Glob(filepath.Join(repo, "upstream-sync", "features", "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	features := make([]Feature, 0, len(paths))
	for _, path := range paths {
		var feature Feature
		if err := loadYAML(path, &feature); err != nil {
			return nil, err
		}
		feature.SourceFile, _ = filepath.Rel(repo, path)
		features = append(features, feature)
	}
	return features, nil
}

func loadDecisions(repo string) []Decision {
	data, err := os.ReadFile(filepath.Join(repo, "upstream-sync", "decisions.ndjson"))
	if err != nil {
		return nil
	}
	var decisions []Decision
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var decision Decision
		if json.Unmarshal([]byte(line), &decision) == nil && decision.RecordType == "decision" {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
