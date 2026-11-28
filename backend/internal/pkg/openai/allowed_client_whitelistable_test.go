package openai

import "testing"

func TestAllowedClientEntry_IsWhitelistable(t *testing.T) {
	cases := []struct {
		name  string
		entry AllowedClientEntry
		want  bool
	}{
		{name: "完整条目可白名单", entry: AllowedClientEntry{Originator: "opencode", UAContains: []string{"opencode/"}}, want: true},
		{name: "多个有效 marker 可白名单", entry: AllowedClientEntry{Originator: "x", UAContains: []string{"a/", "b/"}}, want: true},
		{name: "缺 originator → 不可(静默失效)", entry: AllowedClientEntry{UAContains: []string{"opencode/"}}, want: false},
		{name: "originator 全空白 → 不可", entry: AllowedClientEntry{Originator: "   ", UAContains: []string{"opencode/"}}, want: false},
		{name: "缺 ua_contains → 不可(静默失效)", entry: AllowedClientEntry{Originator: "opencode"}, want: false},
		{name: "ua_contains 全空白 → 不可", entry: AllowedClientEntry{Originator: "opencode", UAContains: []string{"", "  "}}, want: false},
		{name: "含一个空白 marker → 不可(空白会让整条 IsAllowedClientMatch 永不命中)", entry: AllowedClientEntry{Originator: "opencode", UAContains: []string{"opencode/", ""}}, want: false},
		{name: "UA-only with one prefix", entry: AllowedClientEntry{MatchUAOnly: true, UAContains: []string{"WorkBuddy/"}}, want: true},
		{name: "UA-only with no prefix", entry: AllowedClientEntry{MatchUAOnly: true}, want: false},
		{name: "UA-only with blank prefix", entry: AllowedClientEntry{MatchUAOnly: true, UAContains: []string{" "}}, want: false},
		{name: "UA-only with multiple prefixes", entry: AllowedClientEntry{MatchUAOnly: true, UAContains: []string{"WorkBuddy/", "opencode/"}}, want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entry.IsWhitelistable(); got != tt.want {
				t.Fatalf("IsWhitelistable(%+v) = %v, want %v", tt.entry, got, tt.want)
			}
		})
	}
}

func TestAllowedClientMatchUAOnly(t *testing.T) {
	entry := AllowedClientEntry{Originator: "WorkBuddy", UAContains: []string{"WorkBuddy/"}, MatchUAOnly: true}
	for _, tc := range []struct {
		ua, originator string
		want           bool
	}{
		{ua: "WorkBuddy/5.11.3 WorkBuddy/5.11.3 CLI/2.176.0", want: true},
		{ua: "  workbuddy/5.11.3", originator: "other", want: true},
		{ua: "Mozilla/5.0 WorkBuddy/5.11.3"},
		{ua: "OtherWorkBuddy/5.11.3"},
		{ua: "WorkBuddyPro/5.11.3"},
		{ua: "API-Comparison/1.0", originator: "WorkBuddy"},
		{originator: "WorkBuddy"},
	} {
		if got := IsAllowedClientMatch(tc.ua, tc.originator, entry); got != tc.want {
			t.Errorf("IsAllowedClientMatch(%q, %q) = %v, want %v", tc.ua, tc.originator, got, tc.want)
		}
	}
	entry.MatchUAOnly = false
	if IsAllowedClientMatch("WorkBuddy/5.11.3", "", entry) {
		t.Fatal("default mode matched without originator")
	}
	if !IsAllowedClientMatch("client/1.0 WorkBuddy/5.11.3", "workbuddy", entry) {
		t.Fatal("default mode must still support UA contains with matching originator")
	}
	entry.MatchUAOnly = true
	if !IsDeniedClientMatch("API-Comparison/1.0", "WorkBuddy", entry) {
		t.Fatal("blacklist must ignore MatchUAOnly and still match originator independently")
	}
}
