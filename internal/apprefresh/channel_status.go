package apprefresh

import (
	"context"
	"encoding/json"
	"maps"
	"os/exec"
	"time"
)

var channelStatusCollector = collectChannelStatusViaCLI

func collectChannelStatusViaCLI(ctx context.Context, runner func(context.Context, string, ...string) *exec.Cmd, resolve func() string) (map[string]any, bool) {
	if runner == nil {
		runner = execCommandContext
	}
	if resolve == nil {
		resolve = resolveOpenclawBin
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := boundedOutput(runner(ctx, resolve(), "channels", "status", "--probe", "--json", "--timeout", "10000"), maxCLIOutputBytes)
	if err != nil {
		return nil, false
	}
	return channelStatusFromBytes(out)
}

func channelStatusFromBytes(data []byte) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	rawAccounts, ok := payload["channelAccounts"].(map[string]any)
	if !ok || len(rawAccounts) == 0 {
		return nil, false
	}
	out := make(map[string]any, len(rawAccounts))
	for channel, raw := range rawAccounts {
		accounts, ok := raw.([]any)
		if !ok || len(accounts) == 0 {
			continue
		}
		status := channelStatusFromAccounts(accounts)
		if status != nil {
			out[channel] = status
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func channelStatusFromAccounts(accounts []any) map[string]any {
	enabled := false
	configured := false
	connectedSet := false
	connected := false
	health := ""
	errorText := ""

	for _, raw := range accounts {
		account := asObj(raw)
		if account == nil {
			continue
		}
		if b, ok := account["enabled"].(bool); ok {
			enabled = enabled || b
		} else {
			enabled = true
		}
		if b, ok := account["configured"].(bool); ok {
			configured = configured || b
		} else {
			configured = true
		}
		if b, ok := account["connected"].(bool); ok {
			connectedSet = true
			connected = connected || b
		}
		if probe := asObj(account["probe"]); probe != nil {
			if b, ok := probe["ok"].(bool); ok {
				connectedSet = true
				connected = connected || b
				if !b && errorText == "" {
					errorText = jsonStr(probe, "error")
				}
			}
		}
		if state := jsonStr(account, "healthState"); state != "" {
			health = state
		}
		if err := jsonStr(account, "lastError"); err != "" && errorText == "" {
			errorText = err
		}
	}
	if !connectedSet {
		connected = enabled && configured && errorText == ""
	}
	if health == "" {
		if connected {
			health = "healthy"
		} else if errorText != "" || configured {
			health = "unhealthy"
		}
	}
	return map[string]any{
		"enabled":    enabled,
		"configured": configured,
		"connected":  connected,
		"health":     health,
		"error":      errorText,
	}
}

func overlayChannelStatus(agentConfig map[string]any, cliStatus map[string]any) {
	if len(cliStatus) == 0 {
		return
	}
	cs, ok := agentConfig["channelStatus"].(map[string]any)
	if !ok {
		return
	}
	for channel, status := range cliStatus {
		incoming, ok := status.(map[string]any)
		if !ok {
			continue
		}
		current, _ := cs[channel].(map[string]any)
		if current == nil {
			current = map[string]any{}
			cs[channel] = current
		}
		maps.Copy(current, incoming)
	}
}
