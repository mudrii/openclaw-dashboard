package appchat

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// Capability reports whether dashboard chat is configured. State is one of
// "disabled", "configuration_unavailable", "endpoint_disabled",
// "credentials_missing", or "configured"; Available is true only for
// "configured". InferenceVerified stays false because no model is probed.
type Capability struct {
	Available             bool   `json:"available"`
	State                 string `json:"state"`
	CredentialsConfigured bool   `json:"credentialsConfigured"`
	InferenceVerified     bool   `json:"inferenceVerified"`
}

// CheckCapability is a configuration check, not an inference or auth probe.
// It never turns on an endpoint or submits a model request.
func CheckCapability(enabled bool, statePath, token string) Capability {
	if !enabled {
		return CheckCapabilityJSON(false, nil, token)
	}
	path := filepath.Join(statePath, "openclaw.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckCapabilityJSON(enabled, nil, token)
	}
	result, err := checkCapability(enabled, data, token)
	if err != nil {
		slog.Warn("[dashboard] chat capability: cannot parse OpenClaw config", "path", path, "error", err)
	}
	return result
}

// CheckCapabilityJSON checks configuration from the selected runtime without
// reading host files or submitting an inference request.
func CheckCapabilityJSON(enabled bool, data []byte, token string) Capability {
	result, _ := checkCapability(enabled, data, token)
	return result
}

// checkCapability also returns the config parse error so file-based callers can
// log it; a nil or JSON-null document is not a parse error.
func checkCapability(enabled bool, data []byte, token string) (Capability, error) {
	result := Capability{State: "disabled", CredentialsConfigured: token != ""}
	if !enabled {
		return result, nil
	}
	result.State = "configuration_unavailable"
	var cfg *struct {
		Gateway struct {
			HTTP struct {
				Endpoints struct {
					ChatCompletions struct {
						Enabled bool `json:"enabled"`
					} `json:"chatCompletions"`
				} `json:"endpoints"`
			} `json:"http"`
		} `json:"gateway"`
	}
	if len(data) == 0 {
		return result, nil
	}
	if err := appopenclaw.UnmarshalConfig(data, &cfg); err != nil {
		return result, err
	}
	if cfg == nil {
		return result, nil
	}
	result.State = "endpoint_disabled"
	if !cfg.Gateway.HTTP.Endpoints.ChatCompletions.Enabled {
		return result, nil
	}
	result.State = "credentials_missing"
	if token == "" {
		return result, nil
	}
	result.State = "configured"
	result.Available = true
	return result, nil
}
