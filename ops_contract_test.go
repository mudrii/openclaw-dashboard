package dashboard

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCIUsesCanonicalMakeTargets(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/tests.yml")
	for _, want := range []string{
		`uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6`,
		`uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6`,
		`run: make vet`,
		`run: make test`,
		`run: make build`,
		`run: make govulncheck`,
		`run: make staticcheck`,
		`sudo apt-get update && sudo apt-get install -y shellcheck`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("tests workflow missing %q", want)
		}
	}
	if strings.Contains(workflow, `run: go build`) {
		t.Fatal("tests workflow must not use raw go build; make build carries CGO and BuildVersion parity")
	}
	if strings.Contains(workflow, `run: govulncheck ./...`) {
		t.Fatal("tests workflow must not use raw govulncheck; make govulncheck carries pinned version parity")
	}
}

func TestWorkflowActionsAreImmutablePinned(t *testing.T) {
	shaRe := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, path := range []string{
		".github/workflows/tests.yml",
		".github/workflows/release.yml",
		".github/workflows/pr-validate.yml",
		".github/workflows/label-issues.yml",
	} {
		workflow := readTextFile(t, path)
		re := regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*([^@\s]+)@([^ #\s]+)`)
		for _, match := range re.FindAllStringSubmatch(workflow, -1) {
			ref := match[2]
			if !shaRe.MatchString(ref) {
				t.Fatalf("%s action %s must be pinned to a full commit SHA, got %q", path, match[1], ref)
			}
		}
	}
}

func TestPRTemplateUsesCanonicalValidationCommand(t *testing.T) {
	template := readTextFile(t, ".github/PULL_REQUEST_TEMPLATE.md")
	if !strings.Contains(template, "make check") {
		t.Fatal("PR template must ask contributors for make check evidence")
	}
	if strings.Contains(template, "go test -race") {
		t.Fatal("PR template must not advertise raw go test; make check is the repo validation surface")
	}
}

func TestPRValidationSkipsDependabotTemplateGate(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/pr-validate.yml")
	for _, want := range []string{
		`if: ${{ github.actor != 'dependabot[bot]' }}`,
		`uses: actions/github-script@373c709c69115d41ff229c7e5df9f8788daa9553 # v9`,
		`Expected exactly one checked PR type`,
		`What Changed`,
		`"## Checklist" section is missing`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("PR validation workflow missing %q", want)
		}
	}
}

func TestContributingDocsDescribeMakeCheckGate(t *testing.T) {
	makefile := readTextFile(t, "Makefile")
	contributing := readTextFile(t, "CONTRIBUTING.md")
	prereqs := makeTargetPrereqs(t, makefile, "check")
	for _, prereq := range []string{"vet", "lint", "test", "govulncheck", "staticcheck", "build"} {
		if !containsWord(prereqs, prereq) {
			t.Fatalf("Makefile check target missing prerequisite %q in %v", prereq, prereqs)
		}
	}
	for _, want := range []string{
		"`go vet ./...`",
		"`golangci-lint run ./...`",
		"`go test -race -count=1 ./...`",
		"`govulncheck`",
		"`staticcheck`",
		"`make build`",
	} {
		if !strings.Contains(contributing, want) {
			t.Fatalf("CONTRIBUTING.md make check docs missing %q", want)
		}
	}
	if strings.Contains(contributing, "Run `make staticcheck` separately") {
		t.Fatal("CONTRIBUTING.md must not describe staticcheck as separate from make check")
	}
}

func TestMakeBuildCarriesReleaseParityFlags(t *testing.T) {
	makefile := readTextFile(t, "Makefile")
	for _, want := range []string{
		"export CGO_ENABLED := 0",
		`go build -trimpath -ldflags="-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)"`,
		"-o $(BINARY) ./cmd/openclaw-dashboard",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile build contract missing %q", want)
		}
	}
}

func TestReleaseWorkflowContracts(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/release.yml")
	for _, want := range []string{
		`tags:`,
		`- "v*"`,
		`permissions:`,
		`contents: write`,
		`id-token: write`,
		`Verify release commit is on main`,
		`git merge-base --is-ancestor "$GITHUB_SHA" origin/main`,
		`Verify release tag matches VERSION`,
		`if [ "$version" != "$GITHUB_REF_NAME" ]; then`,
		`Install golangci-lint`,
		`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`,
		`run: make check`,
		`Install shellcheck`,
		`shellcheck --severity=warning assets/runtime/refresh.sh`,
		`uses: anchore/sbom-action/download-syft@e22c389904149dbc22b58101806040fa8d37a610 # v0.24.0`,
		`uses: sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6 # v4.1.2`,
		`uses: actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1 # v3.2.0`,
		`HOMEBREW_TAP_APP_CLIENT_ID`,
		`client-id: ${{ vars.HOMEBREW_TAP_APP_CLIENT_ID }}`,
		`repositories: homebrew-tap`,
		`permission-contents: write`,
		`uses: goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7`,
		`HOMEBREW_TAP_TOKEN: ${{ steps.tap-token.outputs.token }}`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow contract missing %q", want)
		}
	}
	if strings.Contains(workflow, "\n  attestations: write") {
		t.Fatal("release workflow must not grant attestations: write until provenance attestation is wired")
	}
	if strings.Contains(workflow, "HOMEBREW_TAP_APP_ID") {
		t.Fatal("release workflow must use GitHub App Client ID, not numeric App ID")
	}
}

