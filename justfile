set shell := ["bash", "-euo", "pipefail", "-c"]

# Dev tools are pinned in tools/go.mod, outside the root go.mod, so the library
# keeps zero dependencies for its importers.
TOOL := "go tool -modfile=tools/go.mod"

# Default recipe to list all recipes
default:
    @just --list

# Build the CLI into bin/
build:
    go build -trimpath -o bin/holes ./cmd/holes

# Cross-compile the CLI into dist/ for the released platforms, plus
# SHA256SUMS. Other platforms can build from source with go install. The
# binaries report the version of the tag on HEAD when the tree is clean, else
# a pseudo-version.
dist:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf dist
    mkdir dist
    for p in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64 freebsd/arm64; do
      os="${p%/*}"
      arch="${p#*/}"
      ext=""

      if [[ $os == windows ]]; then
        ext=.exe
      fi

      # GOARM only affects arm: ARMv6 runs on every Raspberry Pi, including
      # the Zero and 1 that the default ARMv7 excludes.
      CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM=6 go build -trimpath -o "dist/holes-$os-$arch$ext" ./cmd/holes
    done
    cd dist && sha256sum holes-* > SHA256SUMS

# Run unit tests with the race detector
test:
    go test -race -count=1 ./...

# Run each fuzz target in each package for FUZZTIME (default 30s). Listing a
# package's targets is a separate statement so a build failure, or a package
# with no targets, fails the recipe.
fuzz FUZZTIME="30s":
    for pkg in . ./cmd/holes; do targets="$(go test -list '^Fuzz' "$pkg" | grep '^Fuzz')"; for t in $targets; do go test -run='^$' -fuzz="^${t}\$" -fuzztime={{FUZZTIME}} "$pkg"; done; done

# Tidy go.mod and go.sum, in the root and tools modules
tidy:
    go mod tidy
    cd tools && go mod tidy

# Update dependencies to their latest minor/patch versions, then tidy. The
# root module has none today; the dev tools in tools/go.mod are updated too.
# The toolchain line, which dev and CI build with, moves to the latest Go; the
# go line, the minimum Go for importers, stays put.
# Each dev tool moves to its latest release without -u, so its dependencies
# stay at the versions the tool itself requires: -u would take them past what
# the tool was tested with (for example a newer pre-release of a library).
modupdate:
    go get -u -t ./... toolchain@latest
    go mod tidy
    cd tools && go get $(go list -f '{{"{{"}}.Module.Path{{"}}"}}@latest' tool | sort -u) && go mod tidy

# Format code and apply go fix modernizers
fmt:
    go fix ./...
    gofmt -w .

# Lint (golangci-lint v2 with gosec)
lint:
    golangci-lint run

# Scan for known vulnerabilities
vuln:
    {{TOOL}} govulncheck ./...

# Scan for committed secrets
secrets:
    {{TOOL}} gitleaks dir . --no-banner

# Lint the GitHub Actions workflows
actionlint:
    {{TOOL}} actionlint

# Static checks: tidy go.mod (root and tools), no root dependencies, vet,
# formatting, go fix modernizers
verify:
    go mod tidy -diff
    test "$(go list -m all)" = github.com/rgravlin/holes
    cd tools && go mod tidy -diff
    go vet ./...
    test -z "$(gofmt -l .)"
    test -z "$(go fix -diff ./...)"

# Everything CI runs except golangci-lint, which CI runs through its action
ci: verify vuln secrets actionlint test (fuzz "10s")

# Everything CI runs, plus lint
check: lint ci
