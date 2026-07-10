package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	benchmarkPrefix           = "BenchmarkContentModeration"
	maximumRegressionFraction = 0.20
	maximumExtractionBPerByte = 3.0
	requiredBenchmarkSamples  = 5
)

var requiredBenchmarkNames = [...]string{
	"BenchmarkContentModerationExtraction/SmallText",
	"BenchmarkContentModerationExtraction/MultiMessage1MiB",
	"BenchmarkContentModerationRuleMatching",
	"BenchmarkContentModerationRequestSizeRejection",
	"BenchmarkContentModerationConcurrentChecks",
}

type benchmarkEnvironment struct {
	goos   string
	goarch string
	cpu    string
}

type benchmarkSample struct {
	gomaxprocs int
	metrics    map[string]float64
}

type benchmarkOutput struct {
	environment benchmarkEnvironment
	benchmarks  map[string][]benchmarkSample
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: moderation-bench-guard BASE CANDIDATE")
		return 2
	}
	base, err := os.Open(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "open baseline: %v\n", err)
		return 1
	}
	defer func() { _ = base.Close() }()
	candidate, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "open candidate: %v\n", err)
		return 1
	}
	defer func() { _ = candidate.Close() }()

	if err := compareBenchmarkOutputs(base, candidate); err != nil {
		fmt.Fprintf(stderr, "FAIL: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "PASS: content moderation benchmarks are within limits")
	return 0
}

func compareBenchmarkOutputs(baseReader, candidateReader io.Reader) error {
	base, err := parseBenchmarkOutput(baseReader)
	if err != nil {
		return fmt.Errorf("parse baseline: %w", err)
	}
	candidate, err := parseBenchmarkOutput(candidateReader)
	if err != nil {
		return fmt.Errorf("parse candidate: %w", err)
	}
	if base.environment != candidate.environment {
		return fmt.Errorf("benchmarks must come from the same host: baseline=%+v candidate=%+v", base.environment, candidate.environment)
	}
	baseGOMAXPROCS, err := validateRequiredBenchmarkSamples("baseline", base)
	if err != nil {
		return err
	}
	candidateGOMAXPROCS, err := validateRequiredBenchmarkSamples("candidate", candidate)
	if err != nil {
		return err
	}
	if baseGOMAXPROCS != candidateGOMAXPROCS {
		return fmt.Errorf("benchmarks must use the same GOMAXPROCS: baseline=%d candidate=%d", baseGOMAXPROCS, candidateGOMAXPROCS)
	}
	if len(base.benchmarks) == 0 {
		return fmt.Errorf("baseline contains no %s benchmarks", benchmarkPrefix)
	}

	names := make([]string, 0, len(base.benchmarks))
	for name := range base.benchmarks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		candidateSamples, ok := candidate.benchmarks[name]
		if !ok {
			return fmt.Errorf("missing candidate benchmark %s", name)
		}
		baseSamples := base.benchmarks[name]
		if err := compareMetric(name, "ns/op", baseSamples, candidateSamples); err != nil {
			return err
		}
		if err := compareMetric(name, "allocs/op", baseSamples, candidateSamples); err != nil {
			return err
		}
	}

	for name, samples := range candidate.benchmarks {
		if !strings.HasPrefix(name, benchmarkPrefix+"Extraction/MultiMessage1MiB") {
			continue
		}
		bytesPerOp, err := medianMetric(samples, "B/op")
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		inputBytes, err := medianMetric(samples, "input-bytes/op")
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if bytesPerOp > maximumExtractionBPerByte*inputBytes {
			return fmt.Errorf("%s B/op %.0f exceeds 3x input bytes %.0f", name, bytesPerOp, inputBytes)
		}
	}
	return nil
}

func validateRequiredBenchmarkSamples(source string, output benchmarkOutput) (int, error) {
	for _, name := range requiredBenchmarkNames {
		samples, ok := output.benchmarks[name]
		if !ok {
			return 0, fmt.Errorf("missing %s benchmark %s", source, name)
		}
		if len(samples) != requiredBenchmarkSamples {
			return 0, fmt.Errorf("%s benchmark %s must contain exactly %d samples; got %d", source, name, requiredBenchmarkSamples, len(samples))
		}
	}

	gomaxprocs := 0
	for _, samples := range output.benchmarks {
		for _, sample := range samples {
			if gomaxprocs == 0 {
				gomaxprocs = sample.gomaxprocs
				continue
			}
			if sample.gomaxprocs != gomaxprocs {
				return 0, fmt.Errorf("%s benchmark samples use inconsistent GOMAXPROCS: %d and %d", source, gomaxprocs, sample.gomaxprocs)
			}
		}
	}
	return gomaxprocs, nil
}

