package dashboard

import (
	"os"
	"strings"
	"testing"
)

func TestCIUsesCanonicalMakeTargets(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/tests.yml")
	for _, want := range []string{
		"run: make vet",
		"run: make test",
		"run: make build",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("tests workflow missing %q", want)
		}
	}
	if strings.Contains(workflow, `run: go build`) {
		t.Fatal("tests workflow must not use raw go build; make build carries CGO and BuildVersion parity")
	}
}

func TestMakeBuildCarriesReleaseParityFlags(t *testing.T) {
	makefile := readTextFile(t, "Makefile")
	for _, want := range []string{
		"export CGO_ENABLED := 0",
		`-ldflags="-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)"`,
		"-o $(BINARY) ./cmd/openclaw-dashboard",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile build contract missing %q", want)
		}
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
