package main

import (
	"net"
	"os"
	"testing"
)

func TestLocalIP_ReturnsIPv4OrEmpty(t *testing.T) {
	ip := localIP()
	if ip == "" {
		t.Log("localIP returned empty — no non-loopback interface (acceptable in CI)")
		return
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("localIP returned invalid IP: %q", ip)
	}
	if parsed.To4() == nil {
		t.Fatalf("localIP should return IPv4, got %q", ip)
	}
	if parsed.IsLoopback() {
		t.Fatalf("localIP should not return loopback, got %q", ip)
	}
}

func TestLocalIP_Deterministic(t *testing.T) {
	ip1 := localIP()
	ip2 := localIP()
	if ip1 != ip2 {
		t.Errorf("localIP not deterministic: %q vs %q", ip1, ip2)
	}
}

func TestDefaultConfig_MaxTokens(t *testing.T) {
	cfg := defaultConfig()
	if cfg.AI.MaxTokens != 512 {
		t.Fatalf("expected default MaxTokens 512, got %d", cfg.AI.MaxTokens)
	}
}

func TestLoadConfig_MaxTokensClamped(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    int
	}{
		{"zero clamps to 512", `{"ai":{"maxTokens":0}}`, 512},
		{"negative clamps to 512", `{"ai":{"maxTokens":-1}}`, 512},
		{"exceeds max clamps to 512", `{"ai":{"maxTokens":9999}}`, 512},
		{"valid value preserved", `{"ai":{"maxTokens":1024}}`, 1024},
		{"boundary 4096 preserved", `{"ai":{"maxTokens":4096}}`, 4096},
		{"boundary 1 preserved", `{"ai":{"maxTokens":1}}`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(dir+"/config.json", []byte(tt.json), 0644)
			cfg := loadConfig(dir)
			if cfg.AI.MaxTokens != tt.want {
				t.Errorf("expected MaxTokens=%d, got %d", tt.want, cfg.AI.MaxTokens)
			}
		})
	}
}
