package apprefresh

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCollectTokenUsageWithCache_HandlesLargeJSONLLine(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	large := map[string]any{
		"timestamp": "2026-03-22T10:00:00Z",
		"message": map[string]any{
			"role":  "assistant",
			"model": "openai/gpt-5",
			"usage": map[string]any{
				"totalTokens": 100.0,
				"input":       60.0,
				"output":      40.0,
				"cacheRead":   0.0,
				"cost":        map[string]any{"total": 0.12},
			},
			"content": strings.Repeat("x", 2*1024*1024+512),
		},
	}
	data, err := json.Marshal(large)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(sessionDir, "big.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	modelsAll := map[string]*TokenBucket{}
	modelsToday := map[string]*TokenBucket{}
	models7d := map[string]*TokenBucket{}
	models30d := map[string]*TokenBucket{}
	subagentAll := map[string]*TokenBucket{}
	subagentToday := map[string]*TokenBucket{}
	subagent7d := map[string]*TokenBucket{}
	subagent30d := map[string]*TokenBucket{}
	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	CollectTokenUsageWithCache(
		filepath.Join(t.TempDir(), "token-cache.json"),
		basePath, time.UTC, "2026-03-22", "2026-03-15", "2026-02-20",
		map[string]string{}, map[string]string{}, map[string]string{},
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

	got := modelsAll["GPT-5"]
	if got == nil || got.Total != 100 {
		t.Fatalf("expected oversized JSONL line to be counted, got %+v", got)
	}
}

func TestCollectTokenUsageWithCache_ReusesUnchangedFileSummary(t *testing.T) {
	tmp := t.TempDir()
	basePath := filepath.Join(tmp, "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	line := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0.12}}}}` + "\n"
	filePath := filepath.Join(sessionDir, "cached.jsonl")
	if err := os.WriteFile(filePath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(tmp, "token-cache.json")

	run := func() map[string]*TokenBucket {
		modelsAll := map[string]*TokenBucket{}
		modelsToday := map[string]*TokenBucket{}
		models7d := map[string]*TokenBucket{}
		models30d := map[string]*TokenBucket{}
		subagentAll := map[string]*TokenBucket{}
		subagentToday := map[string]*TokenBucket{}
		subagent7d := map[string]*TokenBucket{}
		subagent30d := map[string]*TokenBucket{}
		dailyCosts := map[string]map[string]float64{}
		dailyTokens := map[string]map[string]int{}
		dailyCalls := map[string]map[string]int{}
		dailySubagentCosts := map[string]float64{}
		dailySubagentCount := map[string]int{}

		CollectTokenUsageWithCache(
			cachePath,
			basePath, time.UTC, "2026-03-22", "2026-03-15", "2026-02-20",
			map[string]string{}, map[string]string{}, map[string]string{},
			modelsAll, modelsToday, models7d, models30d,
			subagentAll, subagentToday, subagent7d, subagent30d,
			dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
		)
		return modelsAll
	}

	first := run()
	if first["GPT-5"] == nil || first["GPT-5"].Total != 100 {
		t.Fatalf("expected initial parse to count tokens, got %+v", first["GPT-5"])
	}

	if err := os.Chmod(filePath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filePath, 0o644)

	second := run()
	if second["GPT-5"] == nil || second["GPT-5"].Total != 100 {
		t.Fatalf("expected cached summary to be reused, got %+v", second["GPT-5"])
	}
}

func TestCollectSessions_CachesLiveModelLookup(t *testing.T) {
	prevFetcher := fetchLiveSessionModels
	defer func() { fetchLiveSessionModels = prevFetcher }()
	sessionModelCache = liveSessionModelCache{}

	calls := 0
	fetchLiveSessionModels = func() map[string]string {
		calls++
		return map[string]string{"agent:main:chat": "openai/gpt-5"}
	}

	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	modelAliases := map[string]string{"openai/gpt-5": "GPT-5"}
	stores := []SessionStoreFile{{
		AgentName: "main",
		Store: map[string]map[string]any{
			"agent:main:chat": {
				"sessionId":     "sid-1",
				"updatedAt":     float64(now.UnixMilli()),
				"contextTokens": 1000.0,
				"totalTokens":   100.0,
			},
		},
	}}

	knownA := map[string]string{}
	gotA := collectSessions(stores, t.TempDir(), time.UTC, now, "2026-03-23", modelAliases, knownA, nil, 30*time.Second)
	knownB := map[string]string{}
	gotB := collectSessions(stores, t.TempDir(), time.UTC, now.Add(5*time.Second), "2026-03-23", modelAliases, knownB, nil, 30*time.Second)

	if calls != 1 {
		t.Fatalf("expected one live model fetch within TTL, got %d", calls)
	}
	if len(gotA) != 1 || len(gotB) != 1 {
		t.Fatalf("expected sessions on both calls, got %d and %d", len(gotA), len(gotB))
	}
	if gotA[0]["model"] != "GPT-5" || gotB[0]["model"] != "GPT-5" {
		t.Fatalf("expected cached live model mapping to apply, got %v and %v", gotA[0]["model"], gotB[0]["model"])
	}
}

func TestFetchLiveSessionModelsCLI_UsesResolvedOpenclawBin(t *testing.T) {
	prevResolve := resolveOpenclawBin
	prevExec := execCommandContext
	defer func() {
		resolveOpenclawBin = prevResolve
		execCommandContext = prevExec
	}()

	var gotName string
	var gotArgs []string
	resolveOpenclawBin = func() string { return "/resolved/openclaw" }
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", `printf '[]'`)
	}

	models := fetchLiveSessionModelsCLI()
	if len(models) != 0 {
		t.Fatalf("expected empty models from empty JSON array, got %+v", models)
	}
	if gotName != "/resolved/openclaw" {
		t.Fatalf("expected resolved openclaw path, got %q", gotName)
	}
	if !slices.Equal(gotArgs, []string{"sessions", "--json"}) {
		t.Fatalf("unexpected args: got %v", gotArgs)
	}
}

func TestGetSessionModel_UsesLastModelChange(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	lines := []string{
		`{"type":"model_change","provider":"openai","modelId":"gpt-4o"}`,
		`{"type":"message","message":{"role":"user","content":"hello"}}`,
		`{"type":"model_change","provider":"openai","modelId":"gpt-5"}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "session-1.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := getSessionModel(basePath, "main", "session-1", map[string]string{"main": "anthropic/claude-sonnet"})
	if got != "openai/gpt-5" {
		t.Fatalf("expected last model change to win, got %q", got)
	}
}

func TestGetSessionModel_FindsLateModelChangeInLargeFile(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "agents")
	sessionDir := filepath.Join(basePath, "main", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	largeMessage := `{"type":"message","message":{"role":"assistant","content":"` + strings.Repeat("x", 128*1024) + `"}}`
	content := strings.Join([]string{
		largeMessage,
		`{"type":"model_change","provider":"openai","modelId":"gpt-5"}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(sessionDir, "session-large.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := getSessionModel(basePath, "main", "session-large", map[string]string{"main": "anthropic/claude-sonnet"})
	if got != "openai/gpt-5" {
		t.Fatalf("expected late model change from large file, got %q", got)
	}
}

func TestParseOpenclawConfig_SortsMapBackedLists(t *testing.T) {
	oc := map[string]any{
		"skills": map[string]any{
			"entries": map[string]any{
				"zeta":  map[string]any{"enabled": true},
				"alpha": map[string]any{"enabled": false},
			},
		},
		"plugins": map[string]any{
			"entries": map[string]any{
				"plugin-b": map[string]any{},
				"plugin-a": map[string]any{},
			},
		},
		"hooks": map[string]any{
			"internal": map[string]any{
				"entries": map[string]any{
					"hook-z": map[string]any{"enabled": true},
					"hook-a": map[string]any{"enabled": true},
				},
			},
		},
		"agents": map[string]any{
			"defaults": map[string]any{
				"model": map[string]any{
					"primary":   "openai/gpt-5",
					"fallbacks": []any{},
				},
				"models": map[string]any{
					"openai/gpt-5": map[string]any{"alias": "GPT-5"},
					"anthropic/claude-sonnet": map[string]any{
						"alias": "Claude Sonnet",
					},
				},
			},
		},
	}

	_, skills, availableModels, _, agentConfig := parseOpenclawConfig(oc, t.TempDir())

	var skillNames []string
	for _, entry := range skills {
		skillNames = append(skillNames, entry["name"].(string))
	}
	if !slices.Equal(skillNames, []string{"alpha", "zeta"}) {
		t.Fatalf("skills order = %v, want alphabetical", skillNames)
	}

	var modelIDs []string
	for _, entry := range availableModels {
		modelIDs = append(modelIDs, entry["id"].(string))
	}
	if !slices.Equal(modelIDs, []string{"anthropic/claude-sonnet", "openai/gpt-5"}) {
		t.Fatalf("available model order = %v, want alphabetical", modelIDs)
	}

	hooks := agentConfig["hooks"].([]any)
	if hooks[0].(map[string]any)["name"] != "hook-a" || hooks[1].(map[string]any)["name"] != "hook-z" {
		t.Fatalf("hook order = %#v, want alphabetical", hooks)
	}

	plugins := agentConfig["plugins"].([]string)
	if !slices.Equal(plugins, []string{"plugin-a", "plugin-b"}) {
		t.Fatalf("plugin order = %v, want alphabetical", plugins)
	}
}
