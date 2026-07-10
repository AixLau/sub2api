package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const benchmarkHeader = `goos: darwin
goarch: arm64
pkg: github.com/Wei-Shaw/sub2api/internal/service
cpu: Apple M2 Max
`

var requiredBenchmarkTestNames = []string{
	"BenchmarkContentModerationExtraction/SmallText",
	"BenchmarkContentModerationExtraction/MultiMessage1MiB",
	"BenchmarkContentModerationRuleMatching",
	"BenchmarkContentModerationRequestSizeRejection",
	"BenchmarkContentModerationConcurrentChecks",
}

var validBenchmarkTestLines = map[string]string{
	"BenchmarkContentModerationExtraction/SmallText":        "BenchmarkContentModerationExtraction/SmallText-12  10  100 ns/op  1000 B/op  5 allocs/op  64 input-bytes/op\n",
	"BenchmarkContentModerationExtraction/MultiMessage1MiB": "BenchmarkContentModerationExtraction/MultiMessage1MiB-12  10  100 ns/op  3000 B/op  5 allocs/op  1024 input-bytes/op\n",
	"BenchmarkContentModerationRuleMatching":                "BenchmarkContentModerationRuleMatching-12  10  100 ns/op  1000 B/op  10 allocs/op\n",
	"BenchmarkContentModerationRequestSizeRejection":        "BenchmarkContentModerationRequestSizeRejection-12  10  100 ns/op  16 B/op  5 allocs/op\n",
	"BenchmarkContentModerationConcurrentChecks":            "BenchmarkContentModerationConcurrentChecks-12  10  100 ns/op  1000 B/op  5 allocs/op\n",
}

type benchmarkOutputOptions struct {
	overrides map[string]string
	counts    map[string]int
	omit      string
	extra     []string
}

func benchmarkOutputForTest(options benchmarkOutputOptions) string {
	var output strings.Builder
	output.WriteString(benchmarkHeader)
	for _, name := range requiredBenchmarkTestNames {
		if name == options.omit {
			continue
		}
		line := validBenchmarkTestLines[name]
		if override, ok := options.overrides[name]; ok {
			line = override
		}
		count := 5
		if override, ok := options.counts[name]; ok {
			count = override
		}
		output.WriteString(strings.Repeat(line, count))
	}
	for _, line := range options.extra {
		output.WriteString(line)
	}
	return output.String()
}

func TestCompareBenchmarkOutputs_AllowsThresholdBoundaryAndRepeatedRuns(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
		"BenchmarkContentModerationRuleMatching": "BenchmarkContentModerationRuleMatching-12  10  120 ns/op  1100 B/op  12 allocs/op\n",
	}})

	if err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate)); err != nil {
		t.Fatalf("expected threshold boundary to pass: %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsTimeRegression(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
		"BenchmarkContentModerationConcurrentChecks": "BenchmarkContentModerationConcurrentChecks-12  10  121 ns/op  1000 B/op  5 allocs/op\n",
	}})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "ns/op") {
		t.Fatalf("expected ns/op regression, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsAllocationRegression(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
		"BenchmarkContentModerationRequestSizeRejection": "BenchmarkContentModerationRequestSizeRejection-12  10  100 ns/op  16 B/op  7 allocs/op\n",
	}})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "allocs/op") {
		t.Fatalf("expected allocs/op regression, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsExtractionAboveThreeTimesInput(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
		"BenchmarkContentModerationExtraction/MultiMessage1MiB": "BenchmarkContentModerationExtraction/MultiMessage1MiB-12  10  100 ns/op  3073 B/op  5 allocs/op  1024 input-bytes/op\n",
	}})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "3x input") {
		t.Fatalf("expected extraction allocation limit failure, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_AllowsFixedSmallExtractionOverhead(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{})

	if err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate)); err != nil {
		t.Fatalf("fixed small-input overhead should use the relative allocation gate: %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsDifferentHost(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := strings.Replace(benchmarkOutputForTest(benchmarkOutputOptions{}), "Apple M2 Max", "Apple M4 Max", 1)

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "same host") {
		t.Fatalf("expected host mismatch, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsDifferentGOMAXPROCS(t *testing.T) {
	base := strings.ReplaceAll(benchmarkOutputForTest(benchmarkOutputOptions{}), "-12", "-1")
	candidate := strings.ReplaceAll(benchmarkOutputForTest(benchmarkOutputOptions{}), "-12", "-8")

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "GOMAXPROCS") {
		t.Fatalf("expected GOMAXPROCS mismatch, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsInconsistentRequiredGOMAXPROCS(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := strings.Replace(
		benchmarkOutputForTest(benchmarkOutputOptions{}),
		"BenchmarkContentModerationConcurrentChecks-12",
		"BenchmarkContentModerationConcurrentChecks-8",
		1,
	)

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "candidate") || !strings.Contains(err.Error(), "GOMAXPROCS") {
		t.Fatalf("expected inconsistent candidate GOMAXPROCS failure, got %v", err)
	}
}

func TestParseBenchmarkOutput_RejectsNonDecimalGOMAXPROCSSuffix(t *testing.T) {
	output := strings.Replace(
		benchmarkOutputForTest(benchmarkOutputOptions{}),
		"BenchmarkContentModerationRuleMatching-12",
		"BenchmarkContentModerationRuleMatching-+12",
		1,
	)

	_, err := parseBenchmarkOutput(strings.NewReader(output))
	if err == nil || !strings.Contains(err.Error(), "positive GOMAXPROCS suffix") {
		t.Fatalf("expected non-decimal GOMAXPROCS suffix failure, got %v", err)
	}
}

func TestParseBenchmarkOutput_RejectsConflictingRepeatedMetadata(t *testing.T) {
	tests := []struct {
		name        string
		metadata    string
		conflicting string
	}{
		{name: "goos", metadata: "goos: darwin", conflicting: "goos: linux"},
		{name: "goarch", metadata: "goarch: arm64", conflicting: "goarch: amd64"},
		{name: "cpu", metadata: "cpu: Apple M2 Max", conflicting: "cpu: Intel Xeon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := strings.Replace(
				benchmarkOutputForTest(benchmarkOutputOptions{}),
				tt.metadata,
				tt.conflicting+"\n"+tt.metadata,
				1,
			)

			_, err := parseBenchmarkOutput(strings.NewReader(output))
			if err == nil || !strings.Contains(err.Error(), "conflicting "+tt.name+" metadata") {
				t.Fatalf("expected conflicting %s metadata failure, got %v", tt.name, err)
			}
		})
	}
}

