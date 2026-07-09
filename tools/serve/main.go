// Command serve is a static file server for the web/ directory, for
// trying the WASM frontend locally:
//
//	bash tools/build-wasm.sh && go run ./tools/serve
//
// then open http://localhost:8080. The repo's examples/ directory is
// mounted at /examples/ so the page's built-in example tapes resolve
// the same way they do on the Pages site (which copies them next to
// the emulator).
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
	examples := flag.String("examples", "examples", "directory served at /examples/")
	flag.Parse()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(*dir)))
	mux.Handle("/examples/", http.StripPrefix("/examples/",
		http.FileServer(http.Dir(*examples))))

	fmt.Printf("serving %s (+%s at /examples/) on http://localhost%s\n", *dir, *examples, *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}
