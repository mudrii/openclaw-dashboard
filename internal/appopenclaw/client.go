// Package appopenclaw owns bounded access to the selected OpenClaw runtime.
package appopenclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MaxOutputBytes caps the stdout a single runtime call may produce before it is
// abandoned as too_large.
const MaxOutputBytes = 8 << 20

// Target selects which OpenClaw runtime a call reaches: the CLI binary, native
// or container mode, the container name, and the CLI profile.
type Target struct {
	// Binary overrides the configured CLI executable path.
	Binary string `json:"binary,omitempty"`
	// Mode is "native", "container", or empty to inherit from the environment.
	Mode string `json:"mode,omitempty"`
	// Container names the container the CLI proxies into.
	Container string `json:"container,omitempty"`
	// Profile selects a named CLI profile and its state directory.
	Profile string `json:"profile,omitempty"`
}

var targetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Validate reports whether the target is internally consistent and safe to turn
// into CLI arguments. Names are restricted so they cannot be read as flags.
func (t Target) Validate() error {
	if t.Mode != "" && t.Mode != "native" && t.Mode != "container" {
		return errors.New("openclaw.mode must be native or container")
	}
	if t.Mode == "container" && t.Container == "" {
		return errors.New("openclaw.container is required in container mode")
	}
	if t.Mode == "native" && t.Container != "" {
		return errors.New("openclaw.container conflicts with native mode")
	}
	if t.Container != "" && !targetName.MatchString(t.Container) {
		return errors.New("invalid openclaw.container")
	}
	if t.Profile != "" && !targetName.MatchString(t.Profile) {
		return errors.New("invalid openclaw.profile")
	}
	return nil
}

type targetKey struct{}

// WithTarget returns a context carrying the runtime selection used by
// CommandContext and by the diagnostics logger.
func WithTarget(ctx context.Context, target Target) context.Context {
	return context.WithValue(ctx, targetKey{}, target)
}

// TargetFromContext returns the target carried by ctx, or the zero Target when
// ctx is nil or carries none.
func TargetFromContext(ctx context.Context) Target {
	if ctx == nil {
		return Target{}
	}
	t, _ := ctx.Value(targetKey{}).(Target)
	return t
}

// Effective records inherited selection so caches and diagnostics identify it.
func (t Target) Effective() Target {
	if t.Mode == "" {
		if t.Container == "" {
			t.Container = os.Getenv("OPENCLAW_CONTAINER")
		}
		t.Mode = "native"
		if t.Container != "" {
			t.Mode = "container"
		}
	}
	return t
}

// IsContainer reports whether calls for this target run against a container
// runtime, including the inherited OPENCLAW_CONTAINER case.
func (t Target) IsContainer() bool {
	return t.Mode == "container" || t.Container != "" || (t.Mode == "" && os.Getenv("OPENCLAW_CONTAINER") != "")
}

// StatePath keeps local config/file views on the selected profile. Explicit
// OPENCLAW_STATE_DIR follows the CLI's precedence over profile defaults.
func (t Target) StatePath(fallback string) string {
	if path := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR")); path != "" {
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				return filepath.Join(home, path[2:])
			}
		}
		return filepath.Clean(path)
	}
	if t.Profile != "" {
		return filepath.Join(filepath.Dir(fallback), ".openclaw-"+t.Profile)
	}
	return fallback
}

// CommandContext builds the CLI command for the target carried by ctx, adding
// the --container and --profile prefix arguments. An invalid target yields a
// command whose Err is set, so it can never execute.
func CommandContext(ctx context.Context, binary string, args ...string) *exec.Cmd {
	target := TargetFromContext(ctx)
	if target.Binary != "" {
		binary = target.Binary
	}
	if binary == "" {
		binary = "openclaw"
	}
	prefix := make([]string, 0, 4+len(args))
	if target.Container != "" {
		prefix = append(prefix, "--container", target.Container)
	}
	if target.Profile != "" {
		prefix = append(prefix, "--profile", target.Profile)
	}
	// #nosec G702 -- executable is trusted local configuration; argv is passed without a shell and public callers allowlist operations.
	cmd := exec.CommandContext(ctx, binary, append(prefix, args...)...)
	cmd.Env = CLIEnv(binary)
	cmd.WaitDelay = time.Second
	if err := target.Validate(); err != nil {
		cmd.Err = err
	}
	if target.Mode != "" || target.Container != "" {
		cmd.Env = withoutEnv(cmd.Environ(), "OPENCLAW_CONTAINER")
	}
	return cmd
}

