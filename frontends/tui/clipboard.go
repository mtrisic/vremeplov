package main

// Clipboard, terminal style. Paste needs no OS clipboard access at
// all: bubbletea delivers bracketed paste as a single KeyMsg, and the
// text types through core.TypeText. Copy (^X y) emits the screen text
// as an OSC 52 escape sequence — the terminal (or multiplexer) puts it
// on the system clipboard, so it works over SSH; terminals without
// OSC 52 support (notably macOS Terminal.app) ignore it silently.

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/mtrisic/vremeplov/core"
)

// pasteText routes bracketed-paste text: into the monitor REPL when it
// is open, otherwise typed into the machine (validated before anything
// is queued — an unsupported character rejects the whole paste).
func (mo *model) pasteText(s string) {
	if mo.mon.open {
		// The REPL input is a single line: take the first.
		if i := strings.IndexAny(s, "\r\n"); i >= 0 {
			s = s[:i]
		}
		for _, r := range s {
			if r >= ' ' {
				mo.mon.input = append(mo.mon.input, r)
			}
		}
		return
	}
	end, err := mo.m.TypeText(s)
	if err != nil {
		mo.status = "paste: " + err.Error()
		return
	}
	mo.status = fmt.Sprintf("pasting %d characters (done at t=%.1fs)",
		len([]rune(s)), float64(end)/core.CPUClockHz)
}

// copyScreen puts the decoded 32×16 text screen on the system
// clipboard via OSC 52.
func (mo *model) copyScreen() {
	text := strings.Join(mo.m.ScreenText(), "\n")
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Fprintf(mo.clipOut, "\x1b]52;c;%s\x07", payload)
	mo.status = "screen text copied (needs OSC 52 terminal support)"
}
