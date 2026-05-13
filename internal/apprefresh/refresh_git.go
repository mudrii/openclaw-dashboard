package apprefresh

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// collectGitLog returns the last 5 commits from the openclaw repo as a
// hash/message/ago list. Best-effort: errors collapse to empty.
func collectGitLog(ctx context.Context, openclawPath string) []map[string]any {
	var gitLog []map[string]any
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", openclawPath, "log",
		"--oneline", "-5", "--format=%h|%s|%ar").Output()
	if err != nil {
		return gitLog
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		entry := map[string]any{"hash": parts[0], "message": parts[1]}
		if len(parts) > 2 {
			entry["ago"] = parts[2]
		} else {
			entry["ago"] = ""
		}
		gitLog = append(gitLog, entry)
	}
	return gitLog
}
