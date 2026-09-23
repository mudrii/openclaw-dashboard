// Package appconfig handles configuration loading and defaults for the dashboard.
package appconfig

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// BotConfig is the bot identity shown in the dashboard header.
type BotConfig struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

// ThemeConfig selects the dashboard colour theme preset.
type ThemeConfig struct {
	Preset string `json:"preset"`
}

// RefreshConfig controls how often dashboard data is recollected.
type RefreshConfig struct {
	IntervalSeconds int `json:"intervalSeconds"`
}

// ServerConfig is the dashboard HTTP listen address.
type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

// ValidPort reports whether port is a usable TCP port number (1-65535).
func ValidPort(port int) bool {
	return port >= 1 && port <= 65535
}

// AIConfig configures the dashboard chat panel and its AI gateway. MaxHistory
// is clamped by Load to [minMaxHistory, maxMaxHistory].
type AIConfig struct {
	Enabled     bool   `json:"enabled"`
	GatewayPort int    `json:"gatewayPort"`
	Model       string `json:"model"`
	MaxHistory  int    `json:"maxHistory"`
	DotenvPath  string `json:"dotenvPath"`
}

// defaultDotenvPath is the fallback location for the dashboard's .env file
// when neither config.json nor the AI section overrides it.
const defaultDotenvPath = "~/.openclaw/.env"

// DefaultCPUTimeoutMs is the fallback bound for the CPU sampling command on
// darwin (`top -l 2 -s 1`) and the sampling window on linux (`/proc/stat`).
// It is the canonical default used by both the config layer (struct default
// and out-of-range clamp) and the platform collectors' defensive fallback.
const DefaultCPUTimeoutMs = 6000

// DefaultColdPathTimeoutMs is the fallback budget for the /api/system cold path
// (uncached version + gateway probes). Used as the struct default and the
// out-of-range clamp reset. Range [200, 30000] accommodates runtimes that wrap
// `openclaw status --json` in docker exec (~10s overhead, issue #31).
const DefaultColdPathTimeoutMs = 8000

// defaultMaxHistory, minMaxHistory and maxMaxHistory bound ai.maxHistory:
// non-positive values reset to the default, larger ones clamp to the maximum.
const (
	defaultMaxHistory = 6
	minMaxHistory     = 1
	maxMaxHistory     = 50
)

// LogsConfig configures the log tail and error feed. The snake_case fields are
// legacy aliases: Load applies each one only when its camelCase counterpart is
// absent from config.json (or set to an empty/non-positive value).
type LogsConfig struct {
	Enabled              bool     `json:"enabled"`
	TailLines            int      `json:"tailLines"`
	FastRefreshMs        int      `json:"fastRefreshMs"`
	ErrorWindowHours     int      `json:"errorWindowHours"`
	MaxErrorSignatures   int      `json:"maxErrorSignatures"`
	Sources              []string `json:"sources"`
	SystemdUnit          string   `json:"systemdUnit,omitempty"`
	LogSources           []string `json:"log_sources"`
	LogTailLines         int      `json:"log_tail_lines"`
	LogFastRefreshMs     int      `json:"log_fast_refresh_ms"`
	ErrorFeedWindowHours int      `json:"error_feed_window_hours"`
}

// AlertsConfig holds the thresholds that raise dashboard alerts.
type AlertsConfig struct {
	DailyCostHigh float64 `json:"dailyCostHigh"`
	DailyCostWarn float64 `json:"dailyCostWarn"`
	ContextPct    float64 `json:"contextPct"`
	MemoryMb      float64 `json:"memoryMb"`
}

// MetricThreshold is a warn/critical percentage pair for one system metric.
type MetricThreshold struct {
	Warn     float64 `json:"warn"`
	Critical float64 `json:"critical"`
}

