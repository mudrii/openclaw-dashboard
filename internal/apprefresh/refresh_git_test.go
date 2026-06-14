package apprefresh

import (
	"context"
	"os/exec"
	"testing"
)

// TestCollectGitLog_Parsing drives every parse branch of collectGitLog via the
// execCommandContext seam (no real git shell-out). The stub returns canned
// stdout / a failing command so the test is deterministic.
func TestCollectGitLog_Parsing(t *testing.T) {
	prevExec := execCommandContext
	t.Cleanup(func() { execCommandContext = prevExec })

	// fail produces a command that exits non-zero so Output() returns an error.
	fail := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	emit := func(stdout string) func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "printf '%s' \""+stdout+"\"")
		}
	}

	tests := []struct {
		name string
		stub func(ctx context.Context, name string, args ...string) *exec.Cmd
		want []map[string]any
	}{
		{
			name: "well-formed 3-field lines",
			stub: emit("abc123|fix things|2 hours ago\ndef456|add feature|3 days ago"),
			want: []map[string]any{
				{"hash": "abc123", "message": "fix things", "ago": "2 hours ago"},
				{"hash": "def456", "message": "add feature", "ago": "3 days ago"},
			},
		},
		{
			name: "2-field line yields empty ago",
			stub: emit("abc123|just a message"),
			want: []map[string]any{
				{"hash": "abc123", "message": "just a message", "ago": ""},
			},
		},
		{
			name: "lines without pipe are skipped",
			stub: emit("no-pipe-here\nabc123|kept|now"),
			want: []map[string]any{
				{"hash": "abc123", "message": "kept", "ago": "now"},
			},
		},
		{
			name: "command error yields nil",
			stub: fail,
			want: nil,
		},
		{
			name: "empty stdout yields nil",
			stub: emit(""),
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			execCommandContext = tc.stub
			got := collectGitLog(context.Background(), "/fake/repo")
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range tc.want {
				for k, want := range tc.want[i] {
					if got[i][k] != want {
						t.Errorf("entry %d key %q: got %v, want %v", i, k, got[i][k], want)
					}
				}
			}
		})
	}
}
