package appconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ResolveGatewayToken keeps credentials server-side. Only process/dotenv
// environment refs and literal config tokens are supported; never execute a
// configured secret provider as part of dashboard startup.
func ResolveGatewayToken(dotenvPath, statePath string) string {
	if token := os.Getenv("OPENCLAW_GATEWAY_TOKEN"); token != "" {
		return token
	}
	if dotenvPath == defaultDotenvPath || dotenvPath == ExpandHome(defaultDotenvPath) {
		dotenvPath = filepath.Join(statePath, ".env")
	}
	env := ReadDotenv(dotenvPath)
	if token := env["OPENCLAW_GATEWAY_TOKEN"]; token != "" {
		return token
	}
	raw, err := os.ReadFile(filepath.Join(statePath, "openclaw.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Gateway struct {
			Auth struct {
				Token json.RawMessage `json:"token"`
			} `json:"auth"`
		} `json:"gateway"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	var literal string
	if json.Unmarshal(cfg.Gateway.Auth.Token, &literal) == nil {
		return literal
	}
	var ref struct {
		Source string `json:"source"`
		ID     string `json:"id"`
	}
	if json.Unmarshal(cfg.Gateway.Auth.Token, &ref) != nil || ref.Source != "env" || ref.ID == "" {
		return ""
	}
	if token := os.Getenv(ref.ID); token != "" {
		return token
	}
	return env[ref.ID]
}
