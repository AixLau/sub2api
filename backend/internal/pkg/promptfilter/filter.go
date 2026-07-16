package promptfilter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// BuiltinSourceRevision identifies the exact upstream rule set used here.
	BuiltinSourceRevision = "codex2api@6793e0b09fe170895878f73f256a3d7ee7e5a08b"
	// BuiltinRuleSetRevision identifies the upstream rules plus local supplemental
	// cyber and prompt-injection rules maintained in this repository.
	BuiltinRuleSetRevision = BuiltinSourceRevision + "+" + supplementalSourceRevision + "+" + candidateSourceRevision
	BuiltinSourceURL       = "https://github.com/james-6-23/codex2api/tree/6793e0b09fe170895878f73f256a3d7ee7e5a08b/security/promptfilter"
	BuiltinSourceAuthor    = "james-6-23/codex2api"
	// The upstream rule set is redistributed in this derivative with permission from its author.
	BuiltinSourcePermission = "redistributed with permission from the upstream author"
)

const (
	ModeOff     = "off"
	ModeObserve = "observe"
	ModeWarn    = "warn"
	ModeBlock   = "block"

	DefaultThreshold       = 50
	DefaultStrictThreshold = 90
	DefaultMaxTextLength   = 80 * 1024
	defaultHeadScanLength  = 64 * 1024
	defaultTailScanLength  = 16 * 1024

	ActionAllow   = "allow"
	ActionObserve = "observe"
	ActionWarn    = "warn"
	ActionReview  = "review"
	ActionBlock   = "block"
)

type PatternConfig struct {
	Name           string `json:"name"`
	Regex          string `json:"regex,omitempty"`
	Weight         int    `json:"weight"`
	Category       string `json:"category,omitempty"`
	Strict         bool   `json:"strict,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	// Pattern is accepted for compatibility with older in-process callers. New rules use Regex.
	Pattern string `json:"pattern,omitempty"`
}

type Config struct {
	Mode             string          `json:"mode"`
	Threshold        int             `json:"threshold"`
	StrictThreshold  int             `json:"strict_threshold"`
	MaxTextLength    int             `json:"max_text_length"`
	CustomPatterns   []PatternConfig `json:"custom_patterns,omitempty"`
	DisabledPatterns []string        `json:"disabled_patterns,omitempty"`
}

type Match struct {
	Name           string `json:"name"`
	Weight         int    `json:"weight"`
	Category       string `json:"category,omitempty"`
	Strict         bool   `json:"strict,omitempty"`
	Operational    bool   `json:"operational,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	// StartByte and EndByte identify the raw user-text region that triggered the
	// pattern. They are intentionally excluded from serialized diagnostics: the
	// moderation service keeps the actual bounded payload in its encrypted
	// evidence store instead of leaking it through rule metadata.
	StartByte int `json:"-"`
	EndByte   int `json:"-"`
}

type Verdict struct {
	Action          string  `json:"action"`
	Score           int     `json:"score"`
	RawScore        int     `json:"raw_score"`
	StrictScore     int     `json:"strict_score"`
	Threshold       int     `json:"threshold"`
	StrictThreshold int     `json:"strict_threshold"`
	StrictHit       bool    `json:"strict_hit"`
	OperationalHit  bool    `json:"operational_hit"`
	ReviewRequired  bool    `json:"review_required"`
	Matches         []Match `json:"matches,omitempty"`
	TextPreview     string  `json:"text_preview,omitempty"`
	ExtractedRunes  int     `json:"extracted_runes"`
	SourceRevision  string  `json:"source_revision"`
}

type compiledPattern struct {
	cfg         PatternConfig
	re          *regexp.Regexp
	operational bool
}

type Engine struct {
	cfg      Config
	patterns []compiledPattern
}

var engineCache sync.Map // map[string]*Engine

func BuiltinPatternConfigs() []PatternConfig {
	out := builtinPatternConfigs()
	for idx := range out {
		if out[idx].Regex == "" {
			out[idx].Regex = out[idx].Pattern
		}
		out[idx].Pattern = ""
		if strings.TrimSpace(out[idx].SourceRevision) == "" {
			out[idx].SourceRevision = BuiltinSourceRevision
		}
	}
	return out
}

