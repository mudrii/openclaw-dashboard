package dashboard

import (
	"context"

	appchat "github.com/mudrii/openclaw-dashboard/internal/appchat"
	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	appopenclaw "github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	apprefresh "github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

// OpenclawTarget selects the OpenClaw runtime (native binary or container)
// that collection commands are executed against. It is the type of the
// Openclaw field on Config.
type OpenclawTarget = appopenclaw.Target

// OpenclawClient issues bounded read commands against the selected OpenClaw
// runtime.
type OpenclawClient = appopenclaw.Client

// CollectionStatus reports whether a collected payload is ready, partial, or
// unavailable, so callers never mistake missing runtime data for real values.
type CollectionStatus = apprefresh.CollectionStatus

// RuntimeLogs is a bounded page of OpenClaw runtime log entries with its
// cursor and truncation state.
type RuntimeLogs = apprefresh.RuntimeLogs

// ChatCapability describes whether the AI chat gateway is configured and
// usable, without performing an inference or auth probe.
type ChatCapability = appchat.Capability

// RedactOpenclawOutput removes common credential formats from operational
// text before it reaches a log line or an API payload.
func RedactOpenclawOutput(text string) string {
	return appopenclaw.Redact(text)
}

// OpenclawErrorCode classifies err into a stable collection error code,
// returning "ok" for a nil error.
func OpenclawErrorCode(err error) string {
	return appopenclaw.ErrorCode(err)
}

// ResolveGatewayToken resolves the AI gateway token from the process
// environment, the dotenv file at dotenvPath, or the OpenClaw config under
// statePath. It returns an empty string when no token is configured.
func ResolveGatewayToken(dotenvPath, statePath string) string {
	return appconfig.ResolveGatewayToken(dotenvPath, statePath)
}

// ReadRuntimeLogs reads up to limit log entries from the OpenClaw runtime
// reachable through client.
func ReadRuntimeLogs(ctx context.Context, client OpenclawClient, limit int) (RuntimeLogs, error) {
	return apprefresh.ReadRuntimeLogs(ctx, client, limit)
}
