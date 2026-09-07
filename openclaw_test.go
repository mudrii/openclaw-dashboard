package dashboard

import (
	"reflect"
	"strings"
	"testing"

	appchat "github.com/mudrii/openclaw-dashboard/internal/appchat"
	appopenclaw "github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	apprefresh "github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

// Config.Openclaw must stay reachable through the root facade alias.
var _ OpenclawTarget = Config{}.Openclaw

func TestOpenclawFacadeAliasesMatchInternalTypes(t *testing.T) {
	tests := []struct {
		name  string
		alias reflect.Type
		want  reflect.Type
	}{
		{"OpenclawTarget", reflect.TypeFor[OpenclawTarget](), reflect.TypeFor[appopenclaw.Target]()},
		{"OpenclawClient", reflect.TypeFor[OpenclawClient](), reflect.TypeFor[appopenclaw.Client]()},
		{"CollectionStatus", reflect.TypeFor[CollectionStatus](), reflect.TypeFor[apprefresh.CollectionStatus]()},
		{"RuntimeLogs", reflect.TypeFor[RuntimeLogs](), reflect.TypeFor[apprefresh.RuntimeLogs]()},
		{"ChatCapability", reflect.TypeFor[ChatCapability](), reflect.TypeFor[appchat.Capability]()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.alias != tc.want {
				t.Fatalf("alias type: got %v, want %v", tc.alias, tc.want)
			}
		})
	}
}

func TestOpenclawFacadeRedactsSecrets(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		secret string
	}{
		{"bearer header", "Authorization: Bearer abc123secretvalue", "abc123secretvalue"},
		{"labeled token", `{"api_key":"planted-secret-value"}`, "planted-secret-value"},
		{"api key literal", "used sk-plantedsecret0123 for the call", "sk-plantedsecret0123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactOpenclawOutput(tc.in)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("secret not redacted: got %q, want no %q", got, tc.secret)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("redaction marker missing: got %q", got)
			}
		})
	}
}

func TestOpenclawErrorCodeFacade(t *testing.T) {
	if got := OpenclawErrorCode(nil); got != "ok" {
		t.Fatalf("OpenclawErrorCode(nil): got %q, want %q", got, "ok")
	}
}

func TestOpenclawFacadeReadRuntimeLogsRejectsBadLimit(t *testing.T) {
	for _, limit := range []int{0, 1001} {
		logs, err := ReadRuntimeLogs(t.Context(), OpenclawClient{}, limit)
		if err == nil {
			t.Fatalf("ReadRuntimeLogs(limit=%d): got nil error, want error", limit)
		}
		if logs.Cursor != 0 || logs.Entries != nil {
			t.Fatalf("ReadRuntimeLogs(limit=%d): got %+v, want zero value", limit, logs)
		}
	}
}

func TestOpenclawFacadeResolveGatewayTokenPrefersEnv(t *testing.T) {
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "env-token")
	if got := ResolveGatewayToken(t.TempDir(), t.TempDir()); got != "env-token" {
		t.Fatalf("ResolveGatewayToken: got %q, want %q", got, "env-token")
	}
}
