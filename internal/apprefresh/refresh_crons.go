package apprefresh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// cronFlappingThreshold is the consecutiveErrors count at or above which a cron
// job is flagged as flapping (unstable) in the dashboard.
const cronFlappingThreshold = 3

// loadCronStateSidecar reads jobs-state.json (introduced in OpenClaw v2026.4.20)
// and returns a job-id → state map. Returns nil when the file is absent or invalid;
// callers fall back to inline state.
func loadCronStateSidecar(statePath string) map[string]map[string]any {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	var sidecar map[string]any
	if err := json.Unmarshal(data, &sidecar); err != nil {
		return nil
	}
	jobs := asObj(sidecar["jobs"])
	if jobs == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(jobs))
	for id, entry := range jobs {
		em := asObj(entry)
		if em == nil {
			continue
		}
		state := asObj(em["state"])
		if len(state) == 0 {
			continue
		}
		out[id] = state
	}
	return out
}

// cronScheduleString renders a job's schedule block into the compact label the
// dashboard shows (cron expression, "Every Nh", an "at" timestamp, or raw JSON).
func cronScheduleString(sched map[string]any) string {
	switch jsonStr(sched, "kind") {
	case "cron":
		return jsonStr(sched, "expr")
	case "every":
		ms, _ := sched["everyMs"].(float64)
		msInt := int64(ms)
		switch {
		case msInt >= 86400000:
			return fmt.Sprintf("Every %dd", msInt/86400000)
		case msInt >= 3600000:
			return fmt.Sprintf("Every %dh", msInt/3600000)
		case msInt >= 60000:
			return fmt.Sprintf("Every %dm", msInt/60000)
		default:
			return fmt.Sprintf("Every %dms", msInt)
		}
	case "at":
		at := jsonStr(sched, "at")
		if len(at) > 16 {
			at = at[:16]
		}
		return at
	default:
		b, _ := json.Marshal(sched)
		return string(b)
	}
}

// cronJobToMap maps one cron job object (from jobs.json or `openclaw cron list
// --json`) to the dashboard cron entry. sidecarStates is the optional
// jobs-state.json override map (legacy split-store); pass nil when the job's
// inline `state` is already authoritative (CLI source, where the gateway merges
// runtime state into each job).
func cronJobToMap(jm map[string]any, sidecarStates map[string]map[string]any, loc *time.Location) map[string]any {
	if jm == nil {
		return nil
	}

	schedStr := cronScheduleString(asObj(jm["schedule"]))

	// Sidecar override contract (OpenClaw v2026.4.20+):
	// When jobs-state.json contains an entry for this job id, it is the
	// authoritative live state — we replace inline state wholesale rather than
	// field-merging (inline fields are pre-migration leftovers). Inline state
	// remains the fallback only when the sidecar is absent entirely or has no
	// entry for this id. The CLI source passes sidecarStates=nil, so its inline
	// state (already gateway-merged) is used directly.
	state := asObj(jm["state"])
	if id := jsonStr(jm, "id"); id != "" {
		if sc, ok := sidecarStates[id]; ok {
			state = sc
		}
	}

	// OpenClaw's canonical field is lastRunStatus; lastStatus is a deprecated
	// alias kept only as a legacy fallback.
	lastStatus := jsonStrDefault(state, "lastRunStatus", "")
	if lastStatus == "" {
		lastStatus = jsonStrDefault(state, "lastStatus", "none")
	}
	// INT-5: richer state — delivery outcome + flapping signal.
	deliveryStatus := jsonStrDefault(state, "lastDeliveryStatus", "")
	lastDiagnostics := stringSliceFromAny(state["lastDiagnostics"])
	consecutiveErrors, _ := state["consecutiveErrors"].(float64)
	consecutiveSkipped, _ := state["consecutiveSkipped"].(float64)
	// Flapping keys on errors only — a skipped run (deduped/throttled) is not an
	// instability signal. consecutiveSkipped is surfaced separately for the UI
	// but intentionally does not feed the flapping flag.
	flapping := int(consecutiveErrors) >= cronFlappingThreshold

	lastRunMs, _ := state["lastRunAtMs"].(float64)
	nextRunMs, _ := state["nextRunAtMs"].(float64)
	durationMs, _ := state["lastDurationMs"].(float64)

	var lastRunStr, nextRunStr string
	if lastRunMs > 0 {
		lastRunStr = time.UnixMilli(int64(lastRunMs)).In(loc).Format("2006-01-02 15:04")
	}
	if nextRunMs > 0 {
		nextRunStr = time.UnixMilli(int64(nextRunMs)).In(loc).Format("2006-01-02 15:04")
	}

	enabled := true
	if e, ok := jm["enabled"].(bool); ok {
		enabled = e
	}

	model := ModelName(jsonStr(asObj(jm["payload"]), "model"))

	return map[string]any{
		"id":                 jsonStr(jm, "id"),
		"name":               jsonStrDefault(jm, "name", "Unknown"),
		"agentId":            jsonStr(jm, "agentId"),
		"schedule":           schedStr,
		"enabled":            enabled,
		"lastRun":            lastRunStr,
		"lastStatus":         lastStatus,
		"lastDurationMs":     int(durationMs),
		"nextRun":            nextRunStr,
		"model":              model,
		"lastDeliveryStatus": deliveryStatus,
		"lastDiagnostics":    lastDiagnostics,
		"consecutiveErrors":  int(consecutiveErrors),
		"consecutiveSkipped": int(consecutiveSkipped),
		"flapping":           flapping,
	}
}

