package main

import (
	"bufio"
	"encoding/json"
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

type Config struct {
	Bot      BotConfig     `json:"bot"`
	Theme    ThemeConfig   `json:"theme"`
	Timezone string        `json:"timezone"`
	Refresh  RefreshConfig `json:"refresh"`
	Server   ServerConfig  `json:"server"`
	AI       AIConfig      `json:"ai"`
	Alerts   AlertsConfig  `json:"alerts"`
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
	_ = json.NewDecoder(f).Decode(&cfg)
	if cfg.AI.MaxHistory == 0 {
		cfg.AI.MaxHistory = 6
	}
	if cfg.AI.GatewayPort == 0 {
		cfg.AI.GatewayPort = 18789
	}
	if cfg.AI.DotenvPath == "" {
		cfg.AI.DotenvPath = "~/.openclaw/.env"
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