func withoutEnv(env []string, name string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

type readError struct {
	code string
	// detail is the redacted, log-safe tail of the command output. It is only
	// ever read by logCollectionFailure; Error never exposes it.
	detail string
	cause  error
}

func (e *readError) Error() string { return "OpenClaw collection: " + e.code }
func (e *readError) Unwrap() error { return e.cause }

// ErrorCode maps err to the stable code the dashboard payloads report: "ok",
// "timeout", "unavailable", or the classification CommandError assigned.
func ErrorCode(err error) string {
	if err == nil {
		return "ok"
	}
	var e *readError
	if errors.As(err, &e) {
		return e.code
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return "unavailable"
	}
	return "unavailable"
}

// CommandError classifies raw command output into an error code. The output is
// never retained verbatim: only a redacted tail is kept, for the server-side log
// line, and the returned error's message stays code-only.
func CommandError(output []byte, cause error) error {
	text := strings.ToLower(string(output))
	code := ErrorCode(cause)
	switch {
	case strings.Contains(text, "missing scope"), strings.Contains(text, "pending approval"), strings.Contains(text, "unauthorized"), strings.Contains(text, "pairing required"):
		code = "permission_denied"
	case strings.Contains(text, "unknown method"), strings.Contains(text, "unknown command"), strings.Contains(text, "method not found"):
		code = "unsupported"
	case strings.Contains(text, "database is locked"), strings.Contains(text, "did not stabilize"):
		code = "storage_busy"
	}
	return &readError{code: code, detail: redactedTail(output), cause: cause}
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	stop     func()
}

func (b *cappedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	_, _ = b.buffer.Write(p[:min(n, remaining)])
	if n > remaining {
		b.exceeded = true
		if b.stop != nil {
			b.stop()
		}
	}
	return n, nil
}

// Output bounds both streams; exceeding stdout terminates the producer.
func Output(cmd *exec.Cmd, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, &readError{code: "too_large"}
	}
	stdout := &cappedBuffer{limit: maxBytes, stop: func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}}
	stderr := &cappedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if stdout.exceeded {
		return nil, &readError{code: "too_large"}
	}
	if err != nil {
		return stdout.Bytes(), CommandError(append(bytes.Clone(stdout.Bytes()), stderr.Bytes()...), err)
	}
	return stdout.Bytes(), nil
}

// DecodeJSON unmarshals data into value, skipping any leading CLI chatter by
// retrying from each following "{" or "[". It reports invalid_response when no
// prefix decodes.
func DecodeJSON(data []byte, value any) error {
	data = bytes.TrimSpace(data)
	for attempts := 0; len(data) > 0 && attempts < 16; attempts++ {
		if err := json.Unmarshal(data, value); err == nil {
			return nil
		}
		i := bytes.IndexAny(data[1:], "{[")
		if i < 0 {
			break
		}
		data = data[i+1:]
	}
	return &readError{code: "invalid_response"}
}

// Client runs the allowlisted OpenClaw CLI operations the dashboard collects.
// The zero value is usable and falls back to the "openclaw" binary.
type Client struct {
	// Binary is the CLI executable; empty means "openclaw".
	Binary string
	// Runner builds the command to execute; empty means CommandContext. Tests
	// substitute it to keep the runtime out of the loop.
	Runner func(context.Context, string, ...string) *exec.Cmd
}

var readMethods = map[string]bool{
	"sessions.list": true, "sessions.usage": true, "usage.cost": true,
	"tasks.list": true, "tasks.get": true, "cron.list": true, "cron.get": true, "cron.runs": true,
	"channels.status": true, "logs.tail": true, "status": true, "config.get": true,
	"doctor.memory.status": true, "skills.status": true, "system.info": true,
	"progressCard.get": true, "board.get": true,
	"workboard.boards.list": true, "workboard.cards.list": true, "workboard.cards.stats": true, "workboard.cards.runs": true,
}

// ReadCommand is intentionally limited to inventories without a core RPC.
// It is not a browser-supplied command runner.
func (c Client) ReadCommand(ctx context.Context, operation string, value any) error {
	commands := map[string][]string{
		"plugins":     {"plugins", "list", "--json"},
		"memoryIndex": {"memory", "status", "--json"},
		"diagnostics": {"status", "--json"},
	}
	args, ok := commands[operation]
	if !ok {
		return &readError{code: "unsupported"}
	}
	return c.runJSON(ctx, operation, args, value)
}

// ReadAgentModels decodes the model status of a single agent. The agent id must
// be a bare name of at most 128 characters, so it can never be read as a flag or
// a path.
func (c Client) ReadAgentModels(ctx context.Context, agent string, value any) error {
	if len(agent) > 128 || !targetName.MatchString(agent) {
		return &readError{code: "invalid_request"}
	}
	return c.runJSON(ctx, "models.status", []string{"models", "status", "--agent", agent, "--json"}, value)
}

// runJSON executes one bounded CLI call and decodes its JSON into value. method
// is the operator-facing label used for diagnostics only; every failure is
// logged once, here, with a redacted output tail.
func (c Client) runJSON(ctx context.Context, method string, args []string, value any) (err error) {
	defer func() {
		if err != nil {
			logCollectionFailure(ctx, method, err)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	runner := c.Runner
	if runner == nil {
		runner = CommandContext
	}
	out, err := Output(runner(ctx, c.Binary, args...), MaxOutputBytes)
	if ctx.Err() != nil {
		return &readError{code: "timeout", cause: ctx.Err()}
	}
	if err != nil {
		return err
	}
	var envelope struct {
		OK *bool `json:"ok"`
	}
	if DecodeJSON(out, &envelope) == nil && envelope.OK != nil && !*envelope.OK {
		return CommandError(out, errors.New("command failed"))
	}
	return DecodeJSON(out, value)
}

// Read calls an allowlisted gateway read method and decodes the response into
// value. Unknown methods are refused without executing anything.
func (c Client) Read(ctx context.Context, method string, params any, value any) error {
	if !readMethods[method] {
		return &readError{code: "unsupported"}
	}
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode OpenClaw parameters: %w", err)
	}
	return c.runJSON(ctx, method, []string{"gateway", "call", method, "--json", "--timeout", "10000", "--params", string(data)}, value)
}
