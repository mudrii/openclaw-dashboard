package dashboard

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCIUsesCanonicalMakeTargets(t *testing.T) {
	workflow := readTextFile(t, ".github/workflows/tests.yml")
	for _, want := range []string{
		`uses: actions/checkout@`,
		`uses: actions/setup-go@`,
		`run: make vet`,
		`run: make test`,
		`run: make build`,
		`run: make govulncheck`,
		`run: make staticcheck`,
		`run: make container-test`,
		`run: make workflow-test workflow-lint release-check`,
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
		`uses: actions/github-script@`,
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
		`actions: read`,
		`Require successful main CI for this exact commit`,
		`group: release-publication`,
		`Verify release commit is on main`,
		`git merge-base --is-ancestor "$GITHUB_SHA" origin/main`,
		`Verify release tag matches VERSION`,
		`if [ "$version" != "$GITHUB_REF_NAME" ]; then`,
		`Install golangci-lint`,
		`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@`,
		`run: make check`,
		`Install shellcheck`,
		`shellcheck --severity=warning assets/runtime/refresh.sh`,
		`uses: anchore/sbom-action/download-syft@`,
		`uses: sigstore/cosign-installer@`,
		`uses: actions/create-github-app-token@`,
		`HOMEBREW_TAP_APP_CLIENT_ID`,
		`client-id: ${{ vars.HOMEBREW_TAP_APP_CLIENT_ID }}`,
		`repositories: homebrew-tap`,
		`permission-contents: write`,
		`uses: goreleaser/goreleaser-action@`,
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

func TestReleaseAndCIToolVersionsAgree(t *testing.T) {
	ci := readTextFile(t, ".github/workflows/tests.yml")
	release := readTextFile(t, ".github/workflows/release.yml")
	for _, tc := range []struct{ name, ciPattern, releasePattern string }{
		{"Syft", `syft-version: (v[0-9]+\.[0-9]+\.[0-9]+)`, `syft-version: (v[0-9]+\.[0-9]+\.[0-9]+)`},
		{"golangci-lint", `version: (v[0-9]+\.[0-9]+\.[0-9]+)`, `golangci-lint@(v[0-9]+\.[0-9]+\.[0-9]+)`},
		{"GoReleaser", `version: "(v[0-9]+\.[0-9]+\.[0-9]+)"`, `version: "(v[0-9]+\.[0-9]+\.[0-9]+)"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left := regexp.MustCompile(tc.ciPattern).FindStringSubmatch(ci)
			right := regexp.MustCompile(tc.releasePattern).FindStringSubmatch(release)
			if len(left) != 2 || len(right) != 2 || left[1] != right[1] {
				t.Fatalf("%s CI/release pins differ or are missing: %v vs %v", tc.name, left, right)
			}
		})
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
		`{"context": "Container smoke"}`,
		`{"context": "Release rehearsal (ubuntu-latest)"}`,
		`{"context": "Release rehearsal (macos-latest)"}`,
		`{"context": "Nix flake (ubuntu-latest)"}`,
		`{"context": "Nix flake (macos-latest)"}`,
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
	for _, tag := range []string{"golang:1.27-alpine", "alpine:3.23"} {
		if !regexp.MustCompile(regexp.QuoteMeta(tag) + `@sha256:[0-9a-f]{64}(?:\s|$)`).MatchString(dockerfile) {
			t.Fatalf("Dockerfile base %q must be digest pinned", tag)
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
	return slices.Contains(words, want)
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
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
