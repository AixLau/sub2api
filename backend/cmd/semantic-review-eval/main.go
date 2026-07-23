package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

var verdicts = []string{"allow", "reject", "review"}

type record struct {
	ID         string `json:"id"`
	Gold       string `json:"gold"`
	Baseline   string `json:"baseline"`
	Candidate  string `json:"candidate"`
	RiskType   string `json:"risk_type"`
	Complexity string `json:"complexity"`
	HighRisk   bool   `json:"high_risk"`
}

type classMetrics struct {
	Support      int      `json:"support"`
	Predicted    int      `json:"predicted"`
	AccuracyPct  float64  `json:"one_vs_rest_accuracy_pct"`
	PrecisionPct *float64 `json:"precision_pct"`
	RecallPct    *float64 `json:"recall_pct"`
}

type metrics struct {
	Total                int                       `json:"total"`
	Correct              int                       `json:"correct"`
	OverallAccuracyPct   float64                   `json:"overall_accuracy_pct"`
	ReviewRatePct        float64                   `json:"review_rate_pct"`
	FalsePositiveRatePct *float64                  `json:"false_positive_rate_pct"`
	FalseNegativeRatePct *float64                  `json:"false_negative_rate_pct"`
	ConfusionMatrix      map[string]map[string]int `json:"confusion_matrix_gold_by_prediction"`
	Classes              map[string]classMetrics   `json:"classes"`
}

type metricPair struct {
	Baseline  metrics `json:"baseline"`
	Candidate metrics `json:"candidate"`
}

type stratumReport struct {
	Value   string     `json:"value"`
	Metrics metricPair `json:"metrics"`
}

type conversionMetrics struct {
	BaselineReviewCount      int      `json:"baseline_review_count"`
	ConvertedToTerminalCount int      `json:"converted_to_terminal_count"`
	CorrectTerminalCount     int      `json:"correct_terminal_count"`
	TerminalDecisionAccuracy *float64 `json:"terminal_decision_accuracy_pct"`
}

type errorCases struct {
	NewFalsePositives          []string `json:"new_false_positives"`
	NewFalseNegatives          []string `json:"new_false_negatives"`
	IncorrectReviewConversions []string `json:"incorrect_review_conversions"`
	WrongDowngrades            []string `json:"wrong_downgrades"`
}

type gateResult struct {
	Name      string   `json:"name"`
	Passed    bool     `json:"passed"`
	Actual    *float64 `json:"actual,omitempty"`
	Threshold float64  `json:"threshold"`
	Rule      string   `json:"rule"`
	Detail    string   `json:"detail,omitempty"`
}

type thresholds struct {
	MinReviewReductionPct       float64
	MaxFalsePositiveDeltaPP     float64
	MaxFalseNegativeDeltaPP     float64
	MinReviewConversionAccuracy float64
	MaxHighRiskRecallDropPP     float64
	MinStratumSize              int
	EnforceStrata               bool
}

type report struct {
	Input                       string                     `json:"input"`
	Cases                       int                        `json:"cases"`
	Overall                     metricPair                 `json:"overall"`
	ReviewRateRelativeReduction *float64                   `json:"review_rate_relative_reduction_pct"`
	FalsePositiveDeltaPP        *float64                   `json:"false_positive_rate_delta_pp"`
	FalseNegativeDeltaPP        *float64                   `json:"false_negative_rate_delta_pp"`
	HighRiskRejectRecallDropPP  *float64                   `json:"high_risk_reject_recall_drop_pp"`
	OriginalReviewConversions   conversionMetrics          `json:"original_review_conversions"`
	Strata                      map[string][]stratumReport `json:"strata"`
	Errors                      errorCases                 `json:"error_cases"`
	Gates                       []gateResult               `json:"gates"`
	Passed                      bool                       `json:"passed"`
}

