package ticket

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StatePrefix     = "gAAAAA"
	CredentialTTL   = 240 * time.Second
	DefaultLifetime = time.Hour
)

var ErrInvalidState = errors.New("invalid codex turn-state envelope")

type Shape struct {
	Blocks   int
	IssuedAt time.Time
}

// ParseState validates the Fernet-like envelope used by Codex ticket values.
// It intentionally validates the envelope shape and timestamp only; the state
// blob is opaque and cannot be decrypted by the gateway.
func ParseState(value string) (Shape, error) {
	value = strings.TrimSpace(value)
	if len(value) > 2048 || strings.ContainsAny(value, "\r\n\t ") {
		return Shape{}, ErrInvalidState
	}
	core := strings.TrimRight(value, "=")
	if len(value)-len(core) > 2 {
		return Shape{}, ErrInvalidState
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(core)
	if err != nil || len(raw) < 73 || raw[0] != 0x80 || (len(raw)-57)%16 != 0 {
		return Shape{}, ErrInvalidState
	}
	issued := binary.BigEndian.Uint64(raw[1:9])
	if issued < 1577836800 || issued >= 4102444800 {
		return Shape{}, ErrInvalidState
	}
	return Shape{Blocks: (len(raw) - 57) / 16, IssuedAt: time.Unix(int64(issued), 0)}, nil
}

type Ticket struct {
	AccountID             int64                `json:"account_id"`
	Model                 string               `json:"model"`
	State                 string               `json:"state"`
	Length                int                  `json:"length"`
	CapturedAt            time.Time            `json:"captured_at"`
	ExpiresAt             time.Time            `json:"expires_at"`
	Attempts              int                  `json:"attempts,omitempty"`
	Blocks                int                  `json:"blocks,omitempty"`
	IssuedAt              time.Time            `json:"issued_at,omitempty"`
	Identity              string               `json:"identity,omitempty"`
	HarvestProxyURL       string               `json:"harvest_proxy_url,omitempty"`
	HarvestNodeID         string               `json:"harvest_node_id,omitempty"`
	HarvestNodeName       string               `json:"harvest_node_name,omitempty"`
	HarvestNodeProvider   string               `json:"harvest_node_provider,omitempty"`
	HarvestSessionID      string               `json:"harvest_session_id,omitempty"`
	EdgeIP                string               `json:"edge_ip,omitempty"`
	Transport             string               `json:"transport,omitempty"`
	Gateway               string               `json:"gateway,omitempty"`
	HarvestLite           bool                 `json:"harvest_lite,omitempty"`
	HarvestCookies        []string             `json:"harvest_cookies,omitempty"`
	HarvestCookiesAt      time.Time            `json:"harvest_cookies_at,omitempty"`
	HarvestCookieExpiries map[string]time.Time `json:"harvest_cookie_expiries,omitempty"`
	Standby               *Ticket              `json:"standby,omitempty"`
	Revoked               bool                 `json:"revoked,omitempty"`
}

func (t *Ticket) Normalize() {
	if t == nil {
		return
	}
	t.State = strings.TrimSpace(t.State)
	t.Model = strings.TrimSpace(t.Model)
	if t.Length == 0 {
		t.Length = len(t.State)
	}
	if t.CapturedAt.IsZero() {
		t.CapturedAt = time.Now()
	}
	if t.ExpiresAt.IsZero() {
		t.ExpiresAt = t.CapturedAt.Add(DefaultLifetime)
	}
	if t.Standby != nil {
		t.Standby.Normalize()
	}
}

func (t *Ticket) Shape() (Shape, error) {
	if t == nil {
		return Shape{}, ErrInvalidState
	}
	return ParseState(t.State)
}

// IdentityHash binds a ticket to the account identity without storing secrets.
func IdentityHash(chatGPTAccountID, email string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(chatGPTAccountID) + "\x00" + strings.TrimSpace(email)))
	return fmt.Sprintf("%x", h)
}
func (t *Ticket) IdentityMatches(identity string) bool {
	return t != nil && (t.Identity == "" || t.Identity == identity)
}

