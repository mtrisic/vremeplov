#!/usr/bin/env bash
# Cross-build the DAP debug adapter for release.
#
#   tools/build-dap.sh [output-dir]        # default: dist/
#
# Environment:
#   VERSION   release version for filenames and --version
#             (default: git describe --tags --always --dirty)
#   TARGETS   space-separated os/arch pairs to build
#             (default: linux/amd64 linux/arm64 darwin/amd64
#                       darwin/arm64 windows/386)
#
# The adapter is pure Go (like the TUI), so CGO_ENABLED=0
# cross-compiles every target from any host.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=${1:-dist}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
TARGETS=${TARGETS:-"linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/386"}

mkdir -p "$OUT"
LDFLAGS="-s -w -X main.version=$VERSION"
built=()
for target in $TARGETS; do
    os=${target%/*}
    arch=${target#*/}
    label=$os # filename label: friendlier "macOS" instead of GOOS "darwin"
    [ "$os" = darwin ] && label=macOS
    bin="vremeplov-dap_${VERSION}_${label}_${arch}"
    [ "$os" = windows ] && bin="$bin.exe"
    echo "building $target -> $OUT/$bin"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$bin" ./cmd/dap
    built+=("$bin")
done

(
    cd "$OUT"
    if command -v sha256sum >/dev/null; then
        sha256sum "${built[@]}"
    else
        shasum -a 256 "${built[@]}"
    fi > "vremeplov-dap_${VERSION}_SHA256SUMS"
)
echo "done: $OUT/vremeplov-dap_${VERSION}_SHA256SUMS"
