package service

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// openAIRefusalContextKey stores a request-scoped, protocol-confirmed refusal.
// A refusal is a terminal outcome for this request. It is deliberately kept
// separate from CyberPolicyMark: a normal content refusal must not trigger the
// cyber-policy session block or moderation penalty path.
const openAIRefusalContextKey = "openai_refusal"

type OpenAIRefusalMark struct {
	Code           string
	Reason         string
	Body           string
	UpstreamStatus int
}

func MarkOpenAIRefusal(c *gin.Context, mark OpenAIRefusalMark) {
	if c == nil {
		return
	}
	if existing := GetOpenAIRefusal(c); existing != nil {
		// Streaming refusal deltas are followed by refusal.done. Keep the
		// complete terminal reason when it arrives instead of retaining only
		// the first fragment.
		if strings.TrimSpace(mark.Reason) != "" {
			existing.Reason = strings.TrimSpace(mark.Reason)
		}
		if strings.TrimSpace(mark.Body) != "" {
			existing.Body = truncateString(strings.TrimSpace(mark.Body), 4096)
		}
		if mark.UpstreamStatus > 0 {
			existing.UpstreamStatus = mark.UpstreamStatus
		}
		return
	}
	mark.Code = strings.TrimSpace(mark.Code)
	if mark.Code == "" {
		mark.Code = "refusal"
	}
	mark.Reason = strings.TrimSpace(mark.Reason)
	mark.Body = truncateString(strings.TrimSpace(mark.Body), 4096)
	c.Set(openAIRefusalContextKey, &mark)
	if mark.Reason != "" {
		status := mark.UpstreamStatus
		if status <= 0 {
			status = 400
		}
		setOpsUpstreamError(c, status, mark.Reason, mark.Body)
	}
}

func GetOpenAIRefusal(c *gin.Context) *OpenAIRefusalMark {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(openAIRefusalContextKey); ok {
		if mark, ok := value.(*OpenAIRefusalMark); ok && mark != nil {
			return mark
		}
	}
	return nil
}

// detectOpenAIExplicitRefusal only accepts protocol-defined refusal shapes.
// In particular, it never searches arbitrary text for words such as
// "safety", "policy", or "reasoning" because those may be echoed user input.
func detectOpenAIExplicitRefusal(payload []byte) (bool, string, string) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return false, "", ""
	}
	// Durable account/workspace restrictions take precedence over request-scoped
	// content refusals. Those are the only refusal-shaped responses that may
	// rotate or suspend a credential.
	if isOpenAIUpstreamAccessStateError("", payload) {
		return false, "", ""
	}
	reason := func(value string) string { return strings.TrimSpace(value) }
	for _, path := range []string{"type", "event"} {
		switch strings.TrimSpace(gjson.GetBytes(payload, path).String()) {
		case "response.refusal.delta":
			if value := reason(gjson.GetBytes(payload, "delta").String()); value != "" {
				return true, "refusal", value
			}
			return true, "refusal", ""
		case "response.refusal.done":
			if value := reason(gjson.GetBytes(payload, "refusal").String()); value != "" {
				return true, "refusal", value
			}
			return true, "refusal", ""
		case "response.content_part.added", "response.content_part.done":
			part := gjson.GetBytes(payload, "part")
			if strings.EqualFold(strings.TrimSpace(part.Get("type").String()), "refusal") {
				return true, "refusal", reason(part.Get("refusal").String())
			}
		}
	}

	for _, path := range []string{"output", "response.output"} {
		for _, item := range gjson.GetBytes(payload, path).Array() {
			if !strings.EqualFold(strings.TrimSpace(item.Get("type").String()), "message") {
				continue
			}
			for _, part := range item.Get("content").Array() {
				if strings.EqualFold(strings.TrimSpace(part.Get("type").String()), "refusal") {
					return true, "refusal", reason(part.Get("refusal").String())
				}
			}
		}
	}

	for _, path := range []string{"choices.0.message", "choices.0.delta"} {
		message := gjson.GetBytes(payload, path)
		refusal := message.Get("refusal")
		if refusal.Type == gjson.String && reason(refusal.String()) != "" {
			return true, "refusal", reason(refusal.String())
		}
	}

	for _, path := range []string{"error.code", "response.error.code", "code"} {
		code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, path).String()))
		switch code {
		case "content_policy", "content_policy_violation", "content_filter", "safety_error", "safety_violation", "moderation_blocked", "policy_violation", "refusal":
			message := gjson.GetBytes(payload, "error.message").String()
			if message == "" {
				message = gjson.GetBytes(payload, "response.error.message").String()
			}
			return true, code, reason(message)
		}
	}
	for _, path := range []string{"error.type", "response.error.type", "type"} {
		typ := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, path).String()))
		switch typ {
		case "content_policy", "content_policy_violation", "content_filter", "safety_error", "safety_violation", "moderation_blocked", "policy_violation", "refusal":
			message := gjson.GetBytes(payload, "error.message").String()
			if message == "" {
				message = gjson.GetBytes(payload, "response.error.message").String()
			}
			return true, typ, reason(message)
		}
	}
	for _, path := range []string{"incomplete_details.reason", "response.incomplete_details.reason"} {
		if strings.EqualFold(strings.TrimSpace(gjson.GetBytes(payload, path).String()), "content_filter") {
			return true, "content_filter", "content filtered by upstream"
		}
	}
	return false, "", ""
}

func isOpenAIExplicitRefusal(payload []byte) bool {
	hit, _, _ := detectOpenAIExplicitRefusal(payload)
	return hit
}

func markOpenAIExplicitRefusal(c *gin.Context, payload []byte, upstreamStatus int) bool {
	hit, code, reason := detectOpenAIExplicitRefusal(payload)
	if !hit {
		return false
	}
	MarkOpenAIRefusal(c, OpenAIRefusalMark{
		Code:           code,
		Reason:         reason,
		Body:           string(payload),
		UpstreamStatus: upstreamStatus,
	})
	return true
}
