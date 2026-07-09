package main

// System clipboard: paste types through core.TypeText (or onto the
// monitor REPL input), copy puts the decoded 32×16 screen text on the
// clipboard. atotto/clipboard is pure Go — syscalls on Windows (the
// cgo-free cross-build survives), pbcopy/pbpaste on macOS, xclip/xsel
// on Linux (a helpful status error when neither is installed).

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/mtrisic/vremeplov/core"
)

// clipper is the clipboard seam (faked in tests — the gate must never
// touch the host clipboard).
type clipper interface {
	read() (string, error)
	write(string) error
}

type systemClipboard struct{}

func (systemClipboard) read() (string, error) { return clipboard.ReadAll() }
func (systemClipboard) write(s string) error  { return clipboard.WriteAll(s) }

// shortcutHint is the platform's shortcut spelling for footer labels.
func shortcutHint(letter string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + letter
	}
	return "^" + letter
}

// pasteClipboard types the clipboard into the machine — or onto the
// monitor REPL input line (first line only) when the panel is open.
// TypeText validates before queueing, so a bad character rejects the
// whole paste.
func (g *Game) pasteClipboard() {
	text, err := g.clip.read()
	if err != nil {
		g.status = "clipboard: " + err.Error()
		return
	}
	if text == "" {
		g.status = "clipboard is empty"
		return
	}
	if g.mon.open {
		if i := strings.IndexAny(text, "\r\n"); i >= 0 {
			text = text[:i]
		}
		g.mon.typeRunes([]rune(text))
		return
	}
	end, err := g.m.TypeText(text)
	if err != nil {
		g.status = "paste: " + err.Error()
		return
	}
	g.status = fmt.Sprintf("pasting %d characters (done at t=%.1fs)",
		len([]rune(text)), float64(end)/core.CPUClockHz)
}

// copyScreen puts the decoded text screen on the system clipboard.
func (g *Game) copyScreen() {
	if err := g.clip.write(strings.Join(g.m.ScreenText(), "\n")); err != nil {
		g.status = "clipboard: " + err.Error()
		return
	}
	g.status = "screen text copied"
}
