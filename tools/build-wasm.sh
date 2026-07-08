#!/usr/bin/env bash
# Build the WASM frontend into web/: vremeplov.wasm plus the matching
# wasm_exec.js from the active Go toolchain.
set -euo pipefail
cd "$(dirname "$0")/.."

GOOS=js GOARCH=wasm go build -o web/vremeplov.wasm ./frontends/wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
cp assets/logo/favicon-16.png assets/logo/favicon-32.png assets/logo/icon-256.png web/
echo "built web/vremeplov.wasm ($(stat -c%s web/vremeplov.wasm 2>/dev/null || stat -f%z web/vremeplov.wasm) bytes)"
