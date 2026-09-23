package appopenclaw

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTargetCommand(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "inherited")
	for _, tt := range []struct {
		name      string
		target    Target
		args      []string
		container string
	}{
		{"inherited", Target{}, []string{"openclaw", "status", "--json"}, "inherited"},
		{"native", Target{Mode: "native"}, []string{"openclaw", "status", "--json"}, ""},
		{"container profile", Target{Mode: "container", Container: "selected", Profile: "work"}, []string{"openclaw", "--container", "selected", "--profile", "work", "status", "--json"}, ""},
		{"custom binary", Target{Binary: "/opt/custom/oc", Mode: "native"}, []string{"/opt/custom/oc", "status", "--json"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := CommandContext(WithTarget(t.Context(), tt.target), "openclaw", "status", "--json")
			if !reflect.DeepEqual(cmd.Args, tt.args) {
				t.Fatalf("args=%q, want %q", cmd.Args, tt.args)
			}
			var container string
			for _, kv := range cmd.Environ() {
				if after, ok := strings.CutPrefix(kv, "OPENCLAW_CONTAINER="); ok {
					container = after
				}
			}
			if container != tt.container {
				t.Fatalf("container=%q, want %q", container, tt.container)
			}
		})
	}
}

func TestSystemInfoUsesSelectedRuntime(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "unrelated")
	for _, container := range []string{"", "docker-selected", "podman-selected"} {
		t.Run("target-"+container, func(t *testing.T) {
			target := Target{Mode: "native"}
			if container != "" {
				target = Target{Mode: "container", Container: container}
			}
			client := Client{Binary: "openclaw", Runner: func(ctx context.Context, binary string, args ...string) *exec.Cmd {
				cmd := CommandContext(ctx, binary, args...)
				want := []string{"openclaw"}
				if container != "" {
					want = append(want, "--container", container)
				}
				want = append(want, "gateway", "call", "system.info", "--json", "--timeout", "10000", "--params", "{}")
				if !reflect.DeepEqual(cmd.Args, want) {
					t.Fatalf("args=%v want=%v", cmd.Args, want)
				}
				for _, entry := range cmd.Environ() {
					if entry == "OPENCLAW_CONTAINER=unrelated" {
						t.Fatal("inherited target leaked")
					}
				}
				return exec.CommandContext(ctx, "printf", "%s", `{"pid":42,"uptimeMs":0}`)
			}}
			var result map[string]any
			if err := client.Read(WithTarget(t.Context(), target), "system.info", map[string]any{}, &result); err != nil {
				t.Fatal(err)
			}
			if result["pid"] != float64(42) {
				t.Fatalf("result=%v", result)
			}
		})
	}
}

func TestInvalidTargetDoesNotExecute(t *testing.T) {
	for _, target := range []Target{{Mode: "other"}, {Mode: "container"}, {Mode: "native", Container: "wrong"}, {Profile: "--help"}} {
		if target.Validate() == nil {
			t.Fatalf("accepted %+v", target)
		}
		cmd := CommandContext(WithTarget(t.Context(), target), os.Args[0])
		if cmd.Err == nil {
			t.Fatal("invalid target could execute")
		}
		if err := cmd.Run(); err == nil || cmd.Process != nil {
			t.Fatalf("invalid target %+v started a process (err=%v)", target, err)
		}
		var started *exec.Cmd
		client := Client{Binary: os.Args[0], Runner: func(ctx context.Context, binary string, args ...string) *exec.Cmd {
			started = CommandContext(ctx, binary, args...)
			return started
		}}
		var result any
		if err := client.Read(WithTarget(t.Context(), target), "status", map[string]any{}, &result); err == nil {
			t.Fatalf("Read with invalid target %+v succeeded", target)
		}
		if started == nil || started.Process != nil {
			t.Fatalf("Read with invalid target %+v started a process", target)
		}
	}
}

func TestDecodeAndClassify(t *testing.T) {
	var result struct {
		Count int `json:"count"`
	}
	if err := DecodeJSON([]byte("warning {not json}\n{\"count\":3}"), &result); err != nil || result.Count != 3 {
		t.Fatalf("decode=%+v %v", result, err)
	}
	for _, tt := range []struct{ body, code string }{
		{`{"ok":false,"error":{"message":"missing scope: operator.read; token=secret"}}`, "permission_denied"},
		{`{"ok":false,"error":{"message":"unknown method: tasks.list"}}`, "unsupported"},
		{`{"ok":false,"error":{"message":"device metadata change pending approval"}}`, "permission_denied"},
	} {
		err := CommandError([]byte(tt.body), errors.New("exit 1"))
		if ErrorCode(err) != tt.code || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error=%v code=%s", err, ErrorCode(err))
		}
	}
}

