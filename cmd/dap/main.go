// Command dap is vremeplov's Debug Adapter Protocol server: it hosts a
// Galaksija machine and lets any DAP-speaking editor (VS Code, Helix,
// nvim-dap, …) debug the Z80 program running inside it — breakpoints,
// stepping (including BACKWARDS, on the rewind history), registers,
// memory, disassembly, and the monitor REPL on the debug console.
//
// Transport: DAP over stdio by default (what most editors spawn), or a
// single TCP session with --listen. The debuggee is loaded per the
// launch request: a .gtp/.wav tape image, or a raw .bin with an org —
// optionally with a sjasmplus SLD file for source-level debugging.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
)

// version is stamped by tools/build-dap.sh (-ldflags -X main.version=…).
var version = "dev"

func main() {
	if err := run(); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, "vremeplov-dap:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		listen      = flag.String("listen", "", "serve one DAP session on this TCP address instead of stdio (e.g. 127.0.0.1:4711)")
		screen      = flag.String("screen", "", "serve a live screen view on this HTTP address (e.g. 127.0.0.1:8390); the launch request's \"screen\" argument wins")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println("vremeplov-dap", version)
		return nil
	}

	if *listen == "" {
		return serve(stdio{}, *screen)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Fprintf(os.Stderr, "vremeplov-dap: listening on %s\n", ln.Addr())
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	return serve(conn, *screen)
}

// stdio adapts the process's standard streams to the one io.ReadWriter
// the server drives. DAP owns stdout completely — anything human goes
// to stderr or DAP output events.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// serve runs one DAP session over rw until the client disconnects.
func serve(rw io.ReadWriter, screenAddr string) error {
	s := newServer(rw, screenAddr)
	return s.loop(bufio.NewReader(rw))
}
