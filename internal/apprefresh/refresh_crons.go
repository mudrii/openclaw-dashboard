package apprefresh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

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

// CollectCrons parses jobs.json (with optional jobs-state.json sidecar override
// for v2026.4.20+) and returns one entry per cron job describing schedule, last
// run, next run, and status.
func CollectCrons(cronPath string, loc *time.Location) []map[string]any {
	var crons []map[string]any
	data, err := os.ReadFile(cronPath)
	if err != nil {
		return crons
	}
	var cronFile map[string]any
	if err := json.Unmarshal(data, &cronFile); err != nil {
		return crons
	}
	jobs, ok := cronFile["jobs"].([]any)
	if !ok {
		return crons
	}

	// OpenClaw v2026.4.20+ moved runtime state to a sidecar file. When present, its
	// per-job entries take precedence over inline state (which is now {} in jobs.json).
	sidecarPath := filepath.Join(filepath.Dir(cronPath), "jobs-state.json")
	sidecarStates := loadCronStateSidecar(sidecarPath)

	for _, job := range jobs {
		jm := asObj(job)
		if jm == nil {
			continue
		}
		sched := asObj(jm["schedule"])
		kind := jsonStr(sched, "kind")
		var schedStr string
		switch kind {
		case "cron":
			schedStr = jsonStr(sched, "expr")
		case "every":
			ms, _ := sched["everyMs"].(float64)
			msInt := int64(ms)
			switch {
			case msInt >= 86400000:
				schedStr = fmt.Sprintf("Every %dd", msInt/86400000)
			case msInt >= 3600000:
				schedStr = fmt.Sprintf("Every %dh", msInt/3600000)
			case msInt >= 60000:
				schedStr = fmt.Sprintf("Every %dm", msInt/60000)
			default:
				schedStr = fmt.Sprintf("Every %dms", msInt)
			}
		case "at":
			at := jsonStr(sched, "at")
			if len(at) > 16 {
				at = at[:16]
			}
			schedStr = at
		default:
			b, _ := json.Marshal(sched)
			schedStr = string(b)
		}

		// Sidecar override contract (OpenClaw v2026.4.20+):
		// When jobs-state.json contains an entry for this job id, it is the
		// authoritative live state — we replace inline state wholesale rather
		// than field-merging. Rationale: in v2026.4.20+, OpenClaw stops writing
		// runtime state to jobs.json, so any inline fields present there are
		// pre-migration leftovers, not authoritative. A field-level merge could
		// surface stale lastRun/nextRun values from a long-superseded inline
		// snapshot. Inline state remains the fallback only when the sidecar is
		// absent entirely (loadCronStateSidecar returns nil) or has no entry
		// for this id (legacy jobs.json layout, sidecar missing this job).
		state := asObj(jm["state"])
		if id := jsonStr(jm, "id"); id != "" {
			if sc, ok := sidecarStates[id]; ok {
				state = sc
			}
		}
		// Sidecar exposes both lastStatus and lastRunStatus; prefer lastStatus for
		// continuity with the legacy schema, fall back to lastRunStatus.
		lastStatus := jsonStrDefault(state, "lastStatus", "")
		if lastStatus == "" {
			lastStatus = jsonStrDefault(state, "lastRunStatus", "none")
		}
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

		payload := asObj(jm["payload"])
		model := jsonStr(payload, "model")

		name := jsonStrDefault(jm, "name", "Unknown")

		crons = append(crons, map[string]any{
			"name":           name,
			"schedule":       schedStr,
			"enabled":        enabled,
			"lastRun":        lastRunStr,
			"lastStatus":     lastStatus,
			"lastDurationMs": int(durationMs),
			"nextRun":        nextRunStr,
			"model":          model,
		})
	}
	return crons
}