func (t *Ticket) RemainingUntil() time.Time {
	if t == nil {
		return time.Time{}
	}
	exp := t.ExpiresAt
	if !t.IssuedAt.IsZero() {
		lifetime := DefaultLifetime
		if t.Length == 780 {
			lifetime = CredentialTTL
		}
		e := t.IssuedAt.Add(lifetime)
		if exp.IsZero() || e.Before(exp) {
			exp = e
		}
	}
	if t.Length == 780 {
		if e := t.CookieExpiry(); !e.IsZero() && (exp.IsZero() || e.Before(exp)) {
			exp = e
		}
	}
	return exp
}
func (t *Ticket) CookieExpiry() time.Time {
	if t == nil || len(t.HarvestCookies) == 0 {
		return time.Time{}
	}
	if len(t.HarvestCookieExpiries) > 0 {
		var exp time.Time
		for _, e := range t.HarvestCookieExpiries {
			if !e.IsZero() && (exp.IsZero() || e.Before(exp)) {
				exp = e
			}
		}
		return exp
	}
	at := t.HarvestCookiesAt
	if at.IsZero() {
		at = t.CapturedAt
	}
	if at.IsZero() {
		return time.Time{}
	}
	return at.Add(CredentialTTL)
}
func (t *Ticket) CookiesFresh(now time.Time) bool {
	return len(t.HarvestCookies) > 0 && now.Before(t.CookieExpiry())
}

func (t *Ticket) Validate(now time.Time, targetLength int, transport, gateway string, requireCookies, requireGateway bool) error {
	if t == nil || t.Revoked {
		return errors.New("ticket unavailable")
	}
	t.Normalize()
	if targetLength != 292 && targetLength != 332 && targetLength != 780 {
		return errors.New("unsupported target length")
	}
	lengthOK := t.Length == targetLength && len(t.State) == targetLength
	// The personal 292 profile is also the compatibility sentinel for
	// self-serve business/team variants whose envelope is 332 bytes.
	if targetLength == 292 && t.Length == 332 && len(t.State) == 332 {
		lengthOK = true
	}
	// The 780 profile is the production default, but personal and business
	// accounts can legitimately receive the shorter envelope. Treat it as a
	// dynamic shape selector when configured for 780.
	if targetLength == 780 && (t.Length == 292 || t.Length == 332) && len(t.State) == t.Length {
		lengthOK = true
	}
	if !lengthOK || !strings.HasPrefix(t.State, StatePrefix) {
		return errors.New("ticket length or prefix mismatch")
	}
	shape, err := ParseState(t.State)
	if err != nil {
		return err
	}
	lifetime := DefaultLifetime
	if t.Length == 780 {
		lifetime = CredentialTTL
	}
	if shape.IssuedAt.After(now.Add(30*time.Second)) || !now.Before(shape.IssuedAt.Add(lifetime)) {
		return errors.New("ticket issued time invalid or expired")
	}
	if !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt) {
		return errors.New("ticket expired")
	}
	if t.IssuedAt.IsZero() {
		t.IssuedAt = shape.IssuedAt
	}
	if t.Blocks == 0 {
		t.Blocks = shape.Blocks
	}
	if t.Length == 780 {
		if transport != "" && t.Transport != "" && !strings.EqualFold(t.Transport, transport) {
			return errors.New("ticket transport mismatch")
		}
		if requireCookies && !t.CookiesFresh(now) {
			return errors.New("ticket cookies expired")
		}
		if requireGateway && !GatewayAllowed(t.Gateway, gateway) {
			return errors.New("ticket gateway mismatch")
		}
	}
	return nil
}

func GatewayAllowed(ticketGateway, expected string) bool {
	ticketGateway, expected = strings.TrimSpace(ticketGateway), strings.TrimSpace(expected)
	return expected == "" || ticketGateway == "" || ticketGateway == expected || strings.HasPrefix(ticketGateway, expected+"-")
}

func (t *Ticket) NeedsRefresh(now time.Time, before time.Duration) bool {
	return t == nil || !t.RemainingUntil().After(now.Add(before))
}
func (t *Ticket) Select(now time.Time, targetLength int, transport, gateway string, requireCookies, requireGateway bool) (*Ticket, bool) {
	if t == nil {
		return nil, false
	}
	if t.Validate(now, targetLength, transport, gateway, requireCookies, requireGateway) == nil {
		return t, true
	}
	if t.Standby != nil && t.Standby.Validate(now, targetLength, transport, gateway, requireCookies, requireGateway) == nil {
		return t.Standby, true
	}
	return nil, false
}

