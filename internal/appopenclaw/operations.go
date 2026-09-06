package appopenclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SetAutomationEnabled toggles one cron entry, guarded by the configuration
// revision the caller last observed. It is one of the few narrow write
// operations this package exposes; there is no arbitrary gateway write path.
func (c Client) SetAutomationEnabled(ctx context.Context, id, revision string, enabled bool, result any) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || revision == "" {
		return &readError{code: "invalid_request"}
	}
	return c.writeRPC(ctx, "cron.update", map[string]any{"id": id, "expectedConfigRevision": revision, "patch": map[string]bool{"enabled": enabled}}, result)
}

// RunAutomation triggers one cron entry in if-enabled mode, so a disabled entry
// stays disabled.
func (c Client) RunAutomation(ctx context.Context, id string, result any) error {
	if id == "" || strings.ContainsAny(id, `/\\`) {
		return &readError{code: "invalid_request"}
	}
	return c.writeRPC(ctx, "cron.run", map[string]any{"id": id, "mode": "if-enabled"}, result)
}

// AbortRun aborts one identified run. Both the session key and the exact run id
// are required, so a caller cannot abort a session's current run by guessing.
func (c Client) AbortRun(ctx context.Context, sessionKey, runID string, result any) error {
	if sessionKey == "" || runID == "" {
		return &readError{code: "invalid_request"}
	}
	return c.writeRPC(ctx, "chat.abort", map[string]string{"sessionKey": sessionKey, "runId": runID}, result)
}

func (c Client) writeRPC(ctx context.Context, method string, params any, result any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode %s params: %w", method, err)
	}
	return c.runJSON(ctx, method, []string{"gateway", "call", method, "--json", "--timeout", "10000", "--params", string(data)}, result)
}
