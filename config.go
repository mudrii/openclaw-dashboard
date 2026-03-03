package main

import (
	"bufio"
	"encoding/json"
	"log"
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

type AlertsConfig struct {
	DailyCostHigh float64 `json:"dailyCostHigh"`
	DailyCostWarn float64 `json:"dailyCostWarn"`
	ContextPct    float64 `json:"contextPct"`
	MemoryMb      float64 `json:"memoryMb"`
}

type SystemConfig struct {
	Enabled            bool    `json:"enabled"`
	PollSeconds        int     `json:"pollSeconds"`
	MetricsTTLSeconds  int     `json:"metricsTtlSeconds"`
	VersionsTTLSeconds int     `json:"versionsTtlSeconds"`
	GatewayTimeoutMs   int     `json:"gatewayTimeoutMs"`
	DiskPath           string  `json:"diskPath"`
	WarnPercent        float64 `json:"warnPercent"`
	CriticalPercent    float64 `json:"criticalPercent"`
}

type Config struct {
	Bot      BotConfig     `json:"bot"`
	Theme    ThemeConfig   `json:"theme"`
	Timezone string        `json:"timezone"`
	Refresh  RefreshConfig `json:"refresh"`
	Server   ServerConfig  `json:"server"`
	AI       AIConfig      `json:"ai"`
	Alerts   AlertsConfig  `json:"alerts"`
	System   SystemConfig  `json:"system"`
}

// defaults
func defaultConfig() Config {
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
		Alerts: AlertsConfig{
			DailyCostHigh: 50,
			DailyCostWarn: 20,
			ContextPct:    80,
			MemoryMb:      640,
		},
		System: SystemConfig{
			Enabled:            true,
			PollSeconds:        5,
			MetricsTTLSeconds:  5,
			VersionsTTLSeconds: 300,
			GatewayTimeoutMs:   1500,
			DiskPath:           "/",
			WarnPercent:        70,
			CriticalPercent:    85,
		},
	}
}

func loadConfig(dir string) Config {
	cfg := defaultConfig()
	path := filepath.Join(dir, "config.json")
	f, err := os.Open(path)
	if err != nil {
		return cfg
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		log.Printf("[dashboard] WARNING: invalid config.json, using defaults for missing/invalid fields: %v", err)
	}
	// Clamp/default all AI fields — guard against zero, negative, or missing values
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
	// Clamp system config
	if cfg.System.PollSeconds < 2 || cfg.System.PollSeconds > 60 {
		cfg.System.PollSeconds = 5
	}
	if cfg.System.MetricsTTLSeconds < 2 || cfg.System.MetricsTTLSeconds > 60 {
		cfg.System.MetricsTTLSeconds = 5
	}
	if cfg.System.VersionsTTLSeconds < 30 || cfg.System.VersionsTTLSeconds > 3600 {
		cfg.System.VersionsTTLSeconds = 300
	}
	if cfg.System.GatewayTimeoutMs < 200 || cfg.System.GatewayTimeoutMs > 10000 {
		cfg.System.GatewayTimeoutMs = 1500
	}
	if cfg.System.DiskPath == "" {
		cfg.System.DiskPath = "/"
	}
	if cfg.System.WarnPercent <= 0 || cfg.System.WarnPercent >= 100 {
		cfg.System.WarnPercent = 70
	}
	if cfg.System.CriticalPercent <= cfg.System.WarnPercent || cfg.System.CriticalPercent > 100 {
		cfg.System.CriticalPercent = 85
	}
	return cfg
}

// readDotenv reads KEY=VALUE pairs from a .env file.
// Ignores blank lines and comments (#). Handles KEY=VAL=WITH=EQUALS.
func readDotenv(path string) map[string]string {
	result := make(map[string]string)
	expanded := expandHome(path)
	f, err := os.Open(expanded)
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes (double or single)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		result[key] = val
	}
	return result
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
