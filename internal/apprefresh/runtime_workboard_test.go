package apprefresh

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestWorkboardSummaryCapabilityAndProjection(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		t.Run(strings.ToUpper(map[bool]string{true: "loaded", false: "disabled"}[loaded]), func(t *testing.T) {
			calls := 0
			client := appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				calls++
				body := `{"plugins":[{"id":"workboard","status":"disabled"}]}`
				if args[0] == "plugins" && loaded {
					body = `{"plugins":[{"id":"workboard","status":"loaded"}]}`
				}
				if args[0] == "gateway" {
					body = `{"boards":[{"id":"board","name":"Example","total":4,"byStatus":{"ready":2},"defaultWorkspace":"secret-workspace","orchestration":{"token":"secret-token"}}]}`
				}
				return exec.CommandContext(ctx, "printf", "%s", body)
			}}
			result, err := ReadWorkboardSummary(t.Context(), client)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), "secret") {
				t.Fatalf("leaked config: %s", data)
			}
			if !loaded && calls != 1 {
				t.Fatal("queried disabled plugin")
			}
			if loaded && calls != 2 {
				t.Fatal("did not read boards")
			}
		})
	}
}
