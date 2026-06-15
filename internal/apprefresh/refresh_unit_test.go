package apprefresh

import "testing"

func TestTitleCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"lowercase first byte upcased", "hello", "Hello"},
		{"already uppercase unchanged", "Hello", "Hello"},
		{"single lowercase letter", "x", "X"},
		// Only the first ASCII byte is touched; the remainder is copied verbatim.
		{"rest untouched", "openAI", "OpenAI"},
		// A leading digit is outside the a–z range, so the string is unchanged.
		{"leading digit unchanged", "1abc", "1abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := TitleCase(tc.in); got != tc.want {
				t.Errorf("TitleCase(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestModelName_NonOpenAIArms covers the non-OpenAI branches of ModelName that
// the existing refresh_modelname_test.go (focused on o1/o3) does not exercise.
func TestModelName_NonOpenAIArms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Anthropic: provider prefix stripped; opus-4-6 takes precedence over
		// the broader "opus" arm.
		{"opus 4.6 precedence", "anthropic/claude-opus-4-6", "Claude Opus 4.6"},
		{"opus default", "anthropic/claude-opus-4-5", "Claude Opus 4.5"},
		{"bare opus", "opus", "Claude Opus 4.5"},
		{"sonnet", "anthropic/claude-sonnet-4-5", "Claude Sonnet"},
		{"haiku", "anthropic/claude-3-5-haiku", "Claude Haiku"},

		// Grok: grok-4-fast must win over the broader grok-4 arm.
		{"grok 4 fast", "xai/grok-4-fast", "Grok 4 Fast"},
		{"grok 4", "xai/grok-4", "Grok 4"},
		{"grok4 alt spelling", "grok4", "Grok 4"},

		// Gemini: ordering matters — pro and specific flash variants before the
		// catch-all "gemini"/"flash".
		{"gemini 2.5 pro", "google/gemini-2.5-pro", "Gemini 2.5 Pro"},
		{"gemini pro alias", "gemini-pro", "Gemini 2.5 Pro"},
		{"gemini 3 flash", "gemini-3-flash", "Gemini 3 Flash"},
		{"gemini 2.5 flash", "gemini-2.5-flash", "Gemini 2.5 Flash"},
		{"gemini catch-all", "gemini-1.0", "Gemini Flash"},
		{"bare flash", "flash", "Gemini Flash"},

		// MiniMax / GLM / Kimi.
		{"minimax m2.5", "minimax/minimax-m2.5", "MiniMax M2.5"},
		{"minimax m2", "minimax/minimax-m2", "MiniMax"},
		{"bare minimax", "minimax", "MiniMax"},
		{"glm-5", "glm-5", "GLM-5"},
		{"glm-4.6 preserves minor version", "zhipu/glm-4.6", "GLM-4.6"},
		{"kimi k2p5", "kimi/k2p5", "Kimi K2.5"},
		{"kimi alias", "moonshot/kimi-latest", "Kimi K2.5"},

		// GPT family: codex specialization before the generic gpt-5/4o/4 arms.
		{"gpt-5.3 codex", "openai/gpt-5.3-codex", "GPT-5.3 Codex"},
		{"gpt-5", "openai/gpt-5", "GPT-5"},
		{"gpt-4o", "openai/gpt-4o", "GPT-4o"},
		{"gpt-4", "openai/gpt-4", "GPT-4"},

		// Unknown id falls through to the raw (unchanged) value.
		{"unknown raw", "some-unknown-model", "some-unknown-model"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModelName(tc.in); got != tc.want {
				t.Errorf("ModelName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
