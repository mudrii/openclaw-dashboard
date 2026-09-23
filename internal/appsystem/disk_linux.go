//go:build linux

package appsystem

import "syscall"

// statfsDiskStats converts linux statfs output. Block counts are in units of
// f_frsize (the fundamental block size); f_bsize is only the preferred I/O
// size. Kernels that leave f_frsize zero use f_bsize.
func statfsDiskStats(st *syscall.Statfs_t) diskStats {
	size := int64(st.Frsize)
	if size <= 0 {
		size = int64(st.Bsize)
	}
	return diskStats{Blocks: st.Blocks, Bfree: st.Bfree, Bavail: st.Bavail, BlockSize: size}
}
