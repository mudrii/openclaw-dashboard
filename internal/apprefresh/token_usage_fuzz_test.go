package apprefresh

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// checkTokenBucket asserts a bucket built from untrusted session lines holds
// only non-negative counts and a finite, non-negative cost, so it can always
// be marshalled into data.json and the token cache.
func checkTokenBucket(t *testing.T, where string, b TokenBucket) {
	t.Helper()
	if b.Calls < 0 || b.Input < 0 || b.Output < 0 || b.CacheRead < 0 || b.CacheWrite < 0 || b.Total < 0 {
		t.Fatalf("%s: negative token count %+v", where, b)
	}
	if math.IsNaN(b.Cost) || math.IsInf(b.Cost, 0) || b.Cost < 0 {
		t.Fatalf("%s: invalid cost %v", where, b.Cost)
	}
}

// FuzzParseTokenUsageFile runs arbitrary bytes through the session JSONL
// token parser and checks totals stay non-negative and JSON-encodable.
func FuzzParseTokenUsageFile(f *testing.F) {
	line := func(usage string) string {
		return `{"timestamp":"2026-03-01T10:00:00Z","message":{"role":"assistant","model":"gpt-5","usage":` + usage + `}}`
	}
	for _, s := range []string{
		line(`{"input":10,"output":5,"cacheRead":1,"cacheWrite":2,"totalTokens":18,"cost":{"total":0.01}}`),
		line(`{"input":-10,"totalTokens":5,"cost":{"total":-1}}`),
		line(`{"input":1e300,"totalTokens":1e300,"cost":{"total":1e308}}`) + "\n" + line(`{"totalTokens":1,"cost":{"total":1e308}}`),
		`{"message":{"role":"user"}}`,
		`{"usage":{"totalTokens":3},"message":{"role":"assistant"}}`,
		"not json\n\n{}",
		line(`{"totalTokens":7,"cost":{"total":"x"}}`),
	} {
		f.Add([]byte(s))
	}
	dir := f.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		summary, err := parseTokenUsageFile(path, info, time.UTC)
		if err != nil {
			t.Fatalf("parseTokenUsageFile: %v", err)
		}
		for model, b := range summary.Models {
			checkTokenBucket(t, "model "+model, b)
		}
		for day, models := range summary.Daily {
			for model, b := range models {
				checkTokenBucket(t, day+"/"+model, b)
			}
		}
		if math.IsNaN(summary.SessionCost) || math.IsInf(summary.SessionCost, 0) || summary.SessionCost < 0 {
			t.Fatalf("invalid session cost %v", summary.SessionCost)
		}
		if _, err := json.Marshal(summary); err != nil {
			t.Fatalf("summary not JSON-encodable: %v", err)
		}
	})
}

// FuzzCollectGitLog feeds arbitrary `git log` output through collectGitLog via
// the exec seam and checks every entry carries the three string fields.
func FuzzCollectGitLog(f *testing.F) {
	for _, s := range []string{
		"abc1234\x1ffix: thing\x1f2 hours ago\ndef5678\x1ffeat: other\x1f3 days ago",
		"abc1234\x1fonly two fields",
		"no separator here",
		"\x1f\x1f\x1f\x1f",
		"",
	} {
		f.Add([]byte(s))
	}
	prev := execCommandContext
	f.Cleanup(func() { execCommandContext = prev })
	out := filepath.Join(f.TempDir(), "git.out")
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "cat", out)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := os.WriteFile(out, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, entry := range collectGitLog(t.Context(), t.TempDir()) {
			hash, ok1 := entry["hash"].(string)
			msg, ok2 := entry["message"].(string)
			ago, ok3 := entry["ago"].(string)
			if !ok1 || !ok2 || !ok3 {
				t.Fatalf("entry fields not strings: %#v", entry)
			}
			if strings.Contains(hash+msg, "\n") || strings.Contains(ago, "\n") {
				t.Fatalf("entry spans lines: %#v", entry)
			}
		}
	})
}
