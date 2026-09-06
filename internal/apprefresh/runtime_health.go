package apprefresh

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// perAgentStatus reports a collection assembled one agent at a time. Rows from
// the agents that answered are real, so a mixed run is partial and names the
// agents it is missing; only a total failure is unavailable. firstErr fixes the
// reported code so a later agent cannot mask the original cause.
func perAgentStatus(source string, firstErr error, failed []string, agents int) CollectionStatus {
	if firstErr == nil {
		return collectionStatus(source, nil, true)
	}
	if len(failed) >= agents {
		return collectionStatus(source, firstErr, false)
	}
	status := partialCollectionStatus(source, appopenclaw.ErrorCode(firstErr))
	status.FailedAgents = failed
	return status
}

func collectRuntimeHealth(ctx context.Context, client appopenclaw.Client, agents []string) (map[string]any, map[string]CollectionStatus) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data := map[string]any{}
	statuses := map[string]CollectionStatus{}
	memory := []map[string]any{}
	skills := []map[string]any{}
	var memoryErr, skillErr error
	var memoryFailed, skillFailed []string
	for _, agent := range agents {
		var m map[string]any
		err := client.Read(ctx, "doctor.memory.status", map[string]any{"agentId": agent}, &m)
		if err == nil && m["embedding"] == nil {
			err = fmt.Errorf("missing memory status")
		}
		if err != nil {
			if memoryErr == nil {
				memoryErr = err
			}
			memoryFailed = append(memoryFailed, agent)
		} else {
			row := projectFields(m, "agentId", "provider")
			row["embedding"] = projectFields(asObj(m["embedding"]), "ok", "checked")
			dream := asObj(m["dreaming"])
			row["dreaming"] = projectFields(dream, "enabled", "storageMode", "shortTermCount", "recallSignalCount", "groundedSignalCount", "promotedTotal", "promotedToday")
			phases := map[string]any{}
			for name, phase := range asObj(dream["phases"]) {
				phases[name] = projectFields(asObj(phase), "enabled", "cron", "managedCronPresent", "nextRunAtMs", "lastRunAtMs", "lastStatus")
			}
			asObj(row["dreaming"])["phases"] = phases
			memory = append(memory, row)
		}
		var s struct {
			Skills []map[string]any `json:"skills"`
		}
		err = client.Read(ctx, "skills.status", map[string]any{"agentId": agent}, &s)
		if err == nil && s.Skills == nil {
			err = fmt.Errorf("missing skills inventory")
		}
		if err != nil {
			if skillErr == nil {
				skillErr = err
			}
			skillFailed = append(skillFailed, agent)
		} else {
			for _, skill := range s.Skills {
				row := projectFields(skill, "name", "skillKey", "source", "eligible", "disabled", "modelVisible", "commandVisible", "platformIncompatible", "blockedByAllowlist", "blockedByAgentFilter")
				row["missing"] = projectFields(asObj(skill["missing"]), "bins", "anyBins", "env", "config", "os")
				row["agentId"] = agent
				skills = append(skills, row)
			}
		}
	}
	data["memory"], data["skillInventory"] = memory, skills
	statuses["memory"] = perAgentStatus("gateway.doctor.memory.status", memoryErr, memoryFailed, len(agents))
	statuses["skillInventory"] = perAgentStatus("gateway.skills.status", skillErr, skillFailed, len(agents))
	var channels struct {
		Accounts map[string][]map[string]any `json:"channelAccounts"`
	}
	err := client.Read(ctx, "channels.status", map[string]any{"probe": false}, &channels)
	if err == nil && channels.Accounts == nil {
		err = fmt.Errorf("missing channels inventory")
	}
	rows := []map[string]any{}
	for _, channel := range slices.Sorted(maps.Keys(channels.Accounts)) {
		for _, account := range channels.Accounts[channel] {
			row := projectFields(account, "accountId", "enabled", "configured", "running", "connected", "healthState", "lastInboundAt", "lastOutboundAt")
			row["channel"] = channel
			row["probe"] = projectFields(asObj(account["probe"]), "ok", "elapsedMs")
			row["hasError"] = jsonStr(account, "lastError") != ""
			rows = append(rows, row)
		}
	}
	data["channels"] = rows
	statuses["channels"] = collectionStatus("gateway.channels.status", err, err == nil)
	var status map[string]any
	err = client.Read(ctx, "status", map[string]any{}, &status)
	if err == nil && status["runtimeVersion"] == nil {
		err = fmt.Errorf("missing runtime status")
	}
	data["runtimeHealth"] = projectFields(status, "runtimeVersion", "processMemory", "eventLoop")
	asObj(data["runtimeHealth"])["taskAudit"] = projectFields(asObj(status["taskAudit"]), "total", "warnings", "errors", "byCode")
	for _, key := range []string{"degradedPlugins", "degradedSecretOwners"} {
		if entries, ok := status[key].([]any); ok {
			asObj(data["runtimeHealth"])[key+"Count"] = len(entries)
		}
	}
	statuses["runtimeHealth"] = collectionStatus("gateway.status", err, err == nil)
	// PID and uptime belong to the selected gateway, not the CLI process or host.
	// Decode only these fields: system.info also contains machine identity data.
	var info struct {
		PID      *int   `json:"pid"`
		UptimeMS *int64 `json:"uptimeMs"`
	}
	err = client.Read(ctx, "system.info", map[string]any{}, &info)
	if err == nil && (info.PID == nil || *info.PID <= 0 || info.UptimeMS == nil || *info.UptimeMS < 0) {
		err = fmt.Errorf("missing gateway process info")
	}
	if err == nil {
		data["runtimeInfo"] = map[string]any{"pid": *info.PID, "uptimeMs": *info.UptimeMS}
	}
	statuses["runtimeInfo"] = collectionStatus("gateway.system.info", err, err == nil)
	return data, statuses
}