func compareMetric(name, unit string, baseSamples, candidateSamples []benchmarkSample) error {
	base, err := medianMetric(baseSamples, unit)
	if err != nil {
		return fmt.Errorf("%s baseline: %w", name, err)
	}
	candidate, err := medianMetric(candidateSamples, unit)
	if err != nil {
		return fmt.Errorf("%s candidate: %w", name, err)
	}
	if base == 0 {
		if candidate > 0 {
			return fmt.Errorf("%s %s regressed from zero to %.2f", name, unit, candidate)
		}
		return nil
	}
	limit := base * (1 + maximumRegressionFraction)
	if candidate > limit {
		return fmt.Errorf("%s %s regressed %.2f%%: baseline=%.2f candidate=%.2f limit=%.2f", name, unit, (candidate/base-1)*100, base, candidate, limit)
	}
	return nil
}

func parseBenchmarkOutput(reader io.Reader) (benchmarkOutput, error) {
	out := benchmarkOutput{benchmarks: make(map[string][]benchmarkSample)}
	metadata := make(map[string]string, 3)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "goos:"):
			value := strings.TrimSpace(strings.TrimPrefix(line, "goos:"))
			if err := setBenchmarkMetadata(metadata, "goos", value); err != nil {
				return benchmarkOutput{}, err
			}
			out.environment.goos = value
		case strings.HasPrefix(line, "goarch:"):
			value := strings.TrimSpace(strings.TrimPrefix(line, "goarch:"))
			if err := setBenchmarkMetadata(metadata, "goarch", value); err != nil {
				return benchmarkOutput{}, err
			}
			out.environment.goarch = value
		case strings.HasPrefix(line, "cpu:"):
			value := strings.TrimSpace(strings.TrimPrefix(line, "cpu:"))
			if err := setBenchmarkMetadata(metadata, "cpu", value); err != nil {
				return benchmarkOutput{}, err
			}
			out.environment.cpu = value
		case strings.HasPrefix(line, benchmarkPrefix):
			fields := strings.Fields(line)
			if len(fields) < 4 || (len(fields)-2)%2 != 0 {
				return benchmarkOutput{}, fmt.Errorf("benchmark metrics must be complete value/unit pairs in line %q", line)
			}
			name, gomaxprocs, err := parseBenchmarkName(fields[0])
			if err != nil {
				return benchmarkOutput{}, err
			}
			iterations, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil || iterations == 0 {
				return benchmarkOutput{}, fmt.Errorf("invalid iteration count %q in line %q", fields[1], line)
			}
			sample := benchmarkSample{
				gomaxprocs: gomaxprocs,
				metrics:    make(map[string]float64, (len(fields)-2)/2),
			}
			for i := 2; i+1 < len(fields); i += 2 {
				value, err := strconv.ParseFloat(fields[i], 64)
				unit := fields[i+1]
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
					return benchmarkOutput{}, fmt.Errorf("invalid %s metric %q in line %q", unit, fields[i], line)
				}
				if unit == "" {
					return benchmarkOutput{}, fmt.Errorf("empty metric unit in line %q", line)
				}
				if _, ok := sample.metrics[unit]; ok {
					return benchmarkOutput{}, fmt.Errorf("duplicate metric unit %s in line %q", unit, line)
				}
				sample.metrics[unit] = value
			}
			out.benchmarks[name] = append(out.benchmarks[name], sample)
		}
	}
	if err := scanner.Err(); err != nil {
		return benchmarkOutput{}, err
	}
	if out.environment.goos == "" || out.environment.goarch == "" || out.environment.cpu == "" {
		return benchmarkOutput{}, fmt.Errorf("missing goos, goarch, or cpu metadata")
	}
	return out, nil
}

func setBenchmarkMetadata(metadata map[string]string, key, value string) error {
	if previous, ok := metadata[key]; ok && previous != value {
		return fmt.Errorf("conflicting %s metadata: %q and %q", key, previous, value)
	}
	metadata[key] = value
	return nil
}

func parseBenchmarkName(name string) (string, int, error) {
	separator := strings.LastIndexByte(name, '-')
	if separator < 0 || separator == len(name)-1 {
		return "", 0, fmt.Errorf("benchmark name %q must end with a positive GOMAXPROCS suffix", name)
	}
	suffix := name[separator+1:]
	for _, ch := range suffix {
		if ch < '0' || ch > '9' {
			return "", 0, fmt.Errorf("benchmark name %q must end with a positive GOMAXPROCS suffix", name)
		}
	}
	gomaxprocs, err := strconv.Atoi(suffix)
	if err != nil || gomaxprocs <= 0 {
		return "", 0, fmt.Errorf("benchmark name %q must end with a positive GOMAXPROCS suffix", name)
	}
	return name[:separator], gomaxprocs, nil
}

func medianMetric(samples []benchmarkSample, unit string) (float64, error) {
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		value, ok := sample.metrics[unit]
		if !ok {
			return 0, fmt.Errorf("missing %s", unit)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("missing samples")
	}
	sort.Float64s(values)
	mid := len(values) / 2
	median := values[mid]
	if len(values)%2 == 0 {
		median = values[mid-1] + (values[mid]-values[mid-1])/2
	}
	if math.IsNaN(median) || math.IsInf(median, 0) {
		return 0, fmt.Errorf("non-finite median %s", unit)
	}
	return median, nil
}
