// Command wasm is Vremeplov's web frontend: a Galaksija on a canvas,
// with keyboard input, a local .gtp file picker (fast-load + RUN), and
// a reset button (SPEC.md §5.3).
//
// Build with tools/build-wasm.sh (GOOS=js GOARCH=wasm into web/) and
// serve the web/ directory with `go run ./tools/serve`. Everything that
// does not need syscall/js lives in app.go and is tested headlessly —
// on the host and, via tools/check-wasm.sh, under Node on js/wasm.
package main
