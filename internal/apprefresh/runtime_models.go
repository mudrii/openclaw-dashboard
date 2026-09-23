package apprefresh

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// collectRuntimeModels gathers model readiness one agent at a time. An agent
// that fails is named in the status while the other agents' rows are kept.
func collectRuntimeModels(ctx context.Context, client appopenclaw.Client, agents []string) ([]map[string]any, CollectionStatus) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows := []map[string]any{}
	var firstErr error
	var failed []string
	for _, agent := range agents {
		var status struct {
			Allowed         []string `json:"allowed"`
			ResolvedDefault string   `json:"resolvedDefault"`
			Auth            struct {
				MissingProvidersInUse []string `json:"missingProvidersInUse"`
				Providers             []struct {
					Provider  string `json:"provider"`
					Effective struct {
						Kind string `json:"kind"`
					} `json:"effective"`
				} `json:"providers"`
			} `json:"auth"`
		}
		err := client.ReadAgentModels(ctx, agent, &status)
		if err == nil && status.Allowed == nil {
			err = fmt.Errorf("missing model policy")
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, agent)
			continue
		}
		for _, model := range status.Allowed {
			provider, _, _ := strings.Cut(model, "/")
			var kind any
			for _, auth := range status.Auth.Providers {
				if auth.Provider == provider {
					kind = auth.Effective.Kind
					break
				}
			}
			name, catalogKnown := catalogDisplayName(model)
			var catalog any
			if catalogKnown {
				catalog = name
			}
			var missing any
			if status.Auth.MissingProvidersInUse != nil {
				missing = slices.Contains(status.Auth.MissingProvidersInUse, provider)
			}
			rows = append(rows, map[string]any{"agentId": agent, "modelId": model, "provider": provider, "allowed": true, "default": model == status.ResolvedDefault, "authKind": kind, "missingProviderInUse": missing, "catalogName": catalog, "inferenceVerified": false})
		}
	}
	return rows, perAgentStatus("cli.models.status", firstErr, failed, len(agents))
}