func TestParseBenchmarkOutput_AllowsIdenticalRepeatedMetadata(t *testing.T) {
	output := benchmarkOutputForTest(benchmarkOutputOptions{})
	for _, metadata := range []string{"goos: darwin", "goarch: arm64", "cpu: Apple M2 Max"} {
		output = strings.Replace(output, metadata, metadata+"\n"+metadata, 1)
	}

	if _, err := parseBenchmarkOutput(strings.NewReader(output)); err != nil {
		t.Fatalf("expected identical repeated metadata to pass: %v", err)
	}
}

func TestParseBenchmarkOutput_RejectsInvalidIterationCount(t *testing.T) {
	tests := []struct {
		name       string
		iterations string
	}{
		{name: "invalid", iterations: "not-a-count"},
		{name: "zero", iterations: "0"},
		{name: "negative", iterations: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
				"BenchmarkContentModerationRuleMatching": "BenchmarkContentModerationRuleMatching-12  " + tt.iterations + "  100 ns/op  1000 B/op  10 allocs/op\n",
			}})

			_, err := parseBenchmarkOutput(strings.NewReader(output))
			if err == nil || !strings.Contains(err.Error(), "iteration count") {
				t.Fatalf("expected invalid iteration count failure, got %v", err)
			}
		})
	}
}

func TestParseBenchmarkOutput_RejectsIncompleteMetricPairs(t *testing.T) {
	tests := []struct {
		name     string
		trailing string
	}{
		{name: "trailing value", trailing: "  64"},
		{name: "trailing token", trailing: "  unexpected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
				"BenchmarkContentModerationRuleMatching": "BenchmarkContentModerationRuleMatching-12  10  100 ns/op  1000 B/op  10 allocs/op" + tt.trailing + "\n",
			}})

			_, err := parseBenchmarkOutput(strings.NewReader(output))
			if err == nil || !strings.Contains(err.Error(), "value/unit pairs") {
				t.Fatalf("expected incomplete metric pair failure, got %v", err)
			}
		})
	}
}

func TestParseBenchmarkOutput_RejectsDuplicateMetricUnits(t *testing.T) {
	output := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
		"BenchmarkContentModerationRuleMatching": "BenchmarkContentModerationRuleMatching-12  10  100 ns/op  101 ns/op  1000 B/op  10 allocs/op\n",
	}})

	_, err := parseBenchmarkOutput(strings.NewReader(output))
	if err == nil || !strings.Contains(err.Error(), "duplicate metric unit ns/op") {
		t.Fatalf("expected duplicate metric unit failure, got %v", err)
	}
}

