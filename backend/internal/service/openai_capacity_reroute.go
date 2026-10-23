package service

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

// OpenAICapacityRerouteState tracks the bounded, request-local response to a
// provider capacity shed within the existing OpenAIRequestScopedRetryWindow.
//
// The first capacity retry deliberately keeps the current turn-state and route
// cookies: a short overload may clear on its own. The second retry drops or
// replaces the turn-state ticket, and the third additionally removes the
// __cflb/__oailb route cookies. Later retries keep the stage-3 behavior until
// the normal two-minute same-account retry window is exhausted.
type OpenAICapacityRerouteState struct {
	mu sync.Mutex

	stage          int
	ticketOverride string
	lastSentTicket string
}

func NewOpenAICapacityRerouteState() *OpenAICapacityRerouteState {
	return &OpenAICapacityRerouteState{}
}

type openAICapacityRerouteContextKey struct{}

func WithOpenAICapacityReroute(ctx context.Context, state *OpenAICapacityRerouteState) context.Context {
	if state == nil {
		return ctx
	}
	return context.WithValue(ctx, openAICapacityRerouteContextKey{}, state)
}

func OpenAICapacityRerouteFromContext(ctx context.Context) *OpenAICapacityRerouteState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(openAICapacityRerouteContextKey{}).(*OpenAICapacityRerouteState)
	return state
}

// AdvanceOpenAICapacityReroute records one capacity-shed failure. Callers must
// only invoke it after deciding to retry the same account; quota and generic
// 429s must not advance this state.
func AdvanceOpenAICapacityReroute(ctx context.Context, responseHeaders http.Header) {
	state := OpenAICapacityRerouteFromContext(ctx)
	if state == nil {
		return
	}
	state.advance(responseHeaders)
}

// ResetOpenAICapacityReroute clears the stage after an account switch. The new
// account has its own ticket and route cookies; carrying the old stage into the
// first attempt would strip valid credentials before the provider rejects them.
func ResetOpenAICapacityReroute(ctx context.Context) {
	state := OpenAICapacityRerouteFromContext(ctx)
	if state == nil {
		return
	}
	state.reset()
}

// OpenAICapacityRerouteStage returns the current stage for retry logging. It is
// intentionally read-only; callers must not use it to drive routing decisions.
func OpenAICapacityRerouteStage(ctx context.Context) int {
	state := OpenAICapacityRerouteFromContext(ctx)
	if state == nil {
		return 0
	}
	return state.currentStage()
}

func (s *OpenAICapacityRerouteState) advance(responseHeaders http.Header) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage < 3 {
		s.stage++
	}
	if responseHeaders != nil {
		if ticket := strings.TrimSpace(responseHeaders.Get(openAICodexTurnStateHeader)); ticket != "" {
			s.ticketOverride = ticket
		}
	}
}

func (s *OpenAICapacityRerouteState) reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stage = 0
	s.ticketOverride = ""
}

func (s *OpenAICapacityRerouteState) currentStage() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stage
}

func (s *OpenAICapacityRerouteState) override() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ticketOverride
}

func (s *OpenAICapacityRerouteState) lastSent() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSentTicket
}

func (s *OpenAICapacityRerouteState) noteSent(ticket string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSentTicket = strings.TrimSpace(ticket)
}

// applyOpenAICapacityReroute rewrites the final outbound request after the
// ticket plugin has injected its cached ticket and route cookies. The host
// remains the final authority on the wire: stage 2 removes or replaces the
// turn-state, and stage 3 also strips the route-cookie pair.
func applyOpenAICapacityReroute(request *http.Request, account *Account) {
	if request == nil || account == nil || !account.IsOpenAIOAuth() {
		return
	}
	state := OpenAICapacityRerouteFromContext(request.Context())
	if state == nil {
		return
	}
	state.applyToRequest(request)
}

func (s *OpenAICapacityRerouteState) applyToRequest(request *http.Request) {
	if s == nil || request == nil {
		return
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	stage := s.currentStage()
	if stage >= 2 {
		override := s.override()
		current := headerValueCaseInsensitive(request.Header, pluginOutboundTicketHeader)
		switch {
		case override != "" && override != s.lastSent():
			deleteHeaderCaseInsensitive(request.Header, pluginOutboundTicketHeader)
			request.Header[http.CanonicalHeaderKey(pluginOutboundTicketHeader)] = []string{override}
		case current != "":
			deleteHeaderCaseInsensitive(request.Header, pluginOutboundTicketHeader)
		}
	}
	if stage >= 3 {
		stripOpenAIRouteCookies(request.Header)
	}
	s.noteSent(headerValueCaseInsensitive(request.Header, pluginOutboundTicketHeader))
}

// stripOpenAIRouteCookies removes only the two Codex route-affinity cookies.
// Every other cookie is preserved verbatim so unrelated ChatGPT session state
// is not lost when a capacity retry changes the edge route.
func stripOpenAIRouteCookies(headers http.Header) {
	if headers == nil {
		return
	}
	values := cookieHeaderValues(headers)
	if len(values) == 0 {
		return
	}
	kept := make([]string, 0, len(values))
	for _, line := range values {
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name := part
			if index := strings.IndexByte(part, '='); index >= 0 {
				name = strings.TrimSpace(part[:index])
			}
			if strings.EqualFold(name, "__cflb") || strings.EqualFold(name, "__oailb") {
				continue
			}
			kept = append(kept, part)
		}
	}
	deleteHeaderCaseInsensitive(headers, "Cookie")
	if len(kept) > 0 {
		headers[http.CanonicalHeaderKey("Cookie")] = []string{strings.Join(kept, "; ")}
	}
}

func cookieHeaderValues(headers http.Header) []string {
	if headers == nil {
		return nil
	}
	var values []string
	for key, headerValues := range headers {
		if strings.EqualFold(key, "Cookie") {
			values = append(values, headerValues...)
		}
	}
	return values
}
