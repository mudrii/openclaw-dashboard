//go:build darwin

package appsystem

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Pre-compiled regexes — compiled once at startup, not per-call.
var (
	reTopIdle    = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s+idle`) // P1-3: handles integer idle (e.g. "100% idle")
	reVmPageSize = regexp.MustCompile(`page size of (\d+) bytes`)
	reSwapUsage  = regexp.MustCompile(`total\s*=\s*([\d.]+)([MGT])\s+used\s*=\s*([\d.]+)([MGT])`)
)

func reVmPages(label string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(label) + `\s+(\d+)`)
}

// Pre-compile vm_stat page regexes once.
var (
	reVmActive     = reVmPages("Pages active:")
	reVmWired      = reVmPages("Pages wired down:")
	reVmCompressor = reVmPages("Pages occupied by compressor:")
)

func collectCPU(ctx context.Context) SystemCPU {
	// Use -l 2 for accuracy: first sample is boot average; second is current delta.
	out, err := runWithTimeout(ctx, 4000, "/usr/bin/top", "-l", "2", "-n", "0", "-s", "1")
	if err != nil {
		e := fmt.Sprintf("top failed: %v", err)
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}
	pct, parseErr := parseTopCPU(out)
	if parseErr != nil {
		e := parseErr.Error()
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}
	return SystemCPU{Percent: pct, Cores: runtime.NumCPU()}
}

// collectRAM and collectSwap are independent — run them in parallel.
func collectRAM(ctx context.Context) SystemRAM {
	// total bytes
	totalOut, err := runWithTimeout(ctx, 2000, "/usr/sbin/sysctl", "-n", "hw.memsize")
	if err != nil {
		e := fmt.Sprintf("sysctl hw.memsize failed: %v", err)
		return SystemRAM{Error: &e}
	}
	totalBytes, err := strconv.ParseInt(strings.TrimSpace(totalOut), 10, 64)
	if err != nil {
		e := fmt.Sprintf("parse hw.memsize: %v", err)
		return SystemRAM{Error: &e}
	}

	// page stats
	vmOut, err := runWithTimeout(ctx, 2000, "/usr/bin/vm_stat")
	if err != nil {
		e := fmt.Sprintf("vm_stat failed: %v", err)
		return SystemRAM{TotalBytes: totalBytes, Error: &e}
	}
	used, parseErr := parseVmStatUsed(vmOut)
	if parseErr != nil {
		e := parseErr.Error()
		return SystemRAM{TotalBytes: totalBytes, Error: &e}
	}
	pct := 0.0
	if totalBytes > 0 {
		pct = math.Round(float64(used)/float64(totalBytes)*1000) / 10
	}
	return SystemRAM{UsedBytes: used, TotalBytes: totalBytes, Percent: pct}
}

func collectSwap(ctx context.Context) SystemSwap {
	out, err := runWithTimeout(ctx, 2000, "/usr/sbin/sysctl", "vm.swapusage")
	if err != nil {
		e := fmt.Sprintf("sysctl vm.swapusage failed: %v", err)
		return SystemSwap{Error: &e}
	}
	used, total, parseErr := parseSwapUsage(out)
	if parseErr != nil {
		e := parseErr.Error()
		return SystemSwap{Error: &e}
	}
	pct := 0.0
	if total > 0 {
		pct = math.Round(float64(used)/float64(total)*1000) / 10
	}
	return SystemSwap{UsedBytes: used, TotalBytes: total, Percent: pct}
}

// collectCPURAMSwapParallel runs all three Darwin collectors concurrently.
// Called by system_service.go refresh() instead of sequential calls.
func collectCPURAMSwapParallel(ctx context.Context) (SystemCPU, SystemRAM, SystemSwap) {
	var cpu SystemCPU
	var ram SystemRAM
	var swap SystemSwap
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); cpu = collectCPU(ctx) }()
	go func() { defer wg.Done(); ram = collectRAM(ctx) }()
	go func() { defer wg.Done(); swap = collectSwap(ctx) }()
	wg.Wait()
	return cpu, ram, swap
}

// parseTopCPU parses the LAST "CPU usage:" line from `top -l 2` output.
// The last sample is the current-window delta (accurate); first is boot average.
// P1-3: regex handles both "84.21% idle" and "100% idle".
func parseTopCPU(output string) (float64, error) {
	var lastMatch string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "CPU usage") {
			if reTopIdle.MatchString(line) {
				lastMatch = line
			}
		}
	}
	if lastMatch == "" {
		return 0, fmt.Errorf("CPU usage line not found in top output")
	}
	m := reTopIdle.FindStringSubmatch(lastMatch)
	idle, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	return math.Round((100-idle)*10) / 10, nil
}

// parseVmStatUsed parses vm_stat output and returns used bytes (active+wired+compressed).
func parseVmStatUsed(output string) (int64, error) {
	pageSize := int64(4096)
	if m := reVmPageSize.FindStringSubmatch(output); m != nil {
		if ps, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			pageSize = ps
		}
	}
	getPages := func(re *regexp.Regexp) int64 {
		m := re.FindStringSubmatch(output)
		if m == nil {
			return 0
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		return v
	}
	active := getPages(reVmActive)
	wired := getPages(reVmWired)
	compressed := getPages(reVmCompressor)
	if active+wired+compressed == 0 {
		return 0, fmt.Errorf("could not parse vm_stat pages")
	}
	return (active + wired + compressed) * pageSize, nil
}

// parseSwapUsage parses "vm.swapusage: total = 4096.00M  used = 512.00M  free = 3584.00M"
func parseSwapUsage(output string) (used int64, total int64, err error) {
	m := reSwapUsage.FindStringSubmatch(output)
	if m == nil {
		return 0, 0, fmt.Errorf("could not parse vm.swapusage: %q", output)
	}
	toBytes := func(val, unit string) int64 {
		v, _ := strconv.ParseFloat(val, 64)
		switch unit {
		case "G":
			return int64(v * 1024 * 1024 * 1024)
		case "T":
			return int64(v * 1024 * 1024 * 1024 * 1024)
		default: // M
			return int64(v * 1024 * 1024)
		}
	}
	total = toBytes(m[1], m[2])
	used = toBytes(m[3], m[4])
	return used, total, nil
}
