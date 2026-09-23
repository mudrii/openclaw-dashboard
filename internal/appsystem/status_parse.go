package appsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func parseOpenclawStatusJSON(output string, versions SystemVersions) (SystemOpenclawStatus, error) {
	status := SystemOpenclawStatus{CurrentVersion: versions.Openclaw, LatestVersion: versions.Latest}
	var raw map[string]any
	if err := decodeJSONObjectFromOutput(output, &raw); err != nil {
		return status, err
	}
	if current, ok := raw["currentVersion"].(string); ok && current != "" {
		status.CurrentVersion = current
	}
	if current, ok := raw["version"].(string); ok && current != "" && status.CurrentVersion == "" {
		status.CurrentVersion = current
	}
	if latest, ok := raw["latestVersion"].(string); ok && latest != "" {
		status.LatestVersion = latest
	}
	if ms, ok := int64FromAny(raw["connectLatencyMs"]); ok {
		status.ConnectLatencyMs = ms
	}
	if sec, ok := raw["security"].(map[string]any); ok {
		status.Security = sec
	}
	if sec, ok := raw["securityAudit"].(map[string]any); ok {
		status.SecurityAudit = sec
	}
	if diag, ok := raw["secretDiagnostics"]; ok {
		status.SecretDiagnostics = diag
	}
	if update, ok := raw["update"].(map[string]any); ok {
		status.Update = update
	}
	if updateChannel, ok := raw["updateChannel"]; ok {
		status.UpdateChannel = updateChannel
	}
	if updateChannelSource, ok := raw["updateChannelSource"]; ok {
		status.UpdateChannelSource = updateChannelSource
	}
	if runtimeVersion, ok := raw["runtimeVersion"].(string); ok {
		status.RuntimeVersion = runtimeVersion
		if status.CurrentVersion == "" {
			status.CurrentVersion = runtimeVersion
		}
	}
	// INT-2: additive rich blocks. Typed sub-objects (tasks, eventLoop) are
	// re-decoded from their raw value; loose blocks pass through as maps. Any
	// absent or malformed block is left nil so minimal status output is
	// back-compatible.
	status.Tasks = decodeStatusField[SystemOpenclawTasks](raw, "tasks")
	status.EventLoop = decodeStatusField[SystemOpenclawEventLoop](raw, "eventLoop")
	if pc, ok := raw["pluginCompatibility"].(map[string]any); ok {
		status.PluginCompatibility = pc
	}
	if hb, ok := raw["lastHeartbeat"].(map[string]any); ok {
		status.LastHeartbeat = hb
	} else if hb, ok := raw["heartbeat"].(map[string]any); ok {
		status.LastHeartbeat = hb
	}
	status.ChannelSummary = stringSliceFromAny(raw["channelSummary"])
	if agents, ok := raw["agents"]; ok {
		status.Agents = agents
	}
	if gateway, ok := raw["gateway"].(map[string]any); ok {
		status.Gateway = gateway
	}
	if gatewayService, ok := raw["gatewayService"].(map[string]any); ok {
		status.GatewayService = gatewayService
	}
	if nodeService, ok := raw["nodeService"].(map[string]any); ok {
		status.NodeService = nodeService
	}
	if memory, ok := raw["memory"].(map[string]any); ok {
		status.Memory = memory
	}
	if memoryPlugin, ok := raw["memoryPlugin"].(map[string]any); ok {
		status.MemoryPlugin = memoryPlugin
	}
	if osInfo, ok := raw["os"].(map[string]any); ok {
		status.OS = osInfo
	}
	if sessions, ok := raw["sessions"]; ok {
		status.Sessions = sessions
	}
	if taskAudit, ok := raw["taskAudit"].(map[string]any); ok {
		status.TaskAudit = taskAudit
	}
	if retainedLost, ok := raw["taskAuditRetainedLost"]; ok {
		status.TaskAuditRetainedLost = retainedLost
	}
	if events, ok := raw["queuedSystemEvents"]; ok {
		status.QueuedSystemEvents = events
	}
	return status, nil
}