func TestReleaseDocsUseCurrentHomebrewAppCredentialContract(t *testing.T) {
	homebrewDoc := readTextFile(t, "docs/HOMEBREW.md")
	for _, want := range []string{
		"HOMEBREW_TAP_APP_CLIENT_ID",
		"HOMEBREW_TAP_APP_PRIVATE_KEY",
		"GitHub App **Client ID**",
	} {
		if !strings.Contains(homebrewDoc, want) {
			t.Fatalf("Homebrew docs missing %q", want)
		}
	}
	if strings.Contains(homebrewDoc, "HOMEBREW_TAP_APP_ID") {
		t.Fatal("Homebrew docs must not document deprecated App ID credential")
	}
}

func TestInfraChecklistTracksRequiredChecks(t *testing.T) {
	infra := readTextFile(t, "docs/INFRA-CHECKLIST.md")
	for _, want := range []string{
		`{"context": "GolangCI-Lint"}`,
		`{"context": "Go test suite (ubuntu-latest)"}`,
		`{"context": "Go test suite (macos-latest)"}`,
		`{"context": "govulncheck"}`,
		`{"context": "Staticcheck"}`,
		`{"context": "Lint shell scripts"}`,
	} {
		if !strings.Contains(infra, want) {
			t.Fatalf("INFRA-CHECKLIST.md branch protection snippet missing %q", want)
		}
	}
}

func TestPackagingDocsMatchCurrentDockerBaseTags(t *testing.T) {
	dockerfile := readTextFile(t, "Dockerfile")
	infra := readTextFile(t, "docs/INFRA-CHECKLIST.md")
	for _, tag := range []string{"golang:1.27-alpine", "alpine:3.23"} {
		if !strings.Contains(dockerfile, tag) {
			t.Fatalf("Dockerfile missing %q", tag)
		}
		if !strings.Contains(infra, tag) {
			t.Fatalf("INFRA-CHECKLIST.md missing current Docker tag %q", tag)
		}
	}
	for _, digest := range []string{
		"golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125",
		"alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40",
	} {
		if !strings.Contains(dockerfile, digest) {
			t.Fatalf("Dockerfile missing pinned base image %q", digest)
		}
	}
	if strings.Contains(dockerfile, "ARG VERSION=dev") {
		t.Fatal("Dockerfile must not default release builds to dev-stamped binaries")
	}
	if !strings.Contains(dockerfile, `VERSION="$(tr -d '[:space:]' < VERSION)"`) {
		t.Fatal("Dockerfile must derive the default build version from VERSION")
	}
}

func TestNixDevShellAdvertisesMakeTargets(t *testing.T) {
	flake := readTextFile(t, "flake.nix")
	for _, want := range []string{"make build", "make test", "make check", "make govulncheck", "make lint"} {
		if !strings.Contains(flake, want) {
			t.Fatalf("flake.nix dev shell missing %q", want)
		}
	}
	if !strings.Contains(flake, `flags = [ "-trimpath" ];`) {
		t.Fatal("flake.nix build must use -trimpath like Makefile/Docker/GoReleaser")
	}
}

func TestGoReleaserReleaseContracts(t *testing.T) {
	config := readTextFile(t, ".goreleaser.yml")
	for _, want := range []string{
		`version: 2`,
		`CGO_ENABLED=0`,
		`- -trimpath`,
		`-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion={{ .Version }}`,
		`format: tar.gz`,
		`name_template: "{{ .ProjectName }}-{{ .Os }}-{{ .Arch }}"`,
		`name_template: checksums-sha256.txt`,
		`sboms:`,
		`- artifacts: archive`,
		`cmd: cosign`,
		`--bundle=${signature}`,
		`artifacts: checksum`,
		`token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"`,
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("GoReleaser contract missing %q", want)
		}
	}
	hooks := goReleaserBeforeHooks(t, config)
	if !containsString(hooks, "make test") {
		t.Fatalf("GoReleaser before.hooks = %v, want make test", hooks)
	}
	for _, hook := range hooks {
		if strings.Contains(hook, "go test") {
			t.Fatalf("GoReleaser before.hooks must delegate to make test, got raw hook %q", hook)
		}
	}
}

func TestInstallerFallbackBuildsResolvedTagSource(t *testing.T) {
	installer := readTextFile(t, "install.sh")
	for _, want := range []string{
		`source_archive="$REPO/archive/refs/tags/$LATEST_TAG.tar.gz"`,
		`source_archive="$REPO/archive/refs/heads/main.tar.gz"`,
		`build_version="dev"`,
		`curl -fsSL "$source_archive" | tar -xz --strip-components=1 -C "$INSTALL_DIR"`,
		`-trimpath`,
	} {
		if !strings.Contains(installer, want) {
			t.Fatalf("install.sh fallback build contract missing %q", want)
		}
	}
	if strings.Contains(installer, `archive/main.tar.gz`) {
		t.Fatal("install.sh must not build main while stamping a resolved release tag")
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

func makeTargetPrereqs(t *testing.T, makefile, target string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:\s*(.*)$`)
	match := re.FindStringSubmatch(makefile)
	if match == nil {
		t.Fatalf("Makefile target %q not found", target)
	}
	return strings.Fields(match[1])
}

func containsWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func goReleaserBeforeHooks(t *testing.T, config string) []string {
	t.Helper()
	lines := strings.Split(config, "\n")
	inBefore := false
	inHooks := false
	var hooks []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case line == "before:":
			inBefore = true
			inHooks = false
			continue
		case inBefore && !strings.HasPrefix(line, " ") && trimmed != "before:":
			inBefore = false
			inHooks = false
		case inBefore && trimmed == "hooks:":
			inHooks = true
			continue
		case inHooks && strings.HasPrefix(trimmed, "- "):
			hooks = append(hooks, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		case inHooks && trimmed != "hooks:" && !strings.HasPrefix(trimmed, "- "):
			inHooks = false
		}
	}
	if len(hooks) == 0 {
		t.Fatal("GoReleaser before.hooks not found")
	}
	return hooks
}
