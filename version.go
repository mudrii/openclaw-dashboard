package dashboard

import (
	appruntime "github.com/mudrii/openclaw-dashboard/internal/appruntime"
)

func resolveDashboardDir(dir string) string {
	return appruntime.ResolveDashboardDir(dir)
}

func resolveDashboardDirWithError(dir string) (string, error) {
	return appruntime.ResolveDashboardDirWithError(dir)
}

func resolveRepoRoot(dir string) string {
	return appruntime.ResolveRepoRoot(dir)
}

func detectVersion(dir string) string {
	return appruntime.DetectVersion(dir)
}
