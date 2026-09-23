package apprefresh

import (
	"context"
	"os/exec"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestHasExactActiveRun(t *testing.T) {
	const listing = `{"sessions":[
		{"key":"agent:main:main","sessionId":"s1","hasActiveRun":true,"activeRunIds":["r1","r2"]},
		{"key":"agent:main:idle","sessionId":"s2","hasActiveRun":false,"activeRunIds":["r3"]}
	],"hasMore":false,"totalCount":2}`
	for _, tc := range []struct {
		name, body, key, runID string
		want, wantErr          bool
	}{
		{"active run matches", listing, "agent:main:main", "r2", true, false},
		{"unknown run id", listing, "agent:main:main", "r9", false, false},
		{"run on other session", listing, "agent:main:idle", "r1", false, false},
		{"inactive session", listing, "agent:main:idle", "r3", false, false},
		{"empty identities", listing, "", "", false, false},
		{"malformed listing", `{}`, "agent:main:main", "r1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := appopenclaw.Client{Binary: "openclaw", Runner: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				return exec.CommandContext(ctx, "printf", "%s", tc.body)
			}}
			got, err := HasExactActiveRun(t.Context(), client, tc.key, tc.runID)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("HasExactActiveRun = %v, %v; want %v, err=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
