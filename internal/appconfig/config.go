// Package appconfig handles configuration loading and defaults for the dashboard.
package appconfig

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type BotConfig struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

type ThemeConfig struct {
	Preset string `json:"preset"`
}

type RefreshConfig struct {
	IntervalSeconds int `json:"intervalSeconds"`
}

type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

type AIConfig struct {
	Enabled     bool   `json:"enabled"`
	GatewayPort int    `json:"gatewayPort"`
	Model       string `json:"model"`
	MaxHistory  int    `json:"maxHistory"`
	DotenvPath  string `json:"dotenvPath"`
}

type LogsConfig struct {
	Enabled              bool     `json:"enabled"`
	TailLines            int      `json:"tailLines"`
	FastRefreshMs        int      `json:"fastRefreshMs"`
	ErrorWindowHours     int      `json:"errorWindowHours"`
	MaxErrorSignatures   int      `json:"maxErrorSignatures"`
	Sources              []string `json:"sources"`
	LogSources           []string `json:"log_sources"`
	LogTailLines         int      `json:"log_tail_lines"`
	LogFastRefreshMs     int      `json:"log_fast_refresh_ms"`
	ErrorFeedWindowHours int      `json:"error_feed_window_hours"`
}

type AlertsConfig struct {
	DailyCostHigh float64 `json:"dailyCostHigh"`
	DailyCostWarn float64 `json:"dailyCostWarn"`
	ContextPct    float64 `json:"contextPct"`
	MemoryMb      float64 `json:"memoryMb"`
}

type MetricThreshold struct {
	Warn     float64 `json:"warn"`
	Critical float64 `json:"critical"`
}

type SystemConfig struct {
	Enabled            bool `json:"enabled"`
	PollSeconds        int  `json:"pollSeconds"`
	MetricsTTLSeconds  int  `json:"metricsTtlSeconds"`
	VersionsTTLSeconds int  `json:"versionsTtlSeconds"`
	GatewayTimeoutMs   int  `json:"gatewayTimeoutMs"`
	// ColdPathTimeoutMs bounds the worst-case wall time of a cold /api/system
	// collection (no warm cache). Each subcollector still has its own per-probe
	// timeout; this is the overall budget that prevents a slow gateway from
	// dragging the whole refresh past the frontend fetch deadline.
	ColdPathTimeoutMs int             `json:"coldPathTimeoutMs"`
	GatewayPort       int             `json:"gatewayPort"`
	DiskPath          string          `json:"diskPath"`
	WarnPercent       float64         `json:"warnPercent"`
	CriticalPercent   float64         `json:"criticalPercent"`
	CPU               MetricThreshold `json:"cpu"`
	RAM               MetricThreshold `json:"ram"`
	Swap              MetricThreshold `json:"swap"`
	Disk              MetricThreshold `json:"disk"`
}

type Config struct {
	Bot      BotConfig     `json:"bot"`
	Theme    ThemeConfig   `json:"theme"`
	Timezone string        `json:"timezone"`
	Refresh  RefreshConfig `json:"refresh"`
	Server   ServerConfig  `json:"server"`
	AI       AIConfig      `json:"ai"`
	Logs     LogsConfig    `json:"logs"`
	Alerts   AlertsConfig  `json:"alerts"`
	System   SystemConfig  `json:"system"`
}

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
			MaxHistory:  6,
			DotenvPath:  "~/.openclaw/.env",
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
			ColdPathTimeoutMs:  4000,
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

func Load(dir string) Config {
	cfg := Default()
	path := filepath.Join(dir, "config.json")
	f, err := os.Open(path)
	if err != nil {
		f, err = os.Open(filepath.Join(dir, "assets", "runtime", "config.json"))
	}
	if err != nil {
		slog.Warn("[dashboard] config: no config.json found, using defaults")
	} else {
		defer func() { _ = f.Close() }()
		if err := json.NewDecoder(f).Decode(&cfg); err != nil {
			slog.Warn("[dashboard] invalid config.json, using defaults for missing/invalid fields", "error", err)
		}
	}
	if len(cfg.Logs.Sources) == 0 && len(cfg.Logs.LogSources) > 0 {
		cfg.Logs.Sources = append([]string{}, cfg.Logs.LogSources...)
	}
	if cfg.Logs.TailLines <= 0 && cfg.Logs.LogTailLines > 0 {
		cfg.Logs.TailLines = cfg.Logs.LogTailLines
	}
	if cfg.Logs.FastRefreshMs <= 0 && cfg.Logs.LogFastRefreshMs > 0 {
		cfg.Logs.FastRefreshMs = cfg.Logs.LogFastRefreshMs
	}
	if cfg.Logs.ErrorWindowHours <= 0 && cfg.Logs.ErrorFeedWindowHours > 0 {
		cfg.Logs.ErrorWindowHours = cfg.Logs.ErrorFeedWindowHours
	}
	if cfg.AI.MaxHistory <= 0 {
		cfg.AI.MaxHistory = 6
	}
	if cfg.AI.GatewayPort <= 0 {
		cfg.AI.GatewayPort = 18789
	}
	if cfg.AI.DotenvPath == "" {
		cfg.AI.DotenvPath = "~/.openclaw/.env"
	}
	if cfg.Refresh.IntervalSeconds <= 0 {
		cfg.Refresh.IntervalSeconds = 30
	}
	if cfg.Server.Port <= 0 {
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
	if cfg.System.ColdPathTimeoutMs < 200 || cfg.System.ColdPathTimeoutMs > 15000 {
		cfg.System.ColdPathTimeoutMs = 4000
	}
	if cfg.System.GatewayPort <= 0 {
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
		before, after, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(before)
		if after0, ok0 := strings.CutPrefix(key, "export "); ok0 {
			key = strings.TrimSpace(after0)
		}
		if key == "" {
			continue
		}
		val := strings.TrimSpace(after)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		result[key] = val
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("[dashboard] dotenv: scanner error", "path", expanded, "error", err)
	}
	return result
}

func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Warn("[dashboard] UserHomeDir failed, cannot expand ~", "error", err)
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
