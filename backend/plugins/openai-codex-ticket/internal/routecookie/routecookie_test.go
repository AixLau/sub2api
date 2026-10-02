package routecookie

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestParseHeadersKeepsOnlyCompleteRoutePair(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	exp := now.Add(30 * time.Minute).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp)))
	value := "eyJhbGciOiJub25lIn0." + payload + ".sig"
	headers := http.Header{
		"Set-Cookie": {
			"session=secret; Path=/",
			"__cflb=edge-a; Max-Age=1800; Path=/",
			"__oailb=" + value + "; Max-Age=5; Path=/",
		},
	}
	pair := ParseHeaders(headers, now)
	if !pair.Complete() || pair.Values[CFLB] != "edge-a" || pair.Values[OAILB] != value {
		t.Fatalf("unexpected pair: %#v", pair)
	}
	if !pair.ExpiresAt.Equal(time.Unix(exp, 0).UTC()) {
		t.Fatalf("expiry = %s, want JWT expiry %s", pair.ExpiresAt, time.Unix(exp, 0).UTC())
	}
}

func TestParseHeadersCanonicalizesLeadingUnderscoreVariants(t *testing.T) {
	pair := ParseHeaders(http.Header{"Set-Cookie": {"_cflb=cf", "___oailb=lb"}}, time.Unix(1_800_000_000, 0))
	if !pair.Complete() || pair.Values[CFLB] != "cf" || pair.Values[OAILB] != "lb" {
		t.Fatalf("unexpected canonical pair: %#v", pair)
	}
}

func TestPartialRouteCookieRemainsUsableForSteering(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	pair := ParseHeaders(http.Header{"Set-Cookie": {"__cflb=cf-only; Max-Age=300"}}, now)
	if !pair.HasValue() || pair.Complete() || !pair.Usable(now.Add(time.Minute), time.Hour) {
		t.Fatalf("partial route observation was not usable: %#v", pair)
	}
	if got := pair.Header(); got != "__cflb=cf-only" {
		t.Fatalf("partial header = %q", got)
	}
}

func TestRouteCookieExpiryUsesEarliestDeclaration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	expires := now.Add(time.Hour).UTC().Format(http.TimeFormat)
	pair := ParseHeaders(http.Header{"Set-Cookie": {"__oailb=opaque; Expires=" + expires + "; Max-Age=60"}}, now)
	want := now.Add(time.Minute)
	if !pair.ExpiresAt.Equal(want) {
		t.Fatalf("expiry = %s, want %s", pair.ExpiresAt, want)
	}
}

func TestMergeIntoPreservesNonRouteCookies(t *testing.T) {
	got := MergeInto("session=keep; __cflb=old; extra=value", map[string]string{
		CFLB:  "new",
		OAILB: "pair",
	})
	if got != "session=keep; __cflb=new; extra=value; __oailb=pair" {
		t.Fatalf("merged cookie = %q", got)
	}
}

func TestValidateSeedRejectsMissingPairAndExpiredJWT(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	if _, err := ValidateSeed("__cflb=a", "any", now); err == nil {
		t.Fatal("expected incomplete pair rejection")
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, now.Add(-time.Minute).Unix())))
	value := "eyJhbGciOiJub25lIn0." + payload + ".sig"
	if _, err := ValidateSeed("__cflb=a; __oailb="+value, "any", now); err == nil {
		t.Fatal("expected expired pair rejection")
	}
}

func TestParseCookieHeaderCombinesRoutePair(t *testing.T) {
	pair, err := ParseCookieHeader("session=private; _cflb=edge; ___oailb=lb", time.Unix(1_800_000_000, 0))
	if err != nil || !pair.Complete() || pair.Values[CFLB] != "edge" || pair.Values[OAILB] != "lb" {
		t.Fatalf("pair=%#v err=%v", pair, err)
	}
}

func TestGatewayReadsUnifiedLabelFromJWTPayload(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"gateway":"unified-88"}`, now.Add(time.Hour).Unix())))
	value := "eyJhbGciOiJub25lIn0." + payload + ".sig"
	pair, err := ValidateSeed("__cflb=edge; __oailb="+value, "unified-88", now)
	if err != nil {
		t.Fatalf("ValidateSeed: %v", err)
	}
	if pair.Gateway != "unified-88" {
		t.Fatalf("gateway = %q, want unified-88", pair.Gateway)
	}
}
