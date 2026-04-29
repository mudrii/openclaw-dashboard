//go:build linux

package appsystem

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func collectCPU(ctx context.Context) SystemCPU {
	content1, err := os.ReadFile("/proc/stat")
	if err != nil {
		e := fmt.Sprintf("read /proc/stat: %v", err)
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}
	_, _, id1, tot1, err := parseProcStat(string(content1))
	if err != nil {
		e := err.Error()
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}

	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		e := "cpu sampling cancelled"
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}

	content2, err := os.ReadFile("/proc/stat")
	if err != nil {
		e := fmt.Sprintf("read /proc/stat second sample: %v", err)
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}
	_, _, id2, tot2, err := parseProcStat(string(content2))
	if err != nil {
		e := err.Error()
		return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
	}

	dTotal := tot2 - tot1
	dIdle := id2 - id1
	if dTotal == 0 {
		return SystemCPU{Cores: runtime.NumCPU()}
	}
	pct := math.Round(float64(dTotal-dIdle)/float64(dTotal)*1000) / 10
	return SystemCPU{Percent: pct, Cores: runtime.NumCPU()}
}

// collectMeminfo reads /proc/meminfo once and returns parsed map — shared by RAM and Swap.
func collectMeminfo() (map[string]uint64, error) {
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("read /proc/meminfo: %v", err)
	}
	return parseProcMeminfo(string(content))
}

// ramFromMeminfo builds SystemRAM from a pre-parsed /proc/meminfo map.
func ramFromMeminfo(info map[string]uint64) SystemRAM {
	totalKb := info["MemTotal"]
	// Fallback to MemFree for kernels < 3.14 (no MemAvailable)
	availKb, ok := info["MemAvailable"]
	if !ok {
		availKb = info["MemFree"]
	}
	usedKb := totalKb - availKb
	totalBytes := int64(totalKb * 1024)
	usedBytes := int64(usedKb * 1024)
	pct := 0.0
	if totalBytes > 0 {
		pct = math.Round(float64(usedBytes)/float64(totalBytes)*1000) / 10
	}
	return SystemRAM{UsedBytes: usedBytes, TotalBytes: totalBytes, Percent: pct}
}

// swapFromMeminfo builds SystemSwap from a pre-parsed /proc/meminfo map.
func swapFromMeminfo(info map[string]uint64) SystemSwap {
	totalKb := info["SwapTotal"]
	freeKb := info["SwapFree"]
	usedKb := totalKb - freeKb
	totalBytes := int64(totalKb * 1024)
	usedBytes := int64(usedKb * 1024)
	pct := 0.0
	if totalBytes > 0 {
		pct = math.Round(float64(usedBytes)/float64(totalBytes)*1000) / 10
	}
	return SystemSwap{UsedBytes: usedBytes, TotalBytes: totalBytes, Percent: pct}
}

// collectCPURAMSwapParallel runs all three Linux collectors concurrently.
// /proc/meminfo is read once and shared between RAM and Swap collectors.
func collectCPURAMSwapParallel(ctx context.Context) (SystemCPU, SystemRAM, SystemSwap) {
	// Read /proc/meminfo once — shared by RAM and Swap to avoid double I/O
	// and ensure both metrics come from the same kernel snapshot.
	info, meminfoErr := collectMeminfo()

	var cpu SystemCPU
	var ram SystemRAM
	var swap SystemSwap
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); cpu = collectCPU(ctx) }()
	go func() {
		defer wg.Done()
		if meminfoErr != nil {
			e := meminfoErr.Error()
			ram = SystemRAM{Error: &e}
			return
		}
		ram = ramFromMeminfo(info)
	}()
	go func() {
		defer wg.Done()
		if meminfoErr != nil {
			e := meminfoErr.Error()
			swap = SystemSwap{Error: &e}
			return
		}
		swap = swapFromMeminfo(info)
	}()
	wg.Wait()
	return cpu, ram, swap
}

// parseProcMeminfo parses /proc/meminfo and returns a map of key→kB values.
func parseProcMeminfo(content string) (map[string]uint64, error) {
	result := make(map[string]uint64)
	for _, line := range strings.Split(content, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) == 0 {
			continue
		}
		val, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		result[key] = val
	}
	if _, ok := result["MemTotal"]; !ok {
		return nil, fmt.Errorf("MemTotal not found in /proc/meminfo")
	}
	return result, nil
}

// parseProcStat parses the first line of /proc/stat and returns user, system, idle, total jiffies.
func parseProcStat(content string) (user, system, idle, total uint64, err error) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			return 0, 0, 0, 0, fmt.Errorf("unexpected /proc/stat format")
		}
		parse := func(s string) uint64 {
			v, _ := strconv.ParseUint(s, 10, 64)
			return v
		}
		userV := parse(fields[1])
		niceV := parse(fields[2])
		systemV := parse(fields[3])
		idleV := parse(fields[4])
		iowaitV := parse(fields[5])
		irqV := parse(fields[6])
		softirqV := parse(fields[7])
		// field[8] = steal (VM CPU stolen by hypervisor) — include for accuracy on VMs
		var stealV uint64
		if len(fields) > 8 {
			stealV = parse(fields[8])
		}
		totalV := userV + niceV + systemV + idleV + iowaitV + irqV + softirqV + stealV
		return userV, systemV, idleV, totalV, nil
	}
	return 0, 0, 0, 0, fmt.Errorf("cpu line not found in /proc/stat")
}
