package apprefresh

import (
	"context"
	"strings"
	"time"
)

// gitLogFieldSep separates fields in collectGitLog's --format output.
const gitLogFieldSep = "\x1f"

// collectGitLog returns the last 5 commits from the openclaw repo as a
// hash/message/ago list. Best-effort: errors collapse to empty.
func collectGitLog(ctx context.Context, openclawPath string) []map[string]any {
	var gitLog []map[string]any
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := execCommandContext(ctx, "git", "-C", openclawPath, "log",
		"--oneline", "-5", "--format=%h%x1f%s%x1f%ar").Output()
	if err != nil {
		return gitLog
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		// Unit separator (0x1f) cannot appear in a subject line, unlike "|".
		if !strings.Contains(line, gitLogFieldSep) {
			continue
		}
		parts := strings.SplitN(line, gitLogFieldSep, 3)
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
