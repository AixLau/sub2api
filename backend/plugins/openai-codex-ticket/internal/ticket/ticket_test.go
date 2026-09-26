package ticket

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testState returns an opaque Fernet-shaped token with the requested wire
// length. The ticket implementation must validate its envelope shape without
// attempting to decrypt the upstream-owned value.
func testState(t *testing.T, length int, issuedAt time.Time) string {
	t.Helper()
	var blocks int
	var padding string
	switch length {
	case 292:
		// 57-byte envelope prefix/suffix plus ten 16-byte ciphertext blocks
		// encodes to 290 characters and uses two base64 padding characters.
		blocks, padding = 10, "=="
	case 780:
		// Thirty-three blocks encode to exactly 780 characters.
		blocks = 33
	default:
		t.Fatalf("unsupported test ticket length %d", length)
	}
	raw := make([]byte, 57+16*blocks)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issuedAt.Unix()))
	for i := 9; i < len(raw); i++ {
		raw[i] = byte(i*31 + 7)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if padding != "" {
		require.Equal(t, length-len(padding), len(encoded))
		encoded += padding
	}
	require.Len(t, encoded, length)
	require.True(t, strings.HasPrefix(encoded, StatePrefix))
	return encoded
}

func TestParseStateAccepts292And780Shapes(t *testing.T) {
	issued := time.Unix(1_735_689_600, 0) // 2025-01-01
	tests := []struct {
		name   string
		length int
		blocks int
	}{
		{name: "292", length: 292, blocks: 10},
		{name: "780", length: 780, blocks: 33},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shape, err := ParseState(testState(t, tc.length, issued))
			require.NoError(t, err)
			require.Equal(t, tc.blocks, shape.Blocks)
			require.Equal(t, issued, shape.IssuedAt)
		})
	}
}

func TestTicketValidateAcceptsTeam332WithPersonalProfile(t *testing.T) {
	now := time.Unix(1_735_689_600, 0)
	state := testState332(t, now.Add(-time.Minute))
	value := &Ticket{State: state, Length: 332, CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, value.Validate(now, 292, "sse", "", false, false))
	require.NoError(t, value.Validate(now, 780, "sse", "", false, false))
}

func testState332(t *testing.T, issuedAt time.Time) string {
	t.Helper()
	raw := make([]byte, 57+16*12)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issuedAt.Unix()))
	for i := 9; i < len(raw); i++ {
		raw[i] = byte(i*17 + 3)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	require.Len(t, encoded, 332)
	require.True(t, strings.HasPrefix(encoded, StatePrefix))
	return encoded
}

func TestParseStateRejectsMalformedOrUnsafeValues(t *testing.T) {
	now := time.Unix(1_735_689_600, 0)
	valid := testState(t, 292, now)
	for name, value := range map[string]string{
		"empty":         "",
		"short":         "gAAAAA",
		"whitespace":    valid[:20] + " " + valid[20:],
		"bad alphabet":  valid[:20] + "!" + valid[21:],
		"wrong version": "f" + valid[1:],
		"bad timestamp": testState(t, 292, time.Unix(1_577_836_799, 0)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseState(value)
			require.ErrorIs(t, err, ErrInvalidState)
		})
	}
}

