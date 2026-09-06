package apprefresh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// Container state need not be mounted on the host. Never substitute host config
// when the selected gateway is unavailable; callers retain same-target snapshots.
func readSelectedConfig(ctx context.Context, client appopenclaw.Client, hostPath string) (map[string]any, string, error) {
	if appopenclaw.TargetFromContext(ctx).IsContainer() {
		var response struct {
			Config map[string]any `json:"config"`
			Path   string         `json:"path"`
			Valid  bool           `json:"valid"`
			Exists bool           `json:"exists"`
		}
		if err := client.Read(ctx, "config.get", struct{}{}, &response); err != nil {
			return nil, "", err
		}
		if !response.Valid || !response.Exists || response.Config == nil || response.Path == "" {
			return nil, "", fmt.Errorf("selected runtime configuration unavailable")
		}
		return response.Config, response.Path, nil
	}
	data, err := os.ReadFile(hostPath)
	if err != nil {
		return nil, hostPath, err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, hostPath, err
	}
	if config == nil {
		return nil, hostPath, fmt.Errorf("empty native configuration")
	}
	return config, hostPath, nil
}
