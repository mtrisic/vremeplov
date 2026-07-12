#!/usr/bin/env bash
# Package platform-specific VS Code extensions with the vremeplov-dap
# binary bundled — installing the extension is all a user needs.
#
#   tools/build-vsix.sh [output-dir]       # default: dist/
#
# Environment:
#   VERSION   release version; when it looks like vX.Y.Z the VSIX is
#             stamped X.Y.Z (package.json untouched), otherwise the
#             package.json version is used.
#
# One VSIX per VS Code platform target, each carrying the matching
# pure-Go adapter binary in bin/ (extension.js prefers it over PATH).
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=${1:-dist}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
EXT=editors/vscode

# VS Code target ↔ GOOS/GOARCH pairs.
TARGETS="
win32-x64:windows/amd64
win32-arm64:windows/arm64
linux-x64:linux/amd64
linux-arm64:linux/arm64
darwin-x64:darwin/amd64
darwin-arm64:darwin/arm64
"

VSIX_VERSION=""
if [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    VSIX_VERSION=${VERSION#v}
fi

mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd) # absolute: vsce runs from the extension dir
LDFLAGS="-s -w -X main.version=$VERSION"
trap 'rm -rf "$EXT/bin"' EXIT
for pair in $TARGETS; do
    vst=${pair%%:*}
    target=${pair#*:}
    os=${target%/*}
    arch=${target#*/}
    rm -rf "$EXT/bin" && mkdir -p "$EXT/bin"
    bin="vremeplov-dap"
    [ "$os" = windows ] && bin="$bin.exe"
    echo "building $target -> $vst"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$EXT/bin/$bin" ./cmd/dap
    (
        cd "$EXT"
        args=(--target "$vst" -o "$OUT/vremeplov-galaksija-debug_${VERSION}_${vst}.vsix")
        if [ -n "$VSIX_VERSION" ]; then
            args+=("$VSIX_VERSION" --no-update-package-json)
        fi
        npx --yes @vscode/vsce package "${args[@]}"
    )
done
echo "done: $OUT/*.vsix"
