package appconfig

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quietSlog silences Load's warnings for the fuzz run and restores the
// previous default logger afterwards.
func quietSlog(f *testing.F) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Cleanup(func() { slog.SetDefault(prev) })
}

var dotenvSeeds = []string{
	"KEY=value",
	"export FOO=bar",
	"export\tFOO=bar",
	"notexport=val",
	`QUOTED="a b # c" trailing`,
	`SINGLE='x y'`,
	`ESC="a\"b"`,
	`UNTERM="abc`,
	"INLINE=abc # comment",
	"HASH=abc#def",
	"TAB=abc\t# c",
	"=novalue",
	"# comment",
	"NOEQ",
	`EMPTYQ=""`,
	`BS="\`,
	"",
}

// FuzzDotenvValue checks the value extractor never panics, never returns more
// than its input, and that closingQuote only reports a matching quote byte.
func FuzzDotenvValue(f *testing.F) {
	for _, s := range dotenvSeeds {
		_, after, _ := strings.Cut(s, "=")
		f.Add(after)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got := dotenvValue(raw)
		if len(got) > len(raw) {
			t.Fatalf("dotenvValue(%q) = %q, longer than input", raw, got)
		}
		if !strings.Contains(raw, got) {
			t.Fatalf("dotenvValue(%q) = %q, not a substring of the input", raw, got)
		}
		val := strings.TrimSpace(raw)
		if val == "" {
			return
		}
		if end := closingQuote(val); end >= 0 {
			if end == 0 || end >= len(val) || val[end] != val[0] {
				t.Fatalf("closingQuote(%q) = %d, not a matching quote", val, end)
			}
		}
	})
}

// FuzzReadDotenv writes arbitrary bytes as a .env file and checks every parsed
// entry is derived from a single line of it.
func FuzzReadDotenv(f *testing.F) {
	f.Add([]byte(strings.Join(dotenvSeeds, "\n")))
	for _, s := range dotenvSeeds {
		f.Add([]byte(s))
	}
	quietSlog(f)
	dir := f.TempDir()
	path := filepath.Join(dir, ".env")
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		longest := 0
		for line := range strings.Lines(string(data)) {
			longest = max(longest, len(line))
		}
		for key, value := range ReadDotenv(path) {
			if key == "" || strings.ContainsAny(key, "=\n") {
				t.Fatalf("bad key %q", key)
			}
			if strings.ContainsAny(value, "\n") {
				t.Fatalf("value for %q spans lines: %q", key, value)
			}
			if len(value) > longest {
				t.Fatalf("value for %q (%d bytes) longer than any input line (%d)", key, len(value), longest)
			}
		}
	})
}

// FuzzLoad feeds arbitrary bytes as config.json and checks Load never panics
// and always returns a config clamped to its documented ranges, including
// after legacy snake_case log aliases are applied.
func FuzzLoad(f *testing.F) {
	for _, s := range []string{
		`{}`,
		`{"server":{"port":0,"host":"  "}}`,
		`{"ai":{"gatewayPort":70000,"maxHistory":-1}}`,
		`{"logs":{"log_sources":["a"],"log_tail_lines":5000,"log_fast_refresh_ms":10,"error_feed_window_hours":999}}`,
		`{"logs":{"sources":[],"tailLines":0,"log_tail_lines":50}}`,
		`{"system":{"warnPercent":99,"criticalPercent":1,"cpu":{"warn":150,"critical":-1}}}`,
		`{"system":{"warnPercent":96,"disk":{"warn":97}}}`,
		`{"unknown":{"nested":true},"refresh":{"intervalSeconds":-5}}`,
		`not json`,
		`{"logs":null}`,
	} {
		f.Add([]byte(s))
	}
	quietSlog(f)
	dir := f.TempDir()
	path := filepath.Join(dir, "config.json")
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := Load(dir)
		checkLoadedConfig(t, cfg)
	})
}

func checkLoadedConfig(t *testing.T, cfg Config) {
	t.Helper()
	inRange := func(name string, v, lo, hi int) {
		if v < lo || v > hi {
			t.Fatalf("%s = %d, want within [%d, %d]", name, v, lo, hi)
		}
	}
	inRange("server.port", cfg.Server.Port, 1, 65535)
	inRange("ai.gatewayPort", cfg.AI.GatewayPort, 1, 65535)
	inRange("system.gatewayPort", cfg.System.GatewayPort, 1, 65535)
	inRange("ai.maxHistory", cfg.AI.MaxHistory, minMaxHistory, maxMaxHistory)
	inRange("system.pollSeconds", cfg.System.PollSeconds, 2, 60)
	inRange("system.metricsTtlSeconds", cfg.System.MetricsTTLSeconds, 2, 60)
	inRange("system.versionsTtlSeconds", cfg.System.VersionsTTLSeconds, 30, 3600)
	inRange("system.gatewayTimeoutMs", cfg.System.GatewayTimeoutMs, 200, 15000)
	inRange("system.coldPathTimeoutMs", cfg.System.ColdPathTimeoutMs, 200, 30000)
	inRange("system.cpuTimeoutMs", cfg.System.CPUTimeoutMs, 500, 20000)
	inRange("logs.tailLines", cfg.Logs.TailLines, 1, 1000)
	inRange("logs.fastRefreshMs", cfg.Logs.FastRefreshMs, 1000, 30000)
	inRange("logs.errorWindowHours", cfg.Logs.ErrorWindowHours, 1, 168)
	inRange("logs.maxErrorSignatures", cfg.Logs.MaxErrorSignatures, 1, 10000)
	if cfg.Refresh.IntervalSeconds <= 0 {
		t.Fatalf("refresh.intervalSeconds = %d, want > 0", cfg.Refresh.IntervalSeconds)
	}
	if strings.TrimSpace(cfg.Server.Host) == "" {
		t.Fatal("server.host is blank")
	}
	if cfg.AI.DotenvPath == "" || cfg.System.DiskPath == "" {
		t.Fatalf("empty path defaults: dotenv %q disk %q", cfg.AI.DotenvPath, cfg.System.DiskPath)
	}
	if len(cfg.Logs.Sources) == 0 {
		t.Fatal("logs.sources is empty")
	}
	checkThreshold := func(name string, warn, crit float64) {
		if !(warn > 0 && warn < 100 && crit > warn && crit <= 100) {
			t.Fatalf("%s thresholds warn=%v critical=%v, want 0 < warn < critical <= 100", name, warn, crit)
		}
	}
	checkThreshold("system", cfg.System.WarnPercent, cfg.System.CriticalPercent)
	checkThreshold("cpu", cfg.System.CPU.Warn, cfg.System.CPU.Critical)
	checkThreshold("ram", cfg.System.RAM.Warn, cfg.System.RAM.Critical)
	checkThreshold("swap", cfg.System.Swap.Warn, cfg.System.Swap.Critical)
	checkThreshold("disk", cfg.System.Disk.Warn, cfg.System.Disk.Critical)
}