func ParseJSON(raw []byte, accountID int64, model string) (*Ticket, error) {
	var t Ticket
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	t.AccountID = accountID
	t.Model = strings.TrimSpace(model)
	t.Normalize()
	if t.State == "" {
		return nil, errors.New("ticket state missing")
	}
	return &t, nil
}
func ParseAny(raw any, accountID int64, model string) (*Ticket, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return ParseJSON(b, accountID, model)
}

func ParseHarvest(headers map[string][]string, body []byte, accountID int64, model string, now time.Time, ttl time.Duration) (*Ticket, error) {
	state := ""
	for k, v := range headers {
		if strings.EqualFold(k, "x-codex-turn-state") && len(v) > 0 {
			state = v[0]
			break
		}
	}
	if state == "" {
		var payload struct {
			State     string   `json:"state"`
			TurnState string   `json:"turn_state"`
			Ticket    string   `json:"ticket"`
			SessionID string   `json:"session_id"`
			Gateway   string   `json:"gateway"`
			Transport string   `json:"transport"`
			Cookies   []string `json:"cookies"`
		}
		if json.Unmarshal(body, &payload) == nil {
			state = payload.State
			if state == "" {
				state = payload.TurnState
			}
			if state == "" {
				state = payload.Ticket
			}
			if state == "" {
				return nil, errors.New("harvest response missing x-codex-turn-state")
			}
			t := &Ticket{AccountID: accountID, Model: model, State: state, CapturedAt: now, ExpiresAt: now.Add(ttl), HarvestSessionID: payload.SessionID, Gateway: payload.Gateway, Transport: payload.Transport, HarvestCookies: payload.Cookies}
			t.Normalize()
			return t, nil
		}
	}
	if state == "" {
		return nil, errors.New("harvest response missing x-codex-turn-state")
	}
	t := &Ticket{AccountID: accountID, Model: model, State: state, CapturedAt: now, ExpiresAt: now.Add(ttl)}
	t.Normalize()
	return t, nil
}

type Summary struct {
	AccountID        int64      `json:"account_id"`
	Model            string     `json:"model"`
	Length           int        `json:"length,omitempty"`
	Ready            bool       `json:"ready"`
	RemainingSeconds int64      `json:"remaining_seconds"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Standby          bool       `json:"standby,omitempty"`
	StandbyExpiresAt *time.Time `json:"standby_expires_at,omitempty"`
	Transport        string     `json:"transport,omitempty"`
	Gateway          string     `json:"gateway,omitempty"`
	EdgeIP           string     `json:"edge_ip,omitempty"`
	CookieCount      int        `json:"cookie_count,omitempty"`
	CookieExpiresAt  *time.Time `json:"cookie_expires_at,omitempty"`
	Revoked          bool       `json:"revoked,omitempty"`
}

func (t *Ticket) Summary(now time.Time, targetLength int, transport, gateway string, requireCookies, requireGateway bool) Summary {
	s := Summary{}
	if t == nil {
		return s
	}
	s.AccountID = t.AccountID
	s.Model = t.Model
	s.Length = t.Length
	s.Transport = t.Transport
	s.Gateway = t.Gateway
	s.EdgeIP = t.EdgeIP
	s.CookieCount = len(t.HarvestCookies)
	if e := t.CookieExpiry(); !e.IsZero() {
		s.CookieExpiresAt = &e
	}
	selected, ok := t.Select(now, targetLength, transport, gateway, requireCookies, requireGateway)
	if ok {
		s.Ready = true
		s.ExpiresAtPtr(selected.RemainingUntil(), &s.ExpiresAt)
		s.RemainingSeconds = maxInt64(int64(selected.RemainingUntil().Sub(now)/time.Second), 0)
		if selected != t {
			s.Standby = true
		}
	} else {
		s.Revoked = t.Revoked
	}
	if t.Standby != nil {
		if e := t.Standby.RemainingUntil(); !e.IsZero() {
			s.StandbyExpiresAt = &e
		}
	}
	return s
}
func (s Summary) ExpiresAtPtr(v time.Time, p **time.Time) {
	if !v.IsZero() {
		*p = &v
	}
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
