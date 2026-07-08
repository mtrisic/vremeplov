// Command serve is a static file server for the web/ directory, for
// trying the WASM frontend locally:
//
//	bash tools/build-wasm.sh && go run ./tools/serve
//
// then open http://localhost:8080.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dir := flag.String("dir", "web", "directory to serve")
	flag.Parse()

	fmt.Printf("serving %s on http://localhost%s\n", *dir, *addr)
	if err := http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir))); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}
