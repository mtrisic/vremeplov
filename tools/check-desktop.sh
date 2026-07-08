#!/usr/bin/env bash
# Desktop-frontend gate: vet + pure-logic tests + a native linux cgo
# build + a cgo-free windows cross-build. Ebiten initializes GLFW at
# package init on linux, so even pure-logic tests need a display —
# xvfb-run provides a virtual one; tests still must not touch the
# Ebiten runtime (NewImage/RunGame).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== desktop vet + tests"
(cd frontends/desktop && go vet ./... && xvfb-run -a go test -race ./...)

echo "== desktop linux build (cgo)"
(cd frontends/desktop && go build -o /tmp/vremeplov-desktop .)

echo "== desktop windows cross-build (no cgo)"
(cd frontends/desktop && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/vremeplov-desktop.exe .)

rm -f /tmp/vremeplov-desktop /tmp/vremeplov-desktop.exe
echo "check-desktop: OK"
