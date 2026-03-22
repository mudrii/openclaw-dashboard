package dashboard

import (
	"os"

	appruntime "github.com/mudrii/openclaw-dashboard/internal/appruntime"
)

// resolveDashboardDir returns the writable dashboard runtime directory.
func resolveDashboardDir(dir string) string {
	return appruntime.ResolveDashboardDir(dir)
}

// resolveDashboardDirWithError returns the writable dashboard runtime directory.
func resolveDashboardDirWithError(dir string) (string, error) {
	return appruntime.ResolveDashboardDirWithError(dir)
}

// resolveRepoRoot is kept as a compatibility wrapper for existing tests/callers.
func resolveRepoRoot(dir string) string {
	return appruntime.ResolveRepoRoot(dir)
}

func seedHomebrewRuntimeDir(binDir string) (string, bool, error) {
	return appruntime.SeedHomebrewRuntimeDir(binDir)
}

func copyIfMissing(src, dst string, mode os.FileMode) error {
	return appruntime.CopyIfMissing(src, dst, mode)
}

func detectVersion(dir string) string {
	return appruntime.DetectVersion(dir)
}