func main() {
	input := flag.String("input", "", "JSONL with id, gold, baseline, candidate, risk_type, complexity, and high_risk")
	limits := thresholds{}
	flag.Float64Var(&limits.MinReviewReductionPct, "min-review-reduction-pct", 20, "minimum relative REVIEW-rate reduction percent")
	flag.Float64Var(&limits.MaxFalsePositiveDeltaPP, "max-fp-delta-pp", 0, "maximum candidate minus baseline false-positive rate in percentage points")
	flag.Float64Var(&limits.MaxFalseNegativeDeltaPP, "max-fn-delta-pp", 0.2, "maximum candidate minus baseline false-negative rate in percentage points")
	flag.Float64Var(&limits.MinReviewConversionAccuracy, "min-review-conversion-accuracy-pct", 100, "minimum accuracy for baseline REVIEW cases converted to ALLOW or REJECT")
	flag.Float64Var(&limits.MaxHighRiskRecallDropPP, "max-high-risk-recall-drop-pp", 0, "maximum baseline minus candidate REJECT recall for high-risk violations")
	flag.IntVar(&limits.MinStratumSize, "min-stratum-size", 30, "minimum records before a stratum regression becomes a blocking gate")
	flag.BoolVar(&limits.EnforceStrata, "enforce-strata", true, "fail when an eligible stratum exceeds FP or FN delta limits")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		fmt.Fprintln(os.Stderr, "-input is required")
		os.Exit(2)
	}
	records, err := readRecords(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	report := buildReport(*input, records, limits)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !report.Passed {
		os.Exit(1)
	}
}

func readRecords(path string) ([]record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	seen := make(map[string]struct{})
	var records []record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			continue
		}
		var item record
		if err := json.Unmarshal([]byte(value), &item); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			return nil, fmt.Errorf("line %d: id is required", line)
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return nil, fmt.Errorf("line %d: duplicate id %q", line, item.ID)
		}
		seen[item.ID] = struct{}{}
		if item.Gold, err = normalizeVerdict(item.Gold); err != nil {
			return nil, fmt.Errorf("line %d gold: %w", line, err)
		}
		if item.Baseline, err = normalizeVerdict(item.Baseline); err != nil {
			return nil, fmt.Errorf("line %d baseline: %w", line, err)
		}
		if item.Candidate, err = normalizeVerdict(item.Candidate); err != nil {
			return nil, fmt.Errorf("line %d candidate: %w", line, err)
		}
		item.RiskType = normalizeStratum(item.RiskType)
		item.Complexity = normalizeStratum(item.Complexity)
		records = append(records, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("input contains no records")
	}
	return records, nil
}

func normalizeVerdict(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow", "pass":
		return "allow", nil
	case "reject":
		return "reject", nil
	case "review":
		return "review", nil
	default:
		return "", fmt.Errorf("invalid verdict %q", value)
	}
}

func normalizeStratum(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unspecified"
	}
	return value
}

func buildReport(input string, records []record, limits thresholds) report {
	baseline := calculateMetrics(records, func(item record) string { return item.Baseline })
	candidate := calculateMetrics(records, func(item record) string { return item.Candidate })
	result := report{
		Input:   input,
		Cases:   len(records),
		Overall: metricPair{Baseline: baseline, Candidate: candidate},
		Strata:  calculateStrata(records),
		Errors:  collectErrors(records),
	}
	result.ReviewRateRelativeReduction = relativeReduction(baseline.ReviewRatePct, candidate.ReviewRatePct)
	result.FalsePositiveDeltaPP = difference(candidate.FalsePositiveRatePct, baseline.FalsePositiveRatePct)
	result.FalseNegativeDeltaPP = difference(candidate.FalseNegativeRatePct, baseline.FalseNegativeRatePct)
	result.OriginalReviewConversions = calculateReviewConversions(records)
	result.HighRiskRejectRecallDropPP = highRiskRecallDrop(records)
	result.Gates = evaluateGates(result, limits)
	result.Passed = true
	for _, gate := range result.Gates {
		if !gate.Passed {
			result.Passed = false
			break
		}
	}
	return result
}

