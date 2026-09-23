//go:build darwin

package appsystem

import "syscall"

// statfsDiskStats converts darwin statfs output; f_bsize is the unit of the
// block counts.
func statfsDiskStats(st *syscall.Statfs_t) diskStats {
	return diskStats{Blocks: st.Blocks, Bfree: st.Bfree, Bavail: st.Bavail, BlockSize: int64(st.Bsize)}
}
