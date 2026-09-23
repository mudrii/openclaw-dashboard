//go:build darwin

package appsystem

import (
	"math"
	"testing"
)

var darwinProbeSeeds = []string{
	"Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages active:  100.\nPages wired down: 50.\nPages occupied by compressor: 25.\n",
	"Pages active: 99999999999999999999.\n",
	"vm.swapusage: total = 4096.00M  used = 512.00M  free = 3584.00M  (encrypted)",
	"vm.swapusage: total = 1.5G  used = 2.0G",
	"CPU usage: 5.26% user, 10.52% sys, 84.21% idle\nCPU usage: 1% user, 0% sys, 100% idle",
	"CPU usage: 1.2.3% idle",
	"",
}

// FuzzDarwinProbeParsers checks the top, vm_stat and sysctl vm.swapusage
// parsers never panic on arbitrary output and keep their documented bounds.
func FuzzDarwinProbeParsers(f *testing.F) {
	for _, s := range darwinProbeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, out string) {
		if cpu, err := parseTopCPU(out); err == nil && math.IsNaN(cpu) {
			t.Fatalf("parseTopCPU(%q) = NaN", out)
		}
		_, _ = parseVmStatUsed(out) // page counts are OS-trusted; only panics matter
		if used, total, err := parseSwapUsage(out); err == nil && used > total {
			t.Fatalf("parseSwapUsage(%q) used %d > total %d", out, used, total)
		}
	})
}
