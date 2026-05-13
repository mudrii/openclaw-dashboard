package apprefresh

import "testing"

// ModelName must classify OpenAI reasoning families (o1/o3) before falling
// through to the GPT family checks. Otherwise inputs like "openai/o1-preview"
// match the broader "gpt-*" rules due to substring overlap (no boundary).
//
// The boundary cases document the segment-anchored match: "o1"/"o3" must be a
// standalone token between separators (`/`, `-`, `:`, or string edges) — not
// an embedded substring of an unrelated identifier.
func TestModelName_O1O3OrderingAndBoundary(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// Canonical o1/o3 hits (provider prefix + variants).
		{"openai/o1-preview", "O1"},
		{"openai/o1", "O1"},
		{"o1-mini", "O1"},
		{"openai/o3-mini", "O3"},
		{"o3", "O3"},

		// GPT regressions: must keep hitting the right GPT bucket.
		{"openai/gpt-5", "GPT-5"},
		{"openai/gpt-5.3-codex", "GPT-5.3 Codex"},
		{"openai/gpt-4o", "GPT-4o"},

		// Segment-anchored boundary: "o1"/"o3" embedded inside an unrelated
		// token must NOT trip the O1/O3 rule. These have no other matching
		// rule, so ModelName returns the raw id.
		{"openai/foo1bar", "openai/foo1bar"},
		{"gpt-fo1xx", "gpt-fo1xx"},
		{"o1foo", "o1foo"},        // no trailing separator
		{"foo-o1bar", "foo-o1bar"}, // leading separator but no trailing one
		{"o3xyz", "o3xyz"},

		// Provider-prefixed GPT identifier with an embedded "o3" token: the
		// GPT-* rules sit *after* o1/o3, so a true "o3" segment wins. Here
		// "o3" is a real segment between dashes, so O3 is correct.
		{"openai/gpt-o3-test", "O3"},

		// Dot and underscore separators must also anchor segment boundaries.
		{"openai/o1.preview", "O1"},
		{"o3.mini", "O3"},
		{"o1_preview", "O1"},
		// Negative regression: "o1" embedded in a longer dotted token must not match.
		{"foo1.bar", "foo1.bar"},

		// Empty input: falls through, returns the raw (empty) id.
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := ModelName(tc.in); got != tc.want {
				t.Errorf("ModelName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