// SystemConfig configures host metrics collection and version probes.
type SystemConfig struct {
	Enabled            bool `json:"enabled"`
	PollSeconds        int  `json:"pollSeconds"`
	MetricsTTLSeconds  int  `json:"metricsTtlSeconds"`
	VersionsTTLSeconds int  `json:"versionsTtlSeconds"`
	GatewayTimeoutMs   int  `json:"gatewayTimeoutMs"`
	// DeepStatus opts into `openclaw status --json --deep`, which adds the
	// event-loop and last-heartbeat blocks at the cost of a slower status call.
	// Default off (lean status: task queue + plugin-compat + channel summary).
	DeepStatus bool `json:"deepStatus,omitempty"`
	// ColdPathTimeoutMs bounds the worst-case wall time of a cold /api/system
	// collection (no warm cache). Each subcollector still has its own per-probe
	// timeout; this is the overall budget that prevents a slow gateway from
	// dragging the whole refresh past the frontend fetch deadline.
	ColdPathTimeoutMs int `json:"coldPathTimeoutMs"`
	// CPUTimeoutMs bounds the CPU sampling command on darwin (`top -l 2 -s 1`)
	// and the CPU sampling window on linux (`/proc/stat`). Defaults to 6000ms; raise when
	// the host is heavily loaded and `top` exceeds the default budget.
	CPUTimeoutMs    int             `json:"cpuTimeoutMs"`
	GatewayPort     int             `json:"gatewayPort"`
	DiskPath        string          `json:"diskPath"`
	WarnPercent     float64         `json:"warnPercent"`
	CriticalPercent float64         `json:"criticalPercent"`
	CPU             MetricThreshold `json:"cpu"`
	RAM             MetricThreshold `json:"ram"`
	Swap            MetricThreshold `json:"swap"`
	Disk            MetricThreshold `json:"disk"`
}

// Config is the complete dashboard configuration loaded from config.json.
type Config struct {
	Operations OperationsConfig   `json:"operations,omitzero"`
	Openclaw   appopenclaw.Target `json:"openclaw,omitzero"`
	Bot        BotConfig          `json:"bot"`
	Theme      ThemeConfig        `json:"theme"`
	Timezone   string             `json:"timezone"`
	Refresh    RefreshConfig      `json:"refresh"`
	Server     ServerConfig       `json:"server"`
	AI         AIConfig           `json:"ai"`
	Logs       LogsConfig         `json:"logs"`
	Alerts     AlertsConfig       `json:"alerts"`
	System     SystemConfig       `json:"system"`
}

// OperationsConfig gates runtime write operations. Operations are opt-in and
// also require a separate operator credential.
type OperationsConfig struct {
	Enabled bool `json:"enabled"`
}

// Default returns the built-in configuration used for every key config.json
// omits.
func Default() Config {
	return Config{
		Bot:      BotConfig{Name: "OpenClaw Dashboard", Emoji: "🦞"},
		Theme:    ThemeConfig{Preset: "midnight"},
		Timezone: "UTC",
		Refresh:  RefreshConfig{IntervalSeconds: 30},
		Server:   ServerConfig{Port: 8080, Host: "127.0.0.1"},
		AI: AIConfig{
			Enabled:     true,
			GatewayPort: 18789,
			Model:       "",
			MaxHistory:  defaultMaxHistory,
			DotenvPath:  defaultDotenvPath,
		},
		Logs: LogsConfig{
			Enabled:              true,
			TailLines:            200,
			FastRefreshMs:        3000,
			ErrorWindowHours:     24,
			MaxErrorSignatures:   1000,
			Sources:              []string{"logs/gateway.log", "logs/gateway.err.log"},
			LogSources:           nil,
			LogTailLines:         0,
			LogFastRefreshMs:     0,
			ErrorFeedWindowHours: 0,
		},
		Alerts: AlertsConfig{
			DailyCostHigh: 50,
			DailyCostWarn: 20,
			ContextPct:    80,
			MemoryMb:      640,
		},
		System: SystemConfig{
			Enabled:            true,
			PollSeconds:        10,
			MetricsTTLSeconds:  10,
			VersionsTTLSeconds: 300,
			GatewayTimeoutMs:   5000,
			ColdPathTimeoutMs:  DefaultColdPathTimeoutMs,
			CPUTimeoutMs:       DefaultCPUTimeoutMs,
			// GatewayPort intentionally left zero so Load() can inherit from
			// AI.GatewayPort when system.gatewayPort is omitted in config.json.
			// Pre-filling here would mask user overrides on the AI side.
			DiskPath:        "/",
			WarnPercent:     70,
			CriticalPercent: 85,
			CPU:             MetricThreshold{Warn: 80, Critical: 95},
			RAM:             MetricThreshold{Warn: 80, Critical: 95},
			Swap:            MetricThreshold{Warn: 80, Critical: 95},
			Disk:            MetricThreshold{Warn: 80, Critical: 95},
		},
	}
}

