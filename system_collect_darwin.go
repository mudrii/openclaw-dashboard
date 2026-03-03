//go:build darwin

package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func collectCPU(ctx context.Context) SystemCPU {
	out, err := runWithTimeout(ctx, 3000, "top", "-l", "1", "-n", "0", "-s", "0")
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

func collectRAM(ctx context.Context) SystemRAM {
	// total bytes
	totalOut, err := runWithTimeout(ctx, 2000, "sysctl", "-n", "hw.memsize")
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
	vmOut, err := runWithTimeout(ctx, 2000, "vm_stat")
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
	out, err := runWithTimeout(ctx, 2000, "sysctl", "vm.swapusage")
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

// parseTopCPU parses "CPU usage: 5.26% user, 10.52% sys, 84.21% idle" → 100 - idle.
func parseTopCPU(output string) (float64, error) {
	re := regexp.MustCompile(`(\d+\.\d+)%\s+idle`)
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "CPU usage") {
			m := re.FindStringSubmatch(line)
			if m != nil {
				idle, err := strconv.ParseFloat(m[1], 64)
				if err != nil {
					return 0, err
				}
				return math.Round((100-idle)*10) / 10, nil
			}
		}
	}
	return 0, fmt.Errorf("CPU usage line not found in top output")
}

// parseVmStatUsed parses vm_stat output and returns used bytes (active+wired+compressed).
func parseVmStatUsed(output string) (int64, error) {
	pageSize := int64(4096)
	// extract page size from header: "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
	rePage := regexp.MustCompile(`page size of (\d+) bytes`)
	if m := rePage.FindStringSubmatch(output); m != nil {
		if ps, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			pageSize = ps
		}
	}

	getPages := func(label string) int64 {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(label) + `\s+(\d+)`)
		m := re.FindStringSubmatch(output)
		if m == nil {
			return 0
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		return v
	}

	active := getPages("Pages active:")
	wired := getPages("Pages wired down:")
	compressed := getPages("Pages occupied by compressor:")
	if active+wired+compressed == 0 {
		return 0, fmt.Errorf("could not parse vm_stat pages")
	}
	return (active + wired + compressed) * pageSize, nil
}

// parseSwapUsage parses "vm.swapusage: total = 4096.00M  used = 512.00M  free = 3584.00M"
func parseSwapUsage(output string) (used int64, total int64, err error) {
	re := regexp.MustCompile(`total\s*=\s*([\d.]+)([MGT])\s+used\s*=\s*([\d.]+)([MGT])`)
	m := re.FindStringSubmatch(output)
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
