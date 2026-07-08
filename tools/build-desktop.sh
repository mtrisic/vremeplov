#!/usr/bin/env bash
# Build the desktop (Ebiten) frontend for release.
#
#   tools/build-desktop.sh [output-dir]    # default: dist/
#
# Environment:
#   VERSION   release version for filenames and --version
#             (default: git describe --tags --always --dirty)
#   TARGETS   space-separated os/arch pairs to build
#             (default: picked by host OS, see below)
#
# Unlike the TUI, the desktop frontend uses cgo on linux and macOS, so
# one host cannot build everything:
#   - a linux host builds its native arch (cgo needs a native gcc) and
#     windows/amd64 (Ebiten needs no cgo on Windows — a clean
#     cross-build)
#   - a macOS host builds darwin/arm64 and darwin/amd64 (clang
#     cross-compiles between the two Apple architectures)
# The release workflow runs both hosts. Non-native linux arches and
# windows/arm64 would need cross C toolchains — future TARGETS.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=${1:-dist}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
case "$(uname -s)" in
Darwin) DEFAULT_TARGETS="darwin/arm64 darwin/amd64" ;;
*) DEFAULT_TARGETS="linux/$(go env GOARCH) windows/amd64" ;;
esac
TARGETS=${TARGETS:-$DEFAULT_TARGETS}

mkdir -p "$OUT"
LDFLAGS="-s -w -X main.version=$VERSION"
built=()
for target in $TARGETS; do
    os=${target%/*}
    arch=${target#*/}
    bin="vremeplov-desktop_${VERSION}_${os}_${arch}"
    cgo=1
    [ "$os" = windows ] && bin="$bin.exe" && cgo=0
    echo "building $target -> $OUT/$bin"
    CGO_ENABLED=$cgo GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$bin" ./frontends/desktop
    built+=("$bin")
done

(
    cd "$OUT"
    if command -v sha256sum >/dev/null; then
        sha256sum "${built[@]}"
    else
        shasum -a 256 "${built[@]}"
    fi > "vremeplov-desktop_${VERSION}_$(uname -s | tr '[:upper:]' '[:lower:]')_SHA256SUMS"
)
echo "done: $OUT"