// warnUnknownConfigKeys decodes raw as a generic JSON tree and emits a
// slog warning for every top-level or nested key that does not appear in
// the Config struct schema. Best-effort: a malformed JSON payload is
// reported by the subsequent strict Unmarshal, so we silently skip here
// when the loose decode fails.
func warnUnknownConfigKeys(raw []byte) {
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return
	}
	walkUnknownKeys("", generic, reflect.TypeFor[Config]())
}

// walkUnknownKeys recurses through a decoded JSON tree, comparing each
// key against the JSON tags of structType. Nested objects descend into
// the corresponding field type; unrecognised keys produce a warning
// containing the dotted path (e.g. "ai.gatewayPot").
func walkUnknownKeys(prefix string, m map[string]any, structType reflect.Type) {
	for structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return
	}
	known := jsonFields(structType)
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		ft, ok := known[k]
		if !ok {
			slog.Warn("[dashboard] unknown config key", "field", path)
			continue
		}
		if child, ok := v.(map[string]any); ok {
			walkUnknownKeys(path, child, ft)
		}
	}
}

// jsonFields builds a map from JSON tag name to the field's Go type
// for every exported field in t. Used to validate config.json keys.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, t.NumField())
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		if name == "-" {
			continue
		}
		out[name] = f.Type
	}
	return out
}