func TestParseBenchmarkOutput_RejectsNegativeMetrics(t *testing.T) {
	tests := []struct {
		name string
		line string
		unit string
	}{
		{
			name: "ns/op",
			line: "BenchmarkContentModerationRuleMatching-12  10  -1 ns/op  1000 B/op  10 allocs/op\n",
			unit: "ns/op",
		},
		{
			name: "allocs/op",
			line: "BenchmarkContentModerationRuleMatching-12  10  100 ns/op  1000 B/op  -1 allocs/op\n",
			unit: "allocs/op",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := benchmarkOutputForTest(benchmarkOutputOptions{overrides: map[string]string{
				"BenchmarkContentModerationRuleMatching": tt.line,
			}})

			_, err := parseBenchmarkOutput(strings.NewReader(output))
			if err == nil || !strings.Contains(err.Error(), tt.unit) {
				t.Fatalf("expected negative %s failure, got %v", tt.unit, err)
			}
		})
	}
}

func TestCompareBenchmarkOutputs_RejectsMissingCandidateBenchmark(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{omit: "BenchmarkContentModerationConcurrentChecks"})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "missing candidate benchmark") {
		t.Fatalf("expected missing benchmark failure, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsRequiredCandidateWithOneSample(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{counts: map[string]int{
		"BenchmarkContentModerationConcurrentChecks": 1,
	}})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "BenchmarkContentModerationConcurrentChecks") || !strings.Contains(err.Error(), "exactly 5 samples") {
		t.Fatalf("expected five-sample validation failure, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsRequiredBenchmarkOmittedFromBothOutputs(t *testing.T) {
	for _, name := range requiredBenchmarkTestNames {
		t.Run(name, func(t *testing.T) {
			base := benchmarkOutputForTest(benchmarkOutputOptions{omit: name})
			candidate := benchmarkOutputForTest(benchmarkOutputOptions{omit: name})

			err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected required benchmark validation failure for %s, got %v", name, err)
			}
		})
	}
}

func TestCompareBenchmarkOutputs_AllowsExtraBenchmarks(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{extra: []string{
		"BenchmarkContentModerationAdditionalCoverage-12  10  100 ns/op  16 B/op  1 allocs/op\n",
	}})

	if err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate)); err != nil {
		t.Fatalf("expected extra candidate benchmark to be allowed: %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsMatchedExtraWithDifferentGOMAXPROCS(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{extra: []string{
		"BenchmarkContentModerationAdditionalCoverage-4  10  100 ns/op  16 B/op  1 allocs/op\n",
	}})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{extra: []string{
		"BenchmarkContentModerationAdditionalCoverage-8  10  100 ns/op  16 B/op  1 allocs/op\n",
	}})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "GOMAXPROCS") {
		t.Fatalf("expected matched extra GOMAXPROCS mismatch, got %v", err)
	}
}

func TestCompareBenchmarkOutputs_RejectsRegressionWhenEvenSampleMedianWouldOverflow(t *testing.T) {
	base := benchmarkOutputForTest(benchmarkOutputOptions{extra: []string{
		"BenchmarkContentModerationAdditionalCoverage-12  10  1e308 ns/op  16 B/op  1 allocs/op\n",
		"BenchmarkContentModerationAdditionalCoverage-12  10  1e308 ns/op  16 B/op  1 allocs/op\n",
	}})
	candidate := benchmarkOutputForTest(benchmarkOutputOptions{extra: []string{
		"BenchmarkContentModerationAdditionalCoverage-12  10  1.7e308 ns/op  16 B/op  1 allocs/op\n",
		"BenchmarkContentModerationAdditionalCoverage-12  10  1.7e308 ns/op  16 B/op  1 allocs/op\n",
	}})

	err := compareBenchmarkOutputs(strings.NewReader(base), strings.NewReader(candidate))
	if err == nil || !strings.Contains(err.Error(), "ns/op") || !strings.Contains(err.Error(), "regressed") {
		t.Fatalf("expected extreme finite ns/op regression, got %v", err)
	}
}

func TestMedianMetric_RejectsNonFiniteMedian(t *testing.T) {
	_, err := medianMetric([]benchmarkSample{{metrics: map[string]float64{"ns/op": math.Inf(1)}}}, "ns/op")
	if err == nil || !strings.Contains(err.Error(), "non-finite median") {
		t.Fatalf("expected non-finite median failure, got %v", err)
	}
}

func TestRun_ReadsFilesAndReportsPass(t *testing.T) {
	output := benchmarkOutputForTest(benchmarkOutputOptions{})
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.txt")
	candidatePath := filepath.Join(dir, "candidate.txt")
	if err := os.WriteFile(basePath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	if exitCode := run([]string{basePath, candidatePath}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("expected success, exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("expected PASS output, got %q", stdout.String())
	}
}
