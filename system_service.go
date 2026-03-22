package dashboard

import (
	"context"
	"net/http"
	"time"

	appsystem "github.com/mudrii/openclaw-dashboard/internal/appsystem"
)

type SystemService = appsystem.SystemService

func NewSystemService(cfg SystemConfig, dashVer string, serverCtx context.Context) *SystemService {
	return appsystem.NewSystemService(cfg, dashVer, serverCtx)
}

func collectDiskRoot(path string) SystemDisk {
	return appsystem.CollectDiskRoot(path)
}

func collectVersionsLocal(ctx context.Context, dashVer string, timeoutMs int, gatewayPort int, oclawBin string) SystemVersions {
	return appsystem.CollectVersionsLocal(ctx, dashVer, timeoutMs, gatewayPort, oclawBin)
}

func collectOpenclawRuntime(ctx context.Context, oclawBin string, timeoutMs int, gatewayPort int, versions SystemVersions) SystemOpenclaw {
	return appsystem.CollectOpenclawRuntime(ctx, oclawBin, timeoutMs, gatewayPort, versions)
}

func parseGatewayStatusJSON(ctx context.Context, output string) SystemGateway {
	return appsystem.ParseGatewayStatusJSON(ctx, output)
}

func formatBytes(b int64) string {
	return appsystem.FormatBytes(b)
}

func getProcessInfo(ctx context.Context, pid int) (string, string) {
	return appsystem.GetProcessInfo(ctx, pid)
}

func detectGatewayFallback(ctx context.Context, gatewayPort int, timeoutMs int) SystemGateway {
	return appsystem.DetectGatewayFallback(ctx, gatewayPort, timeoutMs)
}

func resolveOpenclawBin() string {
	return appsystem.ResolveOpenclawBin()
}

func fetchLatestNpmVersion(ctx context.Context, timeoutMs int) string {
	return appsystem.FetchLatestNpmVersion(ctx, timeoutMs)
}

func fetchJSONMap(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
	return appsystem.FetchJSONMap(ctx, client, url)
}

func boolFromAny(v any) (bool, bool) {
	return appsystem.BoolFromAny(v)
}

func expireMetricsCacheForTest(s *SystemService) {
	if s != nil {
		s.SetMetricsTimestampForTest(time.Now().Add(-1 * time.Hour))
	}
}
