package apprefresh

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func collectRuntimeModels(ctx context.Context, client appopenclaw.Client, agents []string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows := []map[string]any{}
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
		if err := client.ReadAgentModels(ctx, agent, &status); err != nil {
			return rows, err
		}
		if status.Allowed == nil {
			return rows, fmt.Errorf("missing model policy")
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
	return rows, nil
}
