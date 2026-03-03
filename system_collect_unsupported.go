//go:build !darwin && !linux

package main

import (
	"context"
	"fmt"
	"runtime"
)

func collectCPU(ctx context.Context) SystemCPU {
	e := fmt.Sprintf("unsupported platform: %s", runtime.GOOS)
	return SystemCPU{Cores: runtime.NumCPU(), Error: &e}
}

func collectRAM(ctx context.Context) SystemRAM {
	e := fmt.Sprintf("unsupported platform: %s", runtime.GOOS)
	return SystemRAM{Error: &e}
}

func collectSwap(ctx context.Context) SystemSwap {
	e := fmt.Sprintf("unsupported platform: %s", runtime.GOOS)
	return SystemSwap{Error: &e}
}
