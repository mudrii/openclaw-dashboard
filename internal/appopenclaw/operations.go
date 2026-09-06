package appopenclaw

import (
	"context"
	"encoding/json"
	"strings"
)

// These methods deliberately expose no arbitrary gateway write operation.
func (c Client) SetAutomationEnabled(ctx context.Context, id, revision string, enabled bool, result any) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || revision == "" {
		return &readError{code: "invalid_request"}
	}
	return c.writeRPC(ctx, "cron.update", map[string]any{"id": id, "expectedConfigRevision": revision, "patch": map[string]bool{"enabled": enabled}}, result)
}

func (c Client) RunAutomation(ctx context.Context, id string, result any) error {
	if id == "" || strings.ContainsAny(id, `/\\`) {
		return &readError{code: "invalid_request"}
	}
	return c.writeRPC(ctx, "cron.run", map[string]any{"id": id, "mode": "if-enabled"}, result)
}

func (c Client) AbortRun(ctx context.Context, sessionKey, runID string, result any) error {
	if sessionKey == "" || runID == "" {
		return &readError{code: "invalid_request"}
	}
	return c.writeRPC(ctx, "chat.abort", map[string]string{"sessionKey": sessionKey, "runId": runID}, result)
}

func (c Client) writeRPC(ctx context.Context, method string, params any, result any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.runJSON(ctx, []string{"gateway", "call", method, "--json", "--timeout", "10000", "--params", string(data)}, result)
}