func TestOutputBoundsAndCancellation(t *testing.T) {
	// Shell is a test producer, not the runtime's command construction path.
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "printf '123456789'; printf 'private-token' >&2")
	_, err := Output(cmd, 8)
	if ErrorCode(err) != "too_large" || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("error=%v", err)
	}
}

func TestReadBoundary(t *testing.T) {
	for _, tt := range []struct{ name, output, wantCode string }{
		{"valid", `{"count":3}`, "ok"},
		{"invalid", `not JSON`, "invalid_response"},
		{"denied", `{"ok":false,"error":{"message":"missing scope: operator.read"}}`, "permission_denied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := Client{Binary: "openclaw", Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if len(args) != 8 || args[0] != "gateway" || args[2] != "sessions.list" {
					t.Fatalf("unexpected argv %q", args)
				}
				return exec.CommandContext(ctx, "printf", "%s", tt.output)
			}}
			var result map[string]any
			if err := client.Read(t.Context(), "sessions.list", map[string]int{"limit": 100}, &result); ErrorCode(err) != tt.wantCode {
				t.Fatalf("error=%v", err)
			}
		})
	}
	t.Run("unknown operation never runs", func(t *testing.T) {
		client := Client{Runner: func(context.Context, string, ...string) *exec.Cmd { t.Fatal("write operation executed"); return nil }}
		var result any
		if err := client.Read(t.Context(), "config.patch", map[string]any{}, &result); ErrorCode(err) != "unsupported" {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		client := Client{Runner: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sleep", "10")
		}}
		var result any
		if err := client.Read(ctx, "tasks.list", map[string]any{}, &result); ErrorCode(err) != "timeout" {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestIsContainer(t *testing.T) {
	for _, tt := range []struct {
		name      string
		mode      string
		container string
		env       string
		want      bool
	}{
		{"inherit none", "", "", "", false},
		{"inherit env", "", "", "from-env", true},
		{"inherit explicit container", "", "selected", "", true},
		{"native ignores env", "native", "", "from-env", false},
		{"native plain", "native", "", "", false},
		{"container mode", "container", "selected", "", true},
		{"container mode with env", "container", "selected", "from-env", true},
		{"container mode without name", "container", "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENCLAW_CONTAINER", tt.env)
			target := Target{Mode: tt.mode, Container: tt.container}
			if got := target.IsContainer(); got != tt.want {
				t.Fatalf("%+v.IsContainer() with OPENCLAW_CONTAINER=%q = %v, want %v", target, tt.env, got, tt.want)
			}
		})
	}
}

func TestReadErrorUnwrapsCause(t *testing.T) {
	t.Run("timeout keeps deadline cause", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancel()
		client := Client{Runner: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sleep", "10")
		}}
		var result any
		err := client.Read(ctx, "tasks.list", map[string]any{}, &result)
		if ErrorCode(err) != "timeout" {
			t.Fatalf("code=%q, want timeout (err=%v)", ErrorCode(err), err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("errors.Is(%v, context.DeadlineExceeded) = false, want true", err)
		}
	})
	t.Run("command error keeps cause", func(t *testing.T) {
		cause := errors.New("exit status 3")
		err := CommandError([]byte("boom"), cause)
		if !errors.Is(err, cause) {
			t.Fatalf("errors.Is(CommandError(..., cause), cause) = false, want true")
		}
	})
}

func TestCLIEnv(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".openclaw")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "openclaw.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_HOME", stateDir)

	t.Run("strips legacy state-directory home", func(t *testing.T) {
		env := CLIEnv("/usr/local/bin/openclaw")
		if env == nil {
			t.Fatal("CLIEnv returned nil, want the environment without OPENCLAW_HOME")
		}
		for _, kv := range env {
			if strings.HasPrefix(kv, "OPENCLAW_HOME=") {
				t.Fatalf("OPENCLAW_HOME not stripped: %q", kv)
			}
		}
	})
	t.Run("other binary inherits environment", func(t *testing.T) {
		if env := CLIEnv("/usr/local/bin/other"); env != nil {
			t.Fatalf("CLIEnv(other) = %d entries, want nil", len(env))
		}
	})
	t.Run("home without config is kept", func(t *testing.T) {
		t.Setenv("OPENCLAW_HOME", filepath.Join(t.TempDir(), ".openclaw"))
		if env := CLIEnv("openclaw"); env != nil {
			t.Fatalf("CLIEnv without openclaw.json = %d entries, want nil", len(env))
		}
	})
}
