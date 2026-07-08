//go:build !js || !wasm

package main

import (
	"fmt"
	"os"
)

// The web frontend only makes sense in a browser; this stub exists so
// host builds of the whole workspace succeed and app.go's logic can be
// unit-tested natively.
func main() {
	fmt.Fprintln(os.Stderr, "vremeplov wasm frontend: build with GOOS=js GOARCH=wasm (see tools/build-wasm.sh)")
	os.Exit(2)
}
