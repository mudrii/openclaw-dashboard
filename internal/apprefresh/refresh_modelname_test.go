package apprefresh

import "testing"

// ModelName must classify OpenAI reasoning families (o1/o3) before falling
// through to the GPT family checks. Otherwise inputs like "openai/o1-preview"
// match the broader "gpt-*" rules due to substring overlap (no boundary).
//
// The synthetic "gpt-fo1bar" case documents the segment-aware boundary check:
// "o1" inside an unrelated identifier MUST NOT trigger the O1 rule. It still
// resolves as a GPT variant via the existing GPT-4 fallthrough.
func TestModelName_O1O3OrderingAndBoundary(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"openai/o1-preview", "O1"},
		{"openai/o3-mini", "O3"},
		{"openai/gpt-5", "GPT-5"},          // regression: must still hit GPT-5
		{"openai/gpt-5.3-codex", "GPT-5.3 Codex"}, // regression: codex variant
		{"openai/gpt-4o", "GPT-4o"},        // regression: gpt-4o still wins
		// Boundary check: "o1" embedded inside an unrelated token must not
		// trip the O1 rule. Returns the raw id since no rule matches.
		{"openai/gpt-fo1bar", "openai/gpt-fo1bar"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := ModelName(tc.in); got != tc.want {
				t.Errorf("ModelName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