func builtinPatternConfigs() []PatternConfig {
	out := make([]PatternConfig, 0, len(defaultPatternConfigs)+len(supplementalPatternConfigs)+len(candidatePatternConfigs))
	out = append(out, defaultPatternConfigs...)
	out = append(out, supplementalPatternConfigs...)
	for idx := len(defaultPatternConfigs); idx < len(out); idx++ {
		if strings.TrimSpace(out[idx].SourceRevision) == "" {
			out[idx].SourceRevision = supplementalSourceRevision
		}
	}
	candidateStart := len(out)
	out = append(out, candidatePatternConfigs...)
	for idx := candidateStart; idx < len(out); idx++ {
		if strings.TrimSpace(out[idx].SourceRevision) == "" {
			out[idx].SourceRevision = candidateSourceRevision
		}
	}
	return out
}

// NewEngine compiles the pinned built-in set plus optional administrator rules.
func NewEngine(cfg Config) (*Engine, error) {
	cfg = normalizeConfig(cfg)
	keyBytes, _ := json.Marshal(cfg)
	cacheKey := string(keyBytes)
	if cached, ok := engineCache.Load(cacheKey); ok {
		return cached.(*Engine), nil
	}
	disabled := make(map[string]struct{}, len(cfg.DisabledPatterns))
	for _, name := range cfg.DisabledPatterns {
		disabled[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	builtins := builtinPatternConfigs()
	merged := make([]PatternConfig, 0, len(builtins)+len(cfg.CustomPatterns))
	for _, pattern := range builtins {
		if strings.TrimSpace(pattern.SourceRevision) == "" {
			pattern.SourceRevision = BuiltinSourceRevision
		}
		if pattern.Regex == "" {
			pattern.Regex = pattern.Pattern
		}
		pattern.Pattern = ""
		merged = append(merged, pattern)
	}
	for _, pattern := range cfg.CustomPatterns {
		if strings.TrimSpace(pattern.SourceRevision) == "" {
			pattern.SourceRevision = "custom"
		}
		merged = append(merged, pattern)
	}
	patterns := make([]compiledPattern, 0, len(merged))
	for _, pattern := range merged {
		pattern.Name = strings.TrimSpace(pattern.Name)
		pattern.Regex = strings.TrimSpace(pattern.Regex)
		pattern.Pattern = strings.TrimSpace(pattern.Pattern)
		if pattern.Regex == "" {
			pattern.Regex = pattern.Pattern
		}
		pattern.Category = strings.TrimSpace(pattern.Category)
		pattern.SourceRevision = strings.TrimSpace(pattern.SourceRevision)
		if pattern.Name == "" || pattern.Regex == "" || pattern.Weight <= 0 {
			continue
		}
		if _, ok := disabled[strings.ToLower(pattern.Name)]; ok {
			continue
		}
		if pattern.Enabled != nil && !*pattern.Enabled {
			continue
		}
		re, err := regexp.Compile(pattern.Regex)
		if err != nil {
			return nil, fmt.Errorf("compile prompt filter pattern %q: %w", pattern.Name, err)
		}
		patterns = append(patterns, compiledPattern{
			cfg:         pattern,
			re:          re,
			operational: operationalPattern(pattern.Name),
		})
	}
	engine := &Engine{cfg: cfg, patterns: patterns}
	actual, _ := engineCache.LoadOrStore(cacheKey, engine)
	return actual.(*Engine), nil
}

// Inspect applies local evidence scoring. It never calls an external model.
func Inspect(text string, cfg Config) Verdict {
	cfg = normalizeConfig(cfg)
	verdict := Verdict{
		Action:          ActionAllow,
		Threshold:       cfg.Threshold,
		StrictThreshold: cfg.StrictThreshold,
		ExtractedRunes:  utf8.RuneCountInString(text),
		SourceRevision:  BuiltinRuleSetRevision,
	}
	if cfg.Mode == ModeOff || strings.TrimSpace(text) == "" {
		return verdict
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		verdict.Action = ActionReview
		verdict.ReviewRequired = true
		return verdict
	}
	return engine.inspect(text)
}

func (e *Engine) inspect(text string) Verdict {
	cfg := e.cfg
	verdict := Verdict{
		Action:          ActionAllow,
		Threshold:       cfg.Threshold,
		StrictThreshold: cfg.StrictThreshold,
		ExtractedRunes:  utf8.RuneCountInString(text),
		SourceRevision:  BuiltinRuleSetRevision,
	}
	scanText := normalizeForScan(limitScanText(text, cfg.MaxTextLength))
	if utf8.RuneCountInString(scanText) < 3 {
		return verdict
	}
	matchesByName := make(map[string]Match)
	for _, pattern := range e.patterns {
		if pattern.re.FindStringIndex(scanText) == nil {
			continue
		}
		match := Match{
			Name:           pattern.cfg.Name,
			Weight:         pattern.cfg.Weight,
			Category:       pattern.cfg.Category,
			Strict:         pattern.cfg.Strict,
			Operational:    pattern.operational,
			SourceRevision: pattern.cfg.SourceRevision,
		}
		// scanText is whitespace-normalized for stable scoring, so its offsets
		// cannot safely be projected back to the request. A second raw lookup is
		// used only to locate the context window sent to the reviewer.
		if rawSpan := pattern.re.FindStringIndex(text); len(rawSpan) == 2 {
			match.StartByte = rawSpan[0]
			match.EndByte = rawSpan[1]
		}
		matchesByName[pattern.cfg.Name] = match
	}
	if len(matchesByName) == 0 {
		return verdict
	}
	for _, match := range matchesByName {
		verdict.Matches = append(verdict.Matches, match)
		verdict.RawScore += match.Weight
		if match.Strict {
			verdict.StrictScore += match.Weight
		}
		if match.Strict && match.Operational {
			verdict.OperationalHit = true
		}
	}
	sort.Slice(verdict.Matches, func(i, j int) bool {
		if verdict.Matches[i].Weight == verdict.Matches[j].Weight {
			return verdict.Matches[i].Name < verdict.Matches[j].Name
		}
		return verdict.Matches[i].Weight > verdict.Matches[j].Weight
	})
	verdict.Score = verdict.RawScore
	verdict.StrictHit = verdict.StrictScore >= cfg.StrictThreshold
	verdict.ReviewRequired = !verdict.OperationalHit || !verdict.StrictHit
	if verdict.OperationalHit {
		verdict.ReviewRequired = false
	}
	switch cfg.Mode {
	case ModeBlock:
		if verdict.OperationalHit {
			verdict.Action = ActionBlock
		} else {
			verdict.Action = ActionReview
		}
	case ModeWarn:
		verdict.Action = ActionWarn
	case ModeObserve:
		verdict.Action = ActionObserve
	default:
		verdict.Action = ActionAllow
	}
	return verdict
}

func normalizeConfig(cfg Config) Config {
	if cfg.Mode == "" {
		cfg.Mode = ModeObserve
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case ModeOff:
		cfg.Mode = ModeOff
	case ModeBlock:
		cfg.Mode = ModeBlock
	case ModeWarn:
		cfg.Mode = ModeWarn
	default:
		cfg.Mode = ModeObserve
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultThreshold
	}
	if cfg.StrictThreshold <= 0 {
		cfg.StrictThreshold = DefaultStrictThreshold
	}
	if cfg.StrictThreshold < cfg.Threshold {
		cfg.StrictThreshold = cfg.Threshold
	}
	if cfg.MaxTextLength <= 0 || cfg.MaxTextLength > 1024*1024 {
		cfg.MaxTextLength = DefaultMaxTextLength
	}
	return cfg
}

func limitScanText(text string, maxRunes int) string {
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return text
	}
	head := defaultHeadScanLength
	tail := defaultTailScanLength
	if maxRunes < head+tail {
		head = maxRunes / 2
		tail = maxRunes - head
	}
	return string(runes[:head]) + "\n[...prompt-filter-truncated...]\n" + string(runes[len(runes)-tail:])
}

func normalizeForScan(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
}

func operationalPattern(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "credential_theft", "evasion", "operational_remote_access_request",
		"reverse_engineering_secret_extraction", "reverse_engineering_license_bypass", "reverse_engineering_anti_debug_bypass",
		"frida_hook_abuse", "license_cracking", "data_exfiltration", "ransomware_deployment", "credential_dumping",
		"token_theft", "mass_exploitation", "jailbreak_operational_request", "prompt_injection_override",
		"system_prompt_extraction", "agent_tool_permission_bypass", "web_exploitation_unauthorized_harm_request",
		"binary_exploitation_unauthorized_harm_request", "crypto_unauthorized_key_theft_request",
		"pentest_unauthorized_harm_request", "credential_attack_operational_request":
		return true
	default:
		return false
	}
}
