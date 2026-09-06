package dashboard

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseRequiresFrontendValidation(t *testing.T) {
	for _, tc := range []struct{ path, required string }{
		{"Makefile", "check: frontend-test "},
		{"Makefile", "\nfrontend-test:\n\tnode scripts/frontend-regression.cjs"},
		{".github/workflows/tests.yml", "run: make frontend-test"},
		{".github/workflows/release.yml", "run: make check"},
		{".goreleaser.yml", "- make frontend-test"},
		{".goreleaser.yml", "- docs/RUNTIME-COMPATIBILITY.md"},
	} {
		t.Run(tc.path+tc.required, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.required) {
				t.Errorf("%s missing required release gate %q", tc.path, tc.required)
			}
		})
	}
}