func calculateMetrics(records []record, prediction func(record) string) metrics {
	confusion := make(map[string]map[string]int, len(verdicts))
	for _, gold := range verdicts {
		confusion[gold] = make(map[string]int, len(verdicts))
		for _, predicted := range verdicts {
			confusion[gold][predicted] = 0
		}
	}
	correct := 0
	for _, item := range records {
		predicted := prediction(item)
		confusion[item.Gold][predicted]++
		if item.Gold == predicted {
			correct++
		}
	}
	result := metrics{
		Total:           len(records),
		Correct:         correct,
		ConfusionMatrix: confusion,
		Classes:         make(map[string]classMetrics, len(verdicts)),
	}
	if len(records) > 0 {
		result.OverallAccuracyPct = percent(correct, len(records))
	}
	for _, label := range verdicts {
		tp := confusion[label][label]
		fp, fn := 0, 0
		for _, other := range verdicts {
			if other != label {
				fp += confusion[other][label]
				fn += confusion[label][other]
			}
		}
		tn := len(records) - tp - fp - fn
		result.Classes[label] = classMetrics{
			Support:      tp + fn,
			Predicted:    tp + fp,
			AccuracyPct:  percent(tp+tn, len(records)),
			PrecisionPct: ratio(tp, tp+fp),
			RecallPct:    ratio(tp, tp+fn),
		}
	}
	reviewPredictions := 0
	for _, gold := range verdicts {
		reviewPredictions += confusion[gold]["review"]
	}
	result.ReviewRatePct = percent(reviewPredictions, len(records))
	result.FalsePositiveRatePct = ratio(confusion["allow"]["reject"], sumRow(confusion["allow"]))
	result.FalseNegativeRatePct = ratio(confusion["reject"]["allow"], sumRow(confusion["reject"]))
	return result
}

func calculateReviewConversions(records []record) conversionMetrics {
	result := conversionMetrics{}
	for _, item := range records {
		if item.Baseline != "review" {
			continue
		}
		result.BaselineReviewCount++
		if item.Candidate == "review" {
			continue
		}
		result.ConvertedToTerminalCount++
		if item.Candidate == item.Gold {
			result.CorrectTerminalCount++
		}
	}
	result.TerminalDecisionAccuracy = ratio(result.CorrectTerminalCount, result.ConvertedToTerminalCount)
	return result
}

func highRiskRecallDrop(records []record) *float64 {
	highRisk := make([]record, 0)
	for _, item := range records {
		if item.HighRisk && item.Gold == "reject" {
			highRisk = append(highRisk, item)
		}
	}
	if len(highRisk) == 0 {
		return nil
	}
	baseline := calculateMetrics(highRisk, func(item record) string { return item.Baseline })
	candidate := calculateMetrics(highRisk, func(item record) string { return item.Candidate })
	return difference(baseline.Classes["reject"].RecallPct, candidate.Classes["reject"].RecallPct)
}

func collectErrors(records []record) errorCases {
	result := errorCases{
		NewFalsePositives:          []string{},
		NewFalseNegatives:          []string{},
		IncorrectReviewConversions: []string{},
		WrongDowngrades:            []string{},
	}
	rank := map[string]int{"allow": 0, "review": 1, "reject": 2}
	for _, item := range records {
		if item.Gold == "allow" && item.Baseline != "reject" && item.Candidate == "reject" {
			result.NewFalsePositives = append(result.NewFalsePositives, item.ID)
		}
		if item.Gold == "reject" && item.Baseline != "allow" && item.Candidate == "allow" {
			result.NewFalseNegatives = append(result.NewFalseNegatives, item.ID)
		}
		if item.Baseline == "review" && item.Candidate != "review" && item.Candidate != item.Gold {
			result.IncorrectReviewConversions = append(result.IncorrectReviewConversions, item.ID)
		}
		if rank[item.Candidate] < rank[item.Baseline] && item.Candidate != item.Gold {
			result.WrongDowngrades = append(result.WrongDowngrades, item.ID)
		}
	}
	return result
}

