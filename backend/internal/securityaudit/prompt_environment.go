package securityaudit

import (
	"regexp"
	"strings"
)

var environmentContextTagPattern = regexp.MustCompile(`</?environment_context>`)

// An environment-only content part must not displace the latest actual user
// text as the priority segment. Its contents are still included in the snapshot.
func hasTextOutsideEnvironment(value string) bool {
	end := 0
	for _, span := range environmentContextRanges(value) {
		if strings.TrimSpace(value[end:span[0]]) != "" {
			return true
		}
		end = span[1]
	}
	return strings.TrimSpace(value[end:]) != ""
}

// preparePromptAuditScanText optimizes only the outbound scanner input. The
// snapshot, hash, queue payload and stored full prompt retain the original text.
// Tags are client-controlled: every distinct environment block is still audited.
func preparePromptAuditScanText(value string) string {
	if !strings.Contains(value, "<environment_context>") {
		return value
	}
	var primary, background []string
	seen := make(map[string]struct{})
	for _, section := range strings.Split(value, promptAuditPrioritySeparator) {
		ranges := environmentContextRanges(section)
		if len(ranges) == 0 {
			if strings.TrimSpace(section) != "" {
				primary = append(primary, section)
			}
			continue
		}
		var parts []string
		end := 0
		for _, span := range ranges {
			if text := strings.TrimSpace(section[end:span[0]]); text != "" {
				parts = append(parts, text)
			}
			block := section[span[0]:span[1]]
			if _, duplicate := seen[block]; !duplicate {
				seen[block] = struct{}{}
				background = append(background, block)
			}
			end = span[1]
		}
		if text := strings.TrimSpace(section[end:]); text != "" {
			parts = append(parts, text)
		}
		if len(parts) > 0 {
			primary = append(primary, strings.Join(parts, "\n\n"))
		}
	}
	if len(background) == 0 {
		return value
	}
	// Keep environment data out of the prioritized request chunk, but retain it
	// for subsequent scanning even when there is no text outside the tags.
	primary = append(primary, strings.Join(background, "\n\n"))
	return strings.Join(primary, promptAuditPrioritySeparator)
}

// environmentContextRanges recognizes only balanced, exact tag pairs. Nested
// blocks remain intact; ambiguous or truncated input is audited verbatim. This
// is a text-boundary parser, not XML: environment contents may contain raw code.
func environmentContextRanges(value string) [][2]int {
	var ranges [][2]int
	depth, start := 0, 0
	for _, tag := range environmentContextTagPattern.FindAllStringIndex(value, -1) {
		if value[tag[0]+1] != '/' {
			if depth == 0 {
				start = tag[0]
			}
			depth++
			continue
		}
		if depth == 0 {
			return nil
		}
		depth--
		if depth == 0 {
			ranges = append(ranges, [2]int{start, tag[1]})
		}
	}
	if depth != 0 {
		return nil
	}
	return ranges
}