// Load reads <dir>/config.json over Default() and clamps every field to its
// valid range. When <dir>/config.json does not exist it falls back to
// <dir>/assets/runtime/config.json; any other open or read error, and a missing
// fallback, log a warning and yield defaults.
func Load(dir string) Config {
	cfg := Default()
	path := filepath.Join(dir, "config.json")
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		path = filepath.Join(dir, "assets", "runtime", "config.json")
		f, err = os.Open(path)
	}
	var raw []byte
	switch {
	case errors.Is(err, fs.ErrNotExist):
		slog.Warn("[dashboard] config: no config.json found, using defaults")
	case err != nil:
		slog.Warn("[dashboard] config: cannot open config.json, using defaults", "path", path, "error", err)
	default:
		defer func() { _ = f.Close() }()
		var readErr error
		raw, readErr = io.ReadAll(f)
		if readErr != nil {
			raw = nil
			slog.Warn("[dashboard] config: read error, using defaults", "error", readErr)
		} else {
			warnUnknownConfigKeys(raw)
			if err := json.Unmarshal(raw, &cfg); err != nil {
				slog.Warn("[dashboard] invalid config.json, using defaults for missing/invalid fields", "error", err)
			}
		}
	}
	applyLegacyLogAliases(&cfg.Logs, raw)
	if len(cfg.Logs.Sources) == 0 {
		// An empty/omitted sources list falls back to the built-in defaults so
		// the log feed is never silently left with zero sources, consistent
		// with how every other Logs field clamps to a sane default.
		cfg.Logs.Sources = append([]string{}, Default().Logs.Sources...)
	}
	if cfg.AI.MaxHistory < minMaxHistory {
		cfg.AI.MaxHistory = defaultMaxHistory
	}
	cfg.AI.MaxHistory = min(cfg.AI.MaxHistory, maxMaxHistory)
	if !ValidPort(cfg.AI.GatewayPort) {
		cfg.AI.GatewayPort = 18789
	}
	if cfg.AI.DotenvPath == "" {
		cfg.AI.DotenvPath = defaultDotenvPath
	}
	if cfg.Refresh.IntervalSeconds <= 0 {
		cfg.Refresh.IntervalSeconds = 30
	}
	if !ValidPort(cfg.Server.Port) {
		cfg.Server.Port = 8080
	}
	if strings.TrimSpace(cfg.Server.Host) == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.System.PollSeconds < 2 || cfg.System.PollSeconds > 60 {
		cfg.System.PollSeconds = 10
	}
	if cfg.System.MetricsTTLSeconds < 2 || cfg.System.MetricsTTLSeconds > 60 {
		cfg.System.MetricsTTLSeconds = 10
	}
	if cfg.System.VersionsTTLSeconds < 30 || cfg.System.VersionsTTLSeconds > 3600 {
		cfg.System.VersionsTTLSeconds = 300
	}
	if cfg.Logs.TailLines <= 0 || cfg.Logs.TailLines > 1000 {
		cfg.Logs.TailLines = 200
	}
	if cfg.Logs.FastRefreshMs < 1000 || cfg.Logs.FastRefreshMs > 30000 {
		cfg.Logs.FastRefreshMs = 3000
	}
	if cfg.Logs.ErrorWindowHours <= 0 || cfg.Logs.ErrorWindowHours > 168 {
		cfg.Logs.ErrorWindowHours = 24
	}
	if cfg.Logs.MaxErrorSignatures <= 0 || cfg.Logs.MaxErrorSignatures > 10000 {
		cfg.Logs.MaxErrorSignatures = 1000
	}
	if cfg.System.GatewayTimeoutMs < 200 || cfg.System.GatewayTimeoutMs > 15000 {
		cfg.System.GatewayTimeoutMs = 5000
	}
	// Upper bound 30000 (was 15000) accommodates runtimes where
	// `openclaw status --json` is wrapped in docker exec or similar that adds
	// ~10s of overhead (see issue #31). Worst case the dashboard waits 30s
	// for cold metrics, which is still finite and protects the page load.
	if cfg.System.ColdPathTimeoutMs < 200 || cfg.System.ColdPathTimeoutMs > 30000 {
		cfg.System.ColdPathTimeoutMs = DefaultColdPathTimeoutMs
	}
	if cfg.System.CPUTimeoutMs < 500 || cfg.System.CPUTimeoutMs > 20000 {
		cfg.System.CPUTimeoutMs = DefaultCPUTimeoutMs
	}
	if !ValidPort(cfg.System.GatewayPort) {
		cfg.System.GatewayPort = cfg.AI.GatewayPort
	}
	if cfg.System.DiskPath == "" {
		cfg.System.DiskPath = "/"
	}
	if cfg.System.WarnPercent <= 0 || cfg.System.WarnPercent >= 100 {
		cfg.System.WarnPercent = 70
	}
	if cfg.System.CriticalPercent <= cfg.System.WarnPercent || cfg.System.CriticalPercent > 100 {
		if cfg.System.WarnPercent < 95 {
			cfg.System.CriticalPercent = cfg.System.WarnPercent + 15
			if cfg.System.CriticalPercent > 100 {
				cfg.System.CriticalPercent = 100
			}
		} else {
			cfg.System.CriticalPercent = 100
		}
	}
	clampThreshold := func(t *MetricThreshold, globalWarn, globalCrit float64) {
		if t.Warn <= 0 || t.Warn >= 100 {
			t.Warn = globalWarn
		}
		if t.Critical <= t.Warn || t.Critical > 100 {
			switch {
			case globalCrit > t.Warn && globalCrit <= 100:
				t.Critical = globalCrit
			case t.Warn < 95:
				t.Critical = t.Warn + 15
				if t.Critical > 100 {
					t.Critical = 100
				}
			default:
				t.Critical = 100
			}
		}
	}
	clampThreshold(&cfg.System.CPU, cfg.System.WarnPercent, cfg.System.CriticalPercent)
	clampThreshold(&cfg.System.RAM, cfg.System.WarnPercent, cfg.System.CriticalPercent)
	clampThreshold(&cfg.System.Swap, cfg.System.WarnPercent, cfg.System.CriticalPercent)
	clampThreshold(&cfg.System.Disk, cfg.System.WarnPercent, cfg.System.CriticalPercent)
	return cfg
}

