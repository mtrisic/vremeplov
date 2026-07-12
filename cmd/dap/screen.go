package main

// The screen view: a tiny local HTTP page showing the live Galaksija
// display while an editor debugs — editor-agnostic by design (VS Code
// users, Helix users, anyone: the URL is announced as a DAP output
// event, open it in a browser). PNG frames come straight from core's
// deterministic FramePNG.

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const screenPage = `<!DOCTYPE html>
<meta charset="utf-8">
<title>Vremeplov — debug screen</title>
<style>
  body { background:#111; color:#ddd; font-family:monospace; text-align:center; padding:24px; }
  img  { width:512px; max-width:95vw; image-rendering:pixelated; background:#000; border:1px solid #333; }
</style>
<h3>Galaksija — debugger view</h3>
<img id="s" src="/screen.png">
<p>live while the machine runs · frozen at breakpoints</p>
<script>
  const img = document.getElementById("s");
  setInterval(() => { img.src = "/screen.png?t=" + Date.now(); }, 100);
</script>
`

type screenServer struct {
	srv *http.Server
	ln  net.Listener
}

// startScreen serves the view for eng on addr (host:port).
func startScreen(eng *engine, addr string) (*screenServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, screenPage)
	})
	mux.HandleFunc("/screen.png", func(w http.ResponseWriter, r *http.Request) {
		var png []byte
		var err error
		eng.do(func() { png, err = eng.m.FramePNG(true) })
		if err != nil || png == nil {
			http.Error(w, "no frame", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})
	mux.HandleFunc("/screen.txt", func(w http.ResponseWriter, r *http.Request) {
		var text string
		eng.do(func() { text = strings.Join(eng.m.ScreenText(), "\n") })
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, text)
	})
	s := &screenServer{srv: &http.Server{Handler: mux}, ln: ln}
	go s.srv.Serve(ln)
	return s, nil
}

func (s *screenServer) addr() string { return s.ln.Addr().String() }

func (s *screenServer) stop() {
	s.srv.Close()
	// Give the last in-flight handler a beat; Close doesn't wait.
	time.Sleep(10 * time.Millisecond)
}