// decodeStatusField re-decodes a named sub-object of the parsed status map into
// a typed struct, returning nil when the key is absent or its value does not
// decode. Used for the typed INT-2 blocks (tasks, eventLoop) so a malformed
// block degrades to "not shown" rather than failing the whole status parse.
func decodeStatusField[T any](raw map[string]any, key string) *T {
	v, ok := raw[key]
	if !ok || v == nil {
		// Absent or explicit JSON null → omit the block (a nil value would
		// otherwise decode to a non-nil zero struct and emit an empty block).
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return &out
}

// decodeJSONObjectFromOutput finds the first '{'-prefixed substring of output
// that parses as a valid JSON object and decodes it into v. CLI tools often
// emit log preambles like "[INFO] starting up {wrong} {actual:json}" — naive
// "first brace wins" fails on those, so we scan forward through every '{'
// position until one parses. Emits a debug log when the JSON started after
// non-empty preamble so operators can spot stdout pollution from upstream.
func decodeJSONObjectFromOutput(output string, v any) error {
	var lastErr error
	for i := 0; i < len(output); i++ {
		if output[i] != '{' {
			continue
		}
		// Decode (not Unmarshal) stops after the first complete value, so
		// trailing log lines after the JSON object do not fail the parse.
		if err := json.NewDecoder(strings.NewReader(output[i:])).Decode(v); err == nil {
			if i > 0 {
				slog.Debug("decoded JSON after preamble", "preamble_bytes", i)
			}
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("decode json: %w", lastErr)
	}
	return fmt.Errorf("json object not found")
}

// BoolFromAny reports v as a bool and whether v was a bool.
func BoolFromAny(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func int64FromAny(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		if err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func stringSliceFromAny(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ParseGatewayStatusJSON parses `openclaw gateway status --json` output.
// The JSON has shape: {"service":{"loaded":true,"runtime":{...}},...}
func ParseGatewayStatusJSON(ctx context.Context, output string) SystemGateway {
	var result struct {
		Service struct {
			Loaded     bool   `json:"loaded"`
			TargetRole string `json:"targetRole"`
			Runtime    struct {
				Status string `json:"status"`
				PID    int    `json:"pid"`
			} `json:"runtime"`
		} `json:"service"`
		Version string `json:"version"`
		Gateway struct {
			Version string `json:"version"`
		} `json:"gateway"`
		RPC *struct {
			OK      bool   `json:"ok"`
			Version string `json:"version"`
			Server  struct {
				Version string `json:"version"`
			} `json:"server"`
		} `json:"rpc"`
	}
	if err := decodeJSONObjectFromOutput(output, &result); err == nil {
		// runtime.status is authoritative when reported; Loaded is only a
		// fallback for CLIs that omit it (a loaded-but-stopped service is
		// offline).
		status := "offline"
		switch result.Service.Runtime.Status {
		case "running":
			status = "online"
		case "":
			if result.Service.Loaded {
				status = "online"
			}
		}
		gw := SystemGateway{Version: result.Version, Status: status, PID: result.Service.Runtime.PID}
		if result.Gateway.Version != "" {
			gw.Version = result.Gateway.Version
		}
		if result.RPC != nil {
			gw.Status = "unknown"
			if result.RPC.OK {
				gw.Status = "online"
			}
			if result.RPC.Version != "" {
				gw.Version = result.RPC.Version
			}
			if result.RPC.Server.Version != "" {
				gw.Version = result.RPC.Server.Version
			}
		}
		if result.Service.TargetRole == "diagnostic-only" || appopenclaw.TargetFromContext(ctx).IsContainer() {
			gw.PID = 0
		}
		// Get uptime + memory from /proc or ps if we have a PID
		if gw.PID > 0 {
			gw.Uptime, gw.Memory = GetProcessInfo(ctx, gw.PID)
		}
		return gw
	}
	// Fallback: text parsing
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "not loaded") && !strings.Contains(lower, "not running") &&
		(strings.Contains(lower, "loaded") || strings.Contains(lower, "running")) {
		return SystemGateway{Status: "online"}
	}
	return SystemGateway{Status: "offline"}
}

func normalizeCLIVersion(out string) string {
	parts := strings.Fields(out)
	if len(parts) > 1 && strings.EqualFold(parts[0], "openclaw") {
		return parts[1]
	}
	return strings.TrimSpace(out)
}