func TestTicketValidateBindsLifetimeAndTransportMetadata(t *testing.T) {
	now := time.Unix(1_735_689_600, 0)
	state := testState(t, 292, now.Add(-time.Minute))
	ticket := &Ticket{
		State:      state,
		Length:     292,
		CapturedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
	}
	require.NoError(t, ticket.Validate(now, 292, "sse", "unified-88", false, false))
	require.Equal(t, now.Add(-time.Minute), ticket.IssuedAt)

	tests := []struct {
		name   string
		mutate func(*Ticket)
		want   string
	}{
		{name: "revoked", mutate: func(v *Ticket) { v.Revoked = true }, want: "ticket unavailable"},
		{name: "expired", mutate: func(v *Ticket) { v.ExpiresAt = now.Add(-time.Second) }, want: "ticket expired"},
		{name: "length", mutate: func(v *Ticket) { v.Length = 780 }, want: "ticket length or prefix mismatch"},
		{name: "state prefix", mutate: func(v *Ticket) { v.State = "x" + v.State[1:] }, want: "ticket length or prefix mismatch"},
		{name: "future issued", mutate: func(v *Ticket) { v.State = testState(t, 292, now.Add(time.Minute)) }, want: "ticket issued time invalid or expired"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clone := *ticket
			tc.mutate(&clone)
			err := clone.Validate(now, 292, "sse", "unified-88", false, false)
			require.EqualError(t, err, tc.want)
		})
	}

	state780 := testState(t, 780, now.Add(-time.Minute))
	websocket := &Ticket{State: state780, Length: 780, CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Transport: "websocket", Gateway: "unified-88-abc"}
	require.NoError(t, websocket.Validate(now, 780, "websocket", "unified-88", false, true))
	require.EqualError(t, websocket.Validate(now, 780, "sse", "unified-88", false, false), "ticket transport mismatch")
	require.EqualError(t, websocket.Validate(now, 780, "websocket", "other", false, true), "ticket gateway mismatch")
}

func TestTicketValidateChecks780CookiesAndIdentity(t *testing.T) {
	now := time.Unix(1_735_689_600, 0)
	state := testState(t, 780, now.Add(-time.Minute))
	value := &Ticket{
		State:            state,
		Length:           780,
		CapturedAt:       now.Add(-time.Minute),
		ExpiresAt:        now.Add(time.Hour),
		Transport:        "sse",
		Gateway:          "unified-88",
		HarvestCookies:   []string{"__Secure-next-auth.session-token=abc"},
		HarvestCookiesAt: now.Add(-time.Minute),
		Identity:         "identity-a",
	}
	require.NoError(t, value.Validate(now, 780, "sse", "unified-88", true, true))
	require.True(t, value.IdentityMatches("identity-a"))
	require.False(t, value.IdentityMatches("identity-b"))

	value.HarvestCookiesAt = now.Add(-CredentialTTL - time.Second)
	require.EqualError(t, value.Validate(now, 780, "sse", "unified-88", true, true), "ticket cookies expired")
	value.Identity = ""
	require.True(t, value.IdentityMatches("any-identity"), "legacy/unbound tickets remain identity-agnostic")
}

func TestTicketSelectFallsBackToValidStandby(t *testing.T) {
	now := time.Unix(1_735_689_600, 0)
	primary := &Ticket{
		State:      testState(t, 292, now.Add(-2*time.Hour)),
		Length:     292,
		CapturedAt: now.Add(-2 * time.Hour),
		ExpiresAt:  now.Add(time.Hour),
		Standby: &Ticket{
			State:      testState(t, 292, now.Add(-time.Minute)),
			Length:     292,
			CapturedAt: now.Add(-time.Minute),
			ExpiresAt:  now.Add(time.Hour),
		},
	}
	selected, ok := primary.Select(now, 292, "sse", "", false, false)
	require.True(t, ok)
	require.NotSame(t, primary, selected)
	require.Equal(t, primary.Standby.State, selected.State)
}

func TestParseHarvestReadsHeaderAndJSONForms(t *testing.T) {
	now := time.Unix(1_735_689_600, 0)
	state := testState(t, 292, now)
	fromHeader, err := ParseHarvest(map[string][]string{"X-Codex-Turn-State": {state}}, nil, 7, "gpt-6-sol", now, time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(7), fromHeader.AccountID)
	require.Equal(t, "gpt-6-sol", fromHeader.Model)
	require.Equal(t, state, fromHeader.State)

	fromJSON, err := ParseHarvest(nil, []byte(`{"turn_state":"`+state+`","session_id":"sess-1","transport":"sse"}`), 8, "gpt-5.6-sol", now, time.Hour)
	require.NoError(t, err)
	require.Equal(t, "sess-1", fromJSON.HarvestSessionID)
	require.Equal(t, "sse", fromJSON.Transport)
	_, err = ParseHarvest(nil, []byte(`{"ok":true}`), 8, "gpt-5.6-sol", now, time.Hour)
	require.Error(t, err)
}
