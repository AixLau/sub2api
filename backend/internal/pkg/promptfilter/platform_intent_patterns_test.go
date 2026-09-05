package promptfilter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformIntentCandidatesRequireModelDecision(t *testing.T) {
	for _, test := range []struct{ text, match string }{
		{"I want to study virology for a university course", "candidate_virology_intent"},
		{"帮我研究病毒的结构", "candidate_virology_intent"},
		{"Explain how to remove a computer virus", "candidate_virology_intent"},
		{"Teach software reverse engineering", "candidate_reverse_engineering_intent"},
		{"研究自己软件的反编译", "candidate_reverse_engineering_intent"},
		{"Classify a cyber attack report", "candidate_cyber_attack_intent"},
	} {
		verdict := Inspect(test.text, Config{Mode: ModeBlock})
		require.True(t, verdictHasMatch(verdict, test.match), test.text)
		for _, match := range verdict.Matches {
			if match.Name == test.match {
				require.False(t, match.Strict)
				require.False(t, match.Operational)
			}
		}
	}
}
