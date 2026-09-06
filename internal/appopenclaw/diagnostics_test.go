package appopenclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
)

// captureLogs redirects the default slog logger into a buffer for the duration
// of the test. slog.SetDefault also rewires the stdlib log package, and
// restoring the previous handler does not undo that, so the log writer and
// flags are saved and restored explicitly.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	previous := slog.Default()
	previousWriter, previousFlags := log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})
	return buffer
}

// failingRunner emits the planted stderr payload and exits non-zero.
func failingRunner(stderr string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		// Shell is a test producer, not the runtime's command construction path.
		return exec.CommandContext(ctx, "sh", "-c", "printf '%s' \"$0\" >&2; exit 1", stderr)
	}
}

func TestReadLogsRedactedDiagnostics(t *testing.T) {
	logs := captureLogs(t)
	client := Client{Binary: "openclaw", Runner: failingRunner("unauthorized\ntoken=abc123secret\n")}

	var result map[string]any
	ctx := WithTarget(t.Context(), Target{Mode: "container", Container: "oc-runtime"})
	err := client.Read(ctx, "sessions.list", map[string]any{}, &result)

	if ErrorCode(err) != "permission_denied" {
		t.Fatalf("ErrorCode = %q, want permission_denied (err=%v)", ErrorCode(err), err)
	}
	if strings.Contains(err.Error(), "abc123secret") || strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("readError.Error() = %q, must stay code-only", err.Error())
	}

	logged := logs.String()
	for _, want := range []string{"collection failed", "sessions.list", "permission_denied", "oc-runtime", "[REDACTED]"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %q missing %q", logged, want)
		}
	}
	if strings.Contains(logged, "abc123secret") {
		t.Fatalf("log %q leaked the planted secret", logged)
	}
	if strings.Count(strings.TrimRight(logged, "\n"), "\n") != 0 || strings.Contains(logged, `\n`) {
		t.Fatalf("log %q keeps newlines; the diagnostic tail must be collapsed", logged)
	}
}

func TestWritePathLogsRedactedDiagnostics(t *testing.T) {
	logs := captureLogs(t)
	client := Client{Binary: "openclaw", Runner: failingRunner("unauthorized token=abc123secret")}

	var result map[string]any
	if err := client.AbortRun(t.Context(), "session", "run", &result); ErrorCode(err) != "permission_denied" {
		t.Fatalf("ErrorCode = %q, want permission_denied", ErrorCode(err))
	}

	logged := logs.String()
	for _, want := range []string{"chat.abort", "permission_denied", "[REDACTED]"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %q missing %q", logged, want)
		}
	}
	if strings.Contains(logged, "abc123secret") {
		t.Fatalf("log %q leaked the planted secret", logged)
	}
}

func TestSuccessfulReadDoesNotLog(t *testing.T) {
	logs := captureLogs(t)
	client := Client{Binary: "openclaw", Runner: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "%s", `{"count":1}`)
	}}
	var result map[string]any
	if err := client.Read(t.Context(), "sessions.list", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("successful read logged %q", logs.String())
	}
}

func TestWriteRPCWrapsMarshalError(t *testing.T) {
	client := Client{Runner: func(context.Context, string, ...string) *exec.Cmd {
		t.Error("unmarshalable params reached the runtime")
		return nil
	}}
	var result map[string]any
	err := client.writeRPC(t.Context(), "cron.update", map[string]any{"patch": make(chan int)}, &result)
	if err == nil {
		t.Fatal("writeRPC accepted unmarshalable params")
	}
	if want := "encode cron.update params: "; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("err = %q, want prefix %q", err.Error(), want)
	}
	if _, ok := errors.AsType[*json.UnsupportedTypeError](err); !ok {
		t.Fatalf("err = %v, want a wrapped json.UnsupportedTypeError", err)
	}
}

func TestReadAgentModelsRejectsUnsafeAgents(t *testing.T) {
	client := Client{Runner: func(context.Context, string, ...string) *exec.Cmd {
		t.Error("rejected agent id reached the runtime")
		return nil
	}}
	for _, tt := range []struct{ name, agent string }{
		{"path traversal", "../x"},
		{"flag injection", "--flag"},
		{"over length", strings.Repeat("a", 129)},
		{"empty", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]any
			if err := client.ReadAgentModels(t.Context(), tt.agent, &result); ErrorCode(err) != "invalid_request" {
				t.Fatalf("ErrorCode = %q, want invalid_request", ErrorCode(err))
			}
		})
	}
}

func TestReadCommandRejectsUnknownOperation(t *testing.T) {
	client := Client{Runner: func(context.Context, string, ...string) *exec.Cmd {
		t.Error("unknown operation reached the runtime")
		return nil
	}}
	var result map[string]any
	if err := client.ReadCommand(t.Context(), "shell", &result); ErrorCode(err) != "unsupported" {
		t.Fatalf("ErrorCode = %q, want unsupported", ErrorCode(err))
	}
}

func TestOutputClassifiesStderrDiagnostics(t *testing.T) {
	// Shell is a test producer, not the runtime's command construction path.
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "printf 'unauthorized: token=abc123secret' >&2; exit 1")
	out, err := Output(cmd, MaxOutputBytes)
	if len(out) != 0 {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if ErrorCode(err) != "permission_denied" {
		t.Fatalf("ErrorCode = %q, want permission_denied (err=%v)", ErrorCode(err), err)
	}
	if strings.Contains(err.Error(), "abc123secret") {
		t.Fatalf("err = %q leaked stderr", err.Error())
	}
}
