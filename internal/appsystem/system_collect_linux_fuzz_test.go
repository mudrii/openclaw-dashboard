//go:build linux

package appsystem

import "testing"

// FuzzLinuxProcParsers checks the /proc/meminfo and /proc/stat parsers, and
// the RAM/swap derivations fed by them, never panic on arbitrary content.
// Counter values are kernel-trusted, so only crashes are asserted.
func FuzzLinuxProcParsers(f *testing.F) {
	for _, s := range []string{
		"MemTotal:       16384000 kB\nMemAvailable:    8192000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
		"MemTotal: notanumber kB\n",
		"cpu  100 0 50 1000 5 0 1 0 0 0\ncpu0 1 2 3 4 5 6 7\n",
		"cpu  1 2 3\n",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		if info, err := parseProcMeminfo(content); err == nil {
			_ = ramFromMeminfo(info)
			_ = swapFromMeminfo(info)
		}
		_, _, _, _, _ = parseProcStat(content)
	})
}
