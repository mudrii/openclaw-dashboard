package appchat

import (
	"encoding/json"
	"os"
	"path/filepath"
)

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
	data, err := os.ReadFile(filepath.Join(statePath, "openclaw.json"))
	if err != nil {
		data = nil
	}
	return CheckCapabilityJSON(enabled, data, token)
}

// CheckCapabilityJSON checks configuration from the selected runtime without
// reading host files or submitting an inference request.
func CheckCapabilityJSON(enabled bool, data []byte, token string) Capability {
	result := Capability{State: "disabled", CredentialsConfigured: token != ""}
	if !enabled {
		return result
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
	if json.Unmarshal(data, &cfg) != nil || cfg == nil {
		return result
	}
	result.State = "endpoint_disabled"
	if !cfg.Gateway.HTTP.Endpoints.ChatCompletions.Enabled {
		return result
	}
	result.State = "credentials_missing"
	if token == "" {
		return result
	}
	result.State = "configured"
	result.Available = true
	return result
}