func collectRuntimeInventories(ctx context.Context, client appopenclaw.Client) (map[string]any, map[string]CollectionStatus) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data := map[string]any{}
	statuses := map[string]CollectionStatus{}
	var plugins struct {
		Plugins []map[string]any `json:"plugins"`
	}
	err := client.ReadCommand(ctx, "plugins", &plugins)
	if err == nil && plugins.Plugins == nil {
		err = fmt.Errorf("missing plugins inventory")
	}
	rows := []map[string]any{}
	for _, plugin := range plugins.Plugins {
		rows = append(rows, projectFields(plugin, "id", "name", "version", "origin", "enabled", "status", "toolNames", "channelIds", "providerIds"))
	}
	data["pluginInventory"] = rows
	statuses["pluginInventory"] = collectionStatus("cli.plugins.list", err, err == nil)
	var indexes []map[string]any
	err = client.ReadCommand(ctx, "memoryIndex", &indexes)
	if err == nil && indexes == nil {
		err = fmt.Errorf("missing memory indexes")
	}
	rows = []map[string]any{}
	for _, index := range indexes {
		row := projectFields(index, "agentId")
		row["status"] = projectFields(asObj(index["status"]), "backend", "provider", "model", "files", "chunks", "dirty", "sources", "vector", "fts", "cache")
		rows = append(rows, row)
	}
	data["memoryIndex"] = rows
	statuses["memoryIndex"] = collectionStatus("cli.memory.status", err, err == nil)
	var diagnostics map[string]any
	err = client.ReadCommand(ctx, "diagnostics", &diagnostics)
	if err == nil && diagnostics["gateway"] == nil {
		err = fmt.Errorf("missing diagnostics")
	}
	backup := projectFields(asObj(diagnostics["backup"]), "enabled", "lastAttemptAt", "lastSuccessAt")
	for _, key := range []string{"lastAttempt", "lastSuccess", "latestAttempt", "latestSuccess"} {
		if entry := asObj(asObj(diagnostics["backup"])[key]); entry != nil {
			backup[key] = projectFields(entry, "at", "status", "startedAt", "completedAt")
		}
	}
	data["diagnostics"] = map[string]any{
		"backup": backup,
		"update": projectFields(asObj(diagnostics["update"]), "channel", "installKind", "packageManager"),
	}
	statuses["diagnostics"] = collectionStatus("cli.status", err, err == nil)
	return data, statuses
}
