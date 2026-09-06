package apprefresh

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestSessionModelCacheSeparatesTargets(t *testing.T) {
	c := newLiveSessionModelCache()
	c.fetchFn = func(ctx context.Context, _ func(context.Context, string, ...string) *exec.Cmd, _ string) map[string]string {
		return map[string]string{"session": appopenclaw.TargetFromContext(ctx).Profile}
	}
	for _, profile := range []string{"first", "second", "first"} {
		ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Mode: "native", Profile: profile})
		if got := c.fetch(ctx, time.Now(), time.Minute)["session"]; got != profile {
			t.Fatalf("profile=%s got=%s", profile, got)
		}
	}
}

func TestCatalogCacheSeparatesTargets(t *testing.T) {
	c := newModelCatalogCache()
	c.runner = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		profile := appopenclaw.TargetFromContext(ctx).Profile
		return exec.CommandContext(ctx, "printf", "%s", `{"models":[{"key":"test/model","name":"`+profile+`"}]}`)
	}
	for _, profile := range []string{"first", "second", "first"} {
		ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Mode: "native", Profile: profile})
		if got := c.fetch(ctx, time.Now(), time.Minute).names["test/model"]; got != profile {
			t.Fatalf("profile=%s got=%s", profile, got)
		}
	}
}