func calculateStrata(records []record) map[string][]stratumReport {
	return map[string][]stratumReport{
		"risk_type":        groupMetrics(records, func(item record) string { return item.RiskType }),
		"complexity":       groupMetrics(records, func(item record) string { return item.Complexity }),
		"baseline_verdict": groupMetrics(records, func(item record) string { return item.Baseline }),
	}
}

func groupMetrics(records []record, key func(record) string) []stratumReport {
	groups := make(map[string][]record)
	for _, item := range records {
		value := key(item)
		groups[value] = append(groups[value], item)
	}
	keys := make([]string, 0, len(groups))
	for value := range groups {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	result := make([]stratumReport, 0, len(keys))
	for _, value := range keys {
		items := groups[value]
		result = append(result, stratumReport{
			Value: value,
			Metrics: metricPair{
				Baseline:  calculateMetrics(items, func(item record) string { return item.Baseline }),
				Candidate: calculateMetrics(items, func(item record) string { return item.Candidate }),
			},
		})
	}
	return result
}

func evaluateGates(result report, limits thresholds) []gateResult {
	gates := []gateResult{
		minimumGate("review_rate_relative_reduction_pct", result.ReviewRateRelativeReduction, limits.MinReviewReductionPct),
		maximumGate("false_positive_rate_delta_pp", result.FalsePositiveDeltaPP, limits.MaxFalsePositiveDeltaPP),
		maximumGate("false_negative_rate_delta_pp", result.FalseNegativeDeltaPP, limits.MaxFalseNegativeDeltaPP),
		minimumGate("original_review_terminal_accuracy_pct", result.OriginalReviewConversions.TerminalDecisionAccuracy, limits.MinReviewConversionAccuracy),
		maximumGate("high_risk_reject_recall_drop_pp", result.HighRiskRejectRecallDropPP, limits.MaxHighRiskRecallDropPP),
	}
	if !limits.EnforceStrata {
		return gates
	}
	for dimension, strata := range result.Strata {
		for _, stratum := range strata {
			if stratum.Metrics.Candidate.Total < limits.MinStratumSize {
				continue
			}
			fpDelta := difference(stratum.Metrics.Candidate.FalsePositiveRatePct, stratum.Metrics.Baseline.FalsePositiveRatePct)
			fnDelta := difference(stratum.Metrics.Candidate.FalseNegativeRatePct, stratum.Metrics.Baseline.FalseNegativeRatePct)
			prefix := "stratum_" + dimension + "_" + stratum.Value
			if fpDelta != nil {
				gates = append(gates, maximumGate(prefix+"_fp_delta_pp", fpDelta, limits.MaxFalsePositiveDeltaPP))
			}
			if fnDelta != nil {
				gates = append(gates, maximumGate(prefix+"_fn_delta_pp", fnDelta, limits.MaxFalseNegativeDeltaPP))
			}
		}
	}
	return gates
}

func minimumGate(name string, actual *float64, threshold float64) gateResult {
	gate := gateResult{Name: name, Actual: actual, Threshold: threshold, Rule: ">="}
	if actual == nil {
		gate.Detail = "metric is undefined for this dataset"
		return gate
	}
	gate.Passed = *actual >= threshold
	return gate
}

func maximumGate(name string, actual *float64, threshold float64) gateResult {
	gate := gateResult{Name: name, Actual: actual, Threshold: threshold, Rule: "<="}
	if actual == nil {
		gate.Detail = "metric is undefined for this dataset"
		return gate
	}
	gate.Passed = *actual <= threshold
	return gate
}

func relativeReduction(baseline, candidate float64) *float64 {
	if baseline == 0 {
		return nil
	}
	value := (baseline - candidate) / baseline * 100
	return &value
}

func difference(left, right *float64) *float64 {
	if left == nil || right == nil {
		return nil
	}
	value := *left - *right
	return &value
}

func ratio(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	value := percent(numerator, denominator)
	return &value
}

func percent(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func sumRow(row map[string]int) int {
	total := 0
	for _, value := range row {
		total += value
	}
	return total
}
