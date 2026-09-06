package appchat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChatCapabilityDoesNotEnableDisabledEndpoint(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		body, token, state string
		available          bool
	}{
		{`{"gateway":{}}`, "token", "endpoint_disabled", false},
		{`{"gateway":{"http":{"endpoints":{"chatCompletions":{"enabled":true}}}}}`, "", "credentials_missing", false},
		{`{"gateway":{"http":{"endpoints":{"chatCompletions":{"enabled":true}}}}}`, "token", "configured", true},
	} {
		if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		got := CheckCapability(true, root, tc.token)
		if got.State != tc.state || got.Available != tc.available {
			t.Fatalf("capability=%+v", got)
		}
	}
}
