package appsystem

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// gatewayReasonHostProbeNotApplicable marks a gateway status that could not be
// determined because the only remaining probe would have measured the host
// rather than the selected container.
const gatewayReasonHostProbeNotApplicable = "host_probe_not_applicable"

// CollectVersionsLocal probes openclaw + gateway CLIs without performing any
// outbound network request. Latest-version lookup is handled asynchronously.
func CollectVersionsLocal(ctx context.Context, dashVer string, timeoutMs int, gatewayPort int, oclawBin string) SystemVersions {
	v := SystemVersions{Dashboard: dashVer}

	// OpenClaw version
	out, err := runOpenclawWithTimeout(ctx, timeoutMs, oclawBin, "--version")
	if err != nil {
		v.Openclaw = "unknown"
	} else {
		v.Openclaw = normalizeCLIVersion(out)
	}
	target := appopenclaw.TargetFromContext(ctx)
	v.Target = target.Effective()
	if target.IsContainer() {
		hostCtx := appopenclaw.WithTarget(ctx, appopenclaw.Target{Binary: target.Binary, Mode: "native"})
		if hostOut, hostErr := runOpenclawWithTimeout(hostCtx, timeoutMs, oclawBin, "--version"); hostErr == nil {
			v.HostOpenclaw = normalizeCLIVersion(hostOut)
		}
	}

	// Gateway status — use --json flag for reliable parsing.
	// I2 fix: attempt to parse stdout even on non-zero exit — many CLIs emit valid JSON
	// to stdout while exiting non-zero (e.g., gateway offline but status successfully queried).
	gw := SystemGateway{Status: "unknown"}
	gwOut, gwErr := runOpenclawWithTimeout(ctx, timeoutMs, oclawBin, "gateway", "status", "--json")
	if gwOut != "" {
		gw = ParseGatewayStatusJSON(ctx, gwOut)
	}
	if gw.Status == "unknown" {
		if target.IsContainer() {
			// DetectGatewayFallback probes 127.0.0.1, which describes the host,
			// not the container: an unpublished container port would report a
			// healthy gateway as offline. Report the gap instead.
			reason := gatewayReasonHostProbeNotApplicable
			if gwErr != nil {
				reason = appopenclaw.ErrorCode(gwErr)
			}
			gw.Error = &reason
		} else {
			// stdout had no usable JSON — fall back to HTTP probe
			gw = DetectGatewayFallback(ctx, gatewayPort, timeoutMs)
		}
	}
	v.Gateway = gw

	return v
}

// statusArgs builds the `openclaw status` argv. Deep status (--deep) adds the
// event-loop and last-heartbeat blocks but is slower, so it is opt-in via
// System.DeepStatus.
func statusArgs(deep bool) []string {
	args := []string{"status", "--json"}
	if deep {
		args = append(args, "--deep")
	}
	return args
}

// CollectOpenclawRuntime probes the gateway health endpoints and `openclaw
// status --json` in parallel. Parsed status output is used even when the CLI
// exits non-zero; failures are reported in SystemOpenclaw.Errors.
func CollectOpenclawRuntime(ctx context.Context, oclawBin string, timeoutMs int, gatewayPort int, versions SystemVersions, deepStatus bool) SystemOpenclaw {
	openclaw := SystemOpenclaw{
		Gateway: SystemOpenclawGateway{},
		Status: SystemOpenclawStatus{
			CurrentVersion: versions.Openclaw,
			LatestVersion:  versions.Latest,
		},
		Freshness: SystemOpenclawFreshness{},
	}
	stamp := func() string { return time.Now().UTC().Format(time.RFC3339) }

	var wg sync.WaitGroup
	var gw SystemOpenclawGateway
	var gwErrs []string
	var gwFresh string
	var status SystemOpenclawStatus
	var statusErr error
	var statusFresh string

	wg.Add(2)
	go func() {
		defer wg.Done()
		// The probe GETs 127.0.0.1, which describes the host gateway: for a
		// container target both its liveness and its connection-refused errors
		// would be about the wrong process, so report the gap instead.
		if appopenclaw.TargetFromContext(ctx).IsContainer() {
			gw.Reason = gatewayReasonHostProbeNotApplicable
			return
		}
		gw, gwErrs = probeOpenclawGatewayEndpoints(ctx, gatewayPort, timeoutMs)
		if len(gwErrs) == 0 {
			gwFresh = stamp()
		}
	}()
	go func() {
		defer wg.Done()
		out, err := runOpenclawWithTimeout(ctx, timeoutMs, oclawBin, statusArgs(deepStatus)...)
		// I2 fix: attempt to parse stdout even on non-zero exit — CLIs often emit valid JSON while
		// subprocess stdout is parsed regardless of returncode. Many CLIs emit valid JSON to
		// stdout while exiting non-zero (e.g., status reported but gateway connect failed).
		if out != "" {
			if parsed, parseErr := parseOpenclawStatusJSON(out, versions); parseErr == nil {
				status = parsed
				statusFresh = stamp()
			}
		}
		if err != nil {
			statusErr = fmt.Errorf("status --json: %w", err)
		}
	}()
	wg.Wait()

	openclaw.Gateway = gw
	if len(gwErrs) > 0 {
		openclaw.Errors = append(openclaw.Errors, gwErrs...)
	}
	if statusErr != nil {
		openclaw.Errors = append(openclaw.Errors, statusErr.Error())
	}
	// I2 fix: apply parsed status data regardless of error — stdout may have useful
	// data even on non-zero exit. statusFresh is non-empty only when parse succeeded.
	if statusFresh != "" {
		openclaw.Status = status
	}
	openclaw.Freshness = SystemOpenclawFreshness{
		Gateway: gwFresh,
		Status:  statusFresh,
	}

	if openclaw.Status.CurrentVersion == "" {
		openclaw.Status.CurrentVersion = versions.Openclaw
	}
	if openclaw.Status.LatestVersion == "" {
		openclaw.Status.LatestVersion = versions.Latest
	}

	return openclaw
}
