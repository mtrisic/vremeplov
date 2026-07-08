#!/usr/bin/env bash
# Gate 4 check: prove the core is js/wasm-portable and the web frontend
# builds and its logic passes under Node (mirrors gozilog's check).
set -euo pipefail
cd "$(dirname "$0")/.."

EXEC="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"
if ! command -v node >/dev/null; then
    echo "check-wasm: node not found (run inside the devcontainer)" >&2
    exit 1
fi

echo "== core tests under js/wasm (Node)"
(cd core && GOOS=js GOARCH=wasm go test -exec "$EXEC" ./...)

echo "== wasm frontend tests under js/wasm (Node)"
(cd frontends/wasm && GOOS=js GOARCH=wasm go test -exec "$EXEC" ./...)

echo "== wasm frontend build"
(cd frontends/wasm && GOOS=js GOARCH=wasm go build ./...)

echo "check-wasm: OK"