// cronJobsFromBytes extracts the jobs array from a `{jobs:[…]}` envelope (used by
// both jobs.json and `openclaw cron list --json`) or a bare array. The second
// return reports whether the payload parsed as valid cron JSON at all.
func cronJobsFromBytes(data []byte) ([]any, bool) {
	var envelope struct {
		Jobs []any `json:"jobs"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Jobs != nil {
		return envelope.Jobs, true
	}
	var arr []any
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, true
	}
	return nil, false
}

// CollectCrons parses jobs.json (with optional jobs-state.json sidecar override
// for v2026.4.20+) and returns one entry per cron job describing schedule, last
// run, next run, and status. This is the legacy file-based source, used as a
// fallback when the gateway CLI is unavailable (pre-migration installs).
func CollectCrons(cronPath string, loc *time.Location) []map[string]any {
	var crons []map[string]any
	data, err := os.ReadFile(cronPath)
	if err != nil {
		return crons
	}
	jobs, ok := cronJobsFromBytes(data)
	if !ok {
		return crons
	}

	// OpenClaw v2026.4.20+ moved runtime state to a sidecar file. When present,
	// its per-job entries take precedence over inline state (which is now {} in
	// jobs.json).
	sidecarPath := filepath.Join(filepath.Dir(cronPath), "jobs-state.json")
	sidecarStates := loadCronStateSidecar(sidecarPath)

	for _, job := range jobs {
		if entry := cronJobToMap(asObj(job), sidecarStates, loc); entry != nil {
			crons = append(crons, entry)
		}
	}
	return crons
}

// collectCronsViaCLI sources crons from `openclaw cron list --json`, the
// post-SQLite-migration store (OpenClaw 2026.6+ moved cron jobs out of
// cron/jobs.json into shared SQLite, served via the gateway). Each job carries a
// gateway-merged inline `state`, so no sidecar is needed. The bool reports
// success: false means the CLI failed (gateway down, binary missing, or
// unparseable output) and the caller should fall back to CollectCrons. A valid
// empty response is authoritative (true, no jobs).
func collectCronsViaCLI(ctx context.Context, runner func(context.Context, string, ...string) *exec.Cmd, resolve func() string, loc *time.Location) ([]map[string]any, bool) {
	if runner == nil {
		runner = execCommandContext
	}
	if resolve == nil {
		resolve = resolveOpenclawBin
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := boundedOutput(runner(ctx, resolve(), "cron", "list", "--json"), maxCLIOutputBytes)
	if err != nil {
		return nil, false
	}
	jobs, ok := cronJobsFromBytes(out)
	if !ok {
		return nil, false
	}
	crons := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		if entry := cronJobToMap(asObj(job), nil, loc); entry != nil {
			crons = append(crons, entry)
		}
	}
	return crons, true
}