// applyLegacyLogAliases copies each snake_case logs alias onto its camelCase
// field when the camelCase key is absent from the raw logs object, or present
// with an empty/non-positive value. Key presence matters because Default()
// pre-fills the camelCase fields, so their values alone cannot show whether
// the operator set them.
func applyLegacyLogAliases(logs *LogsConfig, raw []byte) {
	var doc struct {
		Logs map[string]json.RawMessage `json:"logs"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc) // malformed JSON was already reported by Load
	}
	unset := func(key string, zero bool) bool {
		_, present := doc.Logs[key]
		return !present || zero
	}
	if len(logs.LogSources) > 0 && unset("sources", len(logs.Sources) == 0) {
		logs.Sources = append([]string{}, logs.LogSources...)
	}
	if logs.LogTailLines > 0 && unset("tailLines", logs.TailLines <= 0) {
		logs.TailLines = logs.LogTailLines
	}
	if logs.LogFastRefreshMs > 0 && unset("fastRefreshMs", logs.FastRefreshMs <= 0) {
		logs.FastRefreshMs = logs.LogFastRefreshMs
	}
	if logs.ErrorFeedWindowHours > 0 && unset("errorWindowHours", logs.ErrorWindowHours <= 0) {
		logs.ErrorWindowHours = logs.ErrorFeedWindowHours
	}
}

// ReadDotenv parses a .env file into a map. Missing or unreadable files yield
// an empty map. Each non-comment line is KEY=VALUE, optionally prefixed by
// "export" and whitespace. A value opening with ' or " ends at the matching
// closing quote (text after it is ignored); an unquoted value ends before the
// first " #" or "\t#" inline comment. A "#" not preceded by whitespace is kept.
func ReadDotenv(path string) map[string]string {
	result := make(map[string]string)
	expanded := ExpandHome(path)
	f, err := os.Open(expanded)
	if err != nil {
		return result
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip an optional "export" keyword before splitting on '=' so that
		// "notexport=val" and "exportFOO=1" stay literal keys while
		// "export FOO=bar" or "export<TAB>FOO=bar" yields FOO=bar.
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		before, after, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(before)
		if key == "" {
			continue
		}
		result[key] = dotenvValue(after)
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("[dashboard] dotenv: scanner error", "path", expanded, "error", err)
	}
	return result
}

// dotenvValue extracts the value from the text after '=' in a .env line.
func dotenvValue(raw string) string {
	val := strings.TrimSpace(raw)
	if val != "" && (val[0] == '"' || val[0] == '\'') {
		if end := closingQuote(val); end > 0 {
			return val[1:end]
		}
		return val // unterminated quote: keep the literal text
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "\t#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

// closingQuote returns the index of the quote closing val[0], or -1. Inside
// double quotes a backslash-escaped quote does not close the value; the
// escape is kept verbatim.
func closingQuote(val string) int {
	quote := val[0]
	for i := 1; i < len(val); i++ {
		switch {
		case quote == '"' && val[i] == '\\':
			i++ // skip the escaped byte
		case val[i] == quote:
			return i
		}
	}
	return -1
}

// ExpandHome replaces a leading "~/" with the current user's home directory.
// Other paths, and every path when the home directory is unknown, are
// returned unchanged.
func ExpandHome(path string) string {
	return appopenclaw.ExpandHome(path)
}
