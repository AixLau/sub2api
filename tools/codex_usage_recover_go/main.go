package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

var tokenKeys = []string{
	"input_tokens",
	"cached_input_tokens",
	"output_tokens",
	"reasoning_output_tokens",
	"total_tokens",
}

type price struct {
	Input       float64 `json:"input_cost_per_token"`
	CachedRead  float64 `json:"cache_read_input_token_cost"`
	Output      float64 `json:"output_cost_per_token"`
	DisplayName string
}

type row struct {
	Timestamp             string
	Date                  string
	SessionID             string
	RequestIndex          int
	Model                 string
	InputTokens           int64
	CachedInputTokens     int64
	UncachedInputTokens   int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
	BaseCost              float64
	Multiplier            float64
	FinalCost             float64
	SupplierMatch         string
	SupplierBasis         string
	SourceFile            string
	UnknownModel          bool
}

func main() {
	home, _ := os.UserHomeDir()
	codexHome := flag.String("codex-home", filepath.Join(home, ".codex"), "")
	supplierURL := flag.String("supplier-url", "https://aixlau.me", "")
	multiplierFrom := flag.String("multiplier-from", "2026-06-08", "")
	multiplier := flag.Float64("multiplier", 1.3, "")
	timezone := flag.String("timezone", "Asia/Shanghai", "")
	priceFile := flag.String("price-file", filepath.Join(filepath.Dir(os.Args[0]), "model_prices_and_context_window.json"), "")
	since := flag.String("since", "2026-05-26", "")
	until := flag.String("until", "", "")
	totalOnly := flag.Bool("total-only", true, "")
	status := flag.Bool("status", false, "")
	out := flag.String("out", "", "")
	includeUnknownSupplier := flag.Bool("include-unknown-supplier", false, "")
	flag.Parse()

	if *status {
		fmt.Fprintln(os.Stderr, "Scanning Codex sessions...")
	}
	report, err := run(*codexHome, *priceFile, *supplierURL, *multiplierFrom, *multiplier, *timezone, *since, *until, *includeUnknownSupplier)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *status {
		fmt.Fprintf(os.Stderr, "Scanned %d sessions and %d usage records.\n", report.Sessions, report.Requests)
	}
	if *out != "" {
		if err := writeCSV(*out, report.Rows); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if *totalOnly {
		fmt.Printf("%.2f\n", report.FinalCost)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

type summary struct {
	Requests              int     `json:"requests"`
	Sessions              int     `json:"sessions"`
	InputTokens           int64   `json:"input_tokens"`
	CachedInputTokens     int64   `json:"cached_input_tokens"`
	UncachedInputTokens   int64   `json:"uncached_input_tokens"`
	OutputTokens          int64   `json:"output_tokens"`
	ReasoningOutputTokens int64   `json:"reasoning_output_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	BaseCost              float64 `json:"base_cost"`
	FinalCost             float64 `json:"final_cost"`
	UnknownModelRequests  int     `json:"unknown_model_requests"`
	SupplierMatch         string  `json:"supplier_match"`
	SupplierBasis         string  `json:"supplier_basis"`
	Rows                  []row   `json:"-"`
}

func run(codexHome, priceFile, supplierURL, multiplierFrom string, multiplier float64, timezone, since, until string, includeUnknownSupplier bool) (summary, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return summary{}, err
	}
	prices, err := loadPrices(priceFile)
	if err != nil {
		return summary{}, err
	}
	match, basis := detectSupplier(codexHome, supplierURL)
	if match == "unknown" && !includeUnknownSupplier {
		return summary{SupplierMatch: match, SupplierBasis: basis}, nil
	}
	cutoff, err := localDay(multiplierFrom, loc)
	if err != nil {
		return summary{}, err
	}
	sinceTime, err := optionalLocalDay(since, loc)
	if err != nil {
		return summary{}, err
	}
	untilTime, err := optionalLocalDay(until, loc)
	if err != nil {
		return summary{}, err
	}
	files := sessionFiles(codexHome)
	var rows []row
	for _, file := range files {
		parsed, err := parseSessionFile(file, prices, match, basis, cutoff, multiplier, loc, sinceTime, untilTime)
		if err == nil {
			rows = append(rows, parsed...)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Timestamp == rows[j].Timestamp {
			return rows[i].SessionID < rows[j].SessionID
		}
		return rows[i].Timestamp < rows[j].Timestamp
	})
	return summarize(rows, match, basis), nil
}

func loadPrices(path string) (map[string]price, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	out := map[string]price{}
	for model, value := range data {
		out[normalizeModel(model)] = price{
			Input:       number(value["input_cost_per_token"]),
			CachedRead:  number(value["cache_read_input_token_cost"]),
			Output:      number(value["output_cost_per_token"]),
			DisplayName: model,
		}
	}
	return out, nil
}

func detectSupplier(codexHome, supplierURL string) (string, string) {
	home, _ := os.UserHomeDir()
	needle := strings.TrimRight(strings.ToLower(supplierURL), "/")
	candidates := []string{
		filepath.Join(codexHome, "config.toml"),
		filepath.Join(codexHome, "browser", "config.toml"),
		filepath.Join(home, ".cc-switch", "config.json"),
		filepath.Join(home, ".cc-switch", "config.toml"),
		filepath.Join(home, ".ccswitch", "config.json"),
		filepath.Join(home, ".ccswitch", "config.toml"),
		filepath.Join(home, "Library", "Application Support", "cc-switch", "config.json"),
		filepath.Join(os.Getenv("APPDATA"), "cc-switch", "config.json"),
	}
	var checked []string
	for _, path := range candidates {
		if path == "" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		checked = append(checked, path)
		if strings.Contains(strings.ToLower(string(raw)), needle) {
			if strings.HasPrefix(path, codexHome) {
				return "direct", path
			}
			return "via_ccswitch", path
		}
	}
	env := strings.ToLower(os.Getenv("OPENAI_BASE_URL") + "\n" + os.Getenv("OPENAI_API_BASE"))
	if strings.Contains(env, needle) {
		return "direct_env", "environment"
	}
	return "unknown", strings.Join(checked, ",")
}

func sessionFiles(codexHome string) []string {
	var files []string
	for _, root := range []string{filepath.Join(codexHome, "sessions"), filepath.Join(codexHome, "archived_sessions")} {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
	}
	sort.Strings(files)
	return files
}

func parseSessionFile(path string, prices map[string]price, supplierMatch, supplierBasis string, cutoff time.Time, multiplier float64, loc *time.Location, since, until *time.Time) ([]row, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	model := extractModel(path)
	prev := map[string]int64{}
	var rows []row
	requestIndex := 0
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "token_count") && !strings.Contains(line, "session_meta") && !strings.Contains(line, "turn_context") {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		payload, _ := obj["payload"].(map[string]any)
		switch obj["type"] {
		case "session_meta":
			if id, ok := payload["id"].(string); ok && id != "" {
				sessionID = id
			}
			if m, ok := payload["model"].(string); ok && m != "" {
				model = m
			}
		case "turn_context":
			if m, ok := payload["model"].(string); ok && m != "" {
				model = m
			}
		case "event_msg":
			if payload["type"] != "token_count" {
				continue
			}
			tsText, _ := obj["timestamp"].(string)
			ts, err := parseTimestamp(tsText, loc)
			if err != nil {
				continue
			}
			if since != nil && ts.Before(*since) {
				continue
			}
			if until != nil && !ts.Before(*until) {
				continue
			}
			info, _ := payload["info"].(map[string]any)
			total, _ := info["total_token_usage"].(map[string]any)
			if total == nil {
				continue
			}
			delta := usageDelta(total, prev)
			prev = usageMap(total)
			if delta["input_tokens"] == 0 && delta["cached_input_tokens"] == 0 && delta["output_tokens"] == 0 {
				continue
			}
			requestIndex++
			modelName := normalizeModel(model)
			p, ok := prices[modelName]
			baseCost := computeCost(delta, p)
			rowMultiplier := 1.0
			if !ts.Before(cutoff) {
				rowMultiplier = multiplier
			}
			rows = append(rows, row{
				Timestamp:             ts.Format(time.RFC3339Nano),
				Date:                  ts.Format("2006-01-02"),
				SessionID:             sessionID,
				RequestIndex:          requestIndex,
				Model:                 modelName,
				InputTokens:           delta["input_tokens"],
				CachedInputTokens:     delta["cached_input_tokens"],
				UncachedInputTokens:   max(delta["input_tokens"]-delta["cached_input_tokens"], 0),
				OutputTokens:          delta["output_tokens"],
				ReasoningOutputTokens: delta["reasoning_output_tokens"],
				TotalTokens:           delta["total_tokens"],
				BaseCost:              baseCost,
				Multiplier:            rowMultiplier,
				FinalCost:             baseCost * rowMultiplier,
				SupplierMatch:         supplierMatch,
				SupplierBasis:         supplierBasis,
				SourceFile:            path,
				UnknownModel:          !ok,
			})
		}
	}
	return rows, nil
}

func summarize(rows []row, supplierMatch, supplierBasis string) summary {
	sessions := map[string]bool{}
	s := summary{Requests: len(rows), SupplierMatch: supplierMatch, SupplierBasis: supplierBasis, Rows: rows}
	for _, row := range rows {
		sessions[row.SessionID] = true
		s.InputTokens += row.InputTokens
		s.CachedInputTokens += row.CachedInputTokens
		s.UncachedInputTokens += row.UncachedInputTokens
		s.OutputTokens += row.OutputTokens
		s.ReasoningOutputTokens += row.ReasoningOutputTokens
		s.TotalTokens += row.TotalTokens
		s.BaseCost += row.BaseCost
		s.FinalCost += row.FinalCost
		if row.UnknownModel {
			s.UnknownModelRequests++
		}
	}
	s.Sessions = len(sessions)
	return s
}

func writeCSV(path string, rows []row) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	defer w.Flush()
	_ = w.Write([]string{"timestamp", "date", "session_id", "model", "input_tokens", "cached_input_tokens", "output_tokens", "base_cost", "multiplier", "final_cost", "supplier_match"})
	for _, r := range rows {
		_ = w.Write([]string{
			r.Timestamp, r.Date, r.SessionID, r.Model,
			fmt.Sprint(r.InputTokens), fmt.Sprint(r.CachedInputTokens), fmt.Sprint(r.OutputTokens),
			fmt.Sprintf("%.8f", r.BaseCost), fmt.Sprintf("%.2f", r.Multiplier), fmt.Sprintf("%.8f", r.FinalCost), r.SupplierMatch,
		})
	}
	return w.Error()
}

func usageDelta(current map[string]any, previous map[string]int64) map[string]int64 {
	delta := map[string]int64{}
	for _, key := range tokenKeys {
		value := int64(number(current[key]))
		diff := value - previous[key]
		if diff < 0 {
			diff = 0
		}
		delta[key] = diff
	}
	return delta
}

func usageMap(current map[string]any) map[string]int64 {
	out := map[string]int64{}
	for _, key := range tokenKeys {
		out[key] = int64(number(current[key]))
	}
	return out
}

func computeCost(delta map[string]int64, p price) float64 {
	cached := delta["cached_input_tokens"]
	uncached := max(delta["input_tokens"]-cached, 0)
	return float64(uncached)*p.Input + float64(cached)*p.CachedRead + float64(delta["output_tokens"])*p.Output
}

func parseTimestamp(value string, loc *time.Location) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return ts.In(loc), nil
}

func localDay(day string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func optionalLocalDay(day string, loc *time.Location) (*time.Time, error) {
	if day == "" {
		return nil, nil
	}
	t, err := localDay(day, loc)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func normalizeModel(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	if model == "" {
		return "unknown"
	}
	return model
}

var modelPattern = regexp.MustCompile(`(?i)(gpt-[\w.-]+|codex-[\w.-]+)`)

func extractModel(path string) string {
	match := modelPattern.FindStringSubmatch(filepath.Base(path))
	if len(match) > 1 {
		return match[1]
	}
	return "unknown"
}

func number(value any) float64 {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) {
			return 0
		}
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
