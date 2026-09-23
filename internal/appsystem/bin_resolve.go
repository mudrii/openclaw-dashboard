package appsystem

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var versionishTokenRe = regexp.MustCompile(`[0-9]+|[A-Za-z]+`)

func versionishGreater(a, b string) bool {
	ta := versionishTokenRe.FindAllString(strings.ToLower(a), -1)
	tb := versionishTokenRe.FindAllString(strings.ToLower(b), -1)
	n := min(len(tb), len(ta))
	for i := range n {
		ai, aErr := strconv.Atoi(ta[i])
		bi, bErr := strconv.Atoi(tb[i])
		switch {
		case aErr == nil && bErr == nil:
			if ai != bi {
				return ai > bi
			}
		case aErr == nil:
			return true
		case bErr == nil:
			return false
		default:
			if ta[i] != tb[i] {
				return ta[i] > tb[i]
			}
		}
	}
	return len(ta) > len(tb)
}

// ResolveOpenclawBin finds the openclaw binary, checking PATH then known asdf locations.
// asdf shims may not be on the server's PATH when launched as a background process.
func ResolveOpenclawBin() string {
	if p, err := exec.LookPath("openclaw"); err == nil {
		return p
	}
	var candidates []string
	// asdf candidates live under the home dir; skip them when it cannot be
	// resolved rather than probing paths relative to the working directory.
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".asdf", "shims", "openclaw"))
		// Also probe asdf nodejs installs — sort newest-first using version-aware comparison.
		nodeDir := filepath.Join(home, ".asdf", "installs", "nodejs")
		if entries, err := os.ReadDir(nodeDir); err == nil {
			slices.SortFunc(entries, func(a, b os.DirEntry) int {
				if versionishGreater(a.Name(), b.Name()) {
					return -1
				}
				if versionishGreater(b.Name(), a.Name()) {
					return 1
				}
				return 0
			})
			for _, e := range entries {
				if e.IsDir() {
					candidates = append(candidates, filepath.Join(nodeDir, e.Name(), "bin", "openclaw"))
				}
			}
		}
	}
	candidates = append(candidates,
		"/usr/local/bin/openclaw",
		"/opt/homebrew/bin/openclaw",
	)
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return c
		}
	}
	return "openclaw" // last resort — may fail but gives a clear error
}
