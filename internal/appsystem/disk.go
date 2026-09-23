package appsystem

import (
	"context"
	"fmt"
	"math"
	"syscall"
)

// CollectDiskRoot reports usage of the filesystem containing path via
// syscall.Statfs (darwin and linux), computed the way df does.
func CollectDiskRoot(path string) SystemDisk {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		e := fmt.Sprintf("statfs %s: %v", path, err)
		return SystemDisk{Path: path, Error: &e}
	}
	return diskUsage(path, statfsDiskStats(&stat))
}

// diskStats holds the statfs block counts needed for df-style usage, with
// BlockSize already chosen per platform (f_frsize on linux, f_bsize on darwin).
type diskStats struct {
	Blocks, Bfree, Bavail uint64
	BlockSize             int64
}

// diskUsage computes usage like df: used = blocks - bfree, and the percentage
// is used / (used + bavail), so root-reserved blocks count as neither used
// nor available to the user.
func diskUsage(path string, st diskStats) SystemDisk {
	d := SystemDisk{Path: path, TotalBytes: int64(st.Blocks) * st.BlockSize}
	var usedBlocks uint64
	if st.Blocks > st.Bfree {
		usedBlocks = st.Blocks - st.Bfree
	}
	d.UsedBytes = int64(usedBlocks) * st.BlockSize
	if denom := usedBlocks + st.Bavail; usedBlocks > 0 && denom > 0 {
		d.Percent = math.Round(float64(usedBlocks)/float64(denom)*1000) / 10
	}
	return d
}

// collectDiskBounded runs the disk collector without letting a hung statfs
// (for example on a dead NFS mount) block the refresh past ctx. At most one
// statfs runs at a time: while a previous call is still pending, a new
// refresh reports an error immediately instead of stacking another goroutine.
func (s *SystemService) collectDiskBounded(ctx context.Context) SystemDisk {
	path := s.cfg.DiskPath
	if !s.diskInFlight.CompareAndSwap(false, true) {
		e := fmt.Sprintf("statfs %s: previous call still pending", path)
		return SystemDisk{Path: path, Error: &e}
	}
	result := make(chan SystemDisk, 1)
	go func() {
		defer s.diskInFlight.Store(false)
		result <- s.collectDisk(path)
	}()
	select {
	case d := <-result:
		return d
	case <-ctx.Done():
		e := fmt.Sprintf("statfs %s: %v", path, ctx.Err())
		return SystemDisk{Path: path, Error: &e}
	}
}
