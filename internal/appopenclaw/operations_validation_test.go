package appopenclaw

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// refusingClient returns a Client whose Runner fails the test if any command is
// built, proving input validation happens before execution.
func refusingClient(t *testing.T) Client {
	t.Helper()
	return Client{Runner: func(context.Context, string, ...string) *exec.Cmd {
		t.Fatal("runner called for invalid input")
		return nil
	}}
}

func TestOperationInputValidationNeverExecutes(t *testing.T) {
	client := refusingClient(t)
	var result any
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"run empty id", func() error { return client.RunAutomation(t.Context(), "", &result) }},
		{"run slash id", func() error { return client.RunAutomation(t.Context(), "a/b", &result) }},
		{"run backslash id", func() error { return client.RunAutomation(t.Context(), `a\b`, &result) }},
		{"enable empty id", func() error { return client.SetAutomationEnabled(t.Context(), "", "rev", true, &result) }},
		{"enable slash id", func() error { return client.SetAutomationEnabled(t.Context(), "a/b", "rev", true, &result) }},
		{"enable backslash id", func() error { return client.SetAutomationEnabled(t.Context(), `a\b`, "rev", true, &result) }},
		{"enable empty revision", func() error { return client.SetAutomationEnabled(t.Context(), "job", "", true, &result) }},
		{"abort empty session", func() error { return client.AbortRun(t.Context(), "", "run", &result) }},
		{"abort empty run", func() error { return client.AbortRun(t.Context(), "session", "", &result) }},
		{"models flag agent", func() error { return client.ReadAgentModels(t.Context(), "--help", &result) }},
		{"models long agent", func() error { return client.ReadAgentModels(t.Context(), strings.Repeat("a", 129), &result) }},
		{"models empty agent", func() error { return client.ReadAgentModels(t.Context(), "", &result) }},
		{"models path agent", func() error { return client.ReadAgentModels(t.Context(), "../x", &result) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if code := ErrorCode(tt.call()); code != "invalid_request" {
				t.Fatalf("ErrorCode = %q, want invalid_request", code)
			}
		})
	}
}

func TestReadAgentModelsAcceptsMaxLengthName(t *testing.T) {
	name := strings.Repeat("a", 128)
	client := Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		want := []string{"models", "status", "--agent", name, "--json"}
		if strings.Join(args, " ") != strings.Join(want, " ") {
			t.Fatalf("args=%q, want %q", args, want)
		}
		return exec.CommandContext(ctx, "printf", "%s", `{}`)
	}}
	var result map[string]any
	if err := client.ReadAgentModels(t.Context(), name, &result); err != nil {
		t.Fatal(err)
	}
}
