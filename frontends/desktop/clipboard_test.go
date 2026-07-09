package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core"
)

// fakeClip stands in for the host clipboard — the gate must never
// touch the real one.
type fakeClip struct {
	text     string
	failRead bool
	failWrit bool
}

func (f *fakeClip) read() (string, error) {
	if f.failRead {
		return "", errors.New("no clipboard here")
	}
	return f.text, nil
}

func (f *fakeClip) write(s string) error {
	if f.failWrit {
		return errors.New("no clipboard here")
	}
	f.text = s
	return nil
}

// TestPasteClipboard: the paste action types a CRLF listing through
// TypeText and it runs.
func TestPasteClipboard(t *testing.T) {
	g := testGame(t)
	g.clip = &fakeClip{text: "10 PRINT 123\r\nRUN\r\n"}
	g.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY

	g.chromeAction("paste")
	if !strings.Contains(g.status, "pasting") {
		t.Fatalf("status = %q, want pasting feedback", g.status)
	}
	g.m.RunTstates(400 * core.TstatesPerFrame)
	if screen := strings.Join(g.m.ScreenText(), "\n"); !strings.Contains(screen, "123") {
		t.Fatalf("pasted listing did not run:\n%s", screen)
	}
}

// TestPasteIntoMonitorREPL: with the panel open, the first pasted line
// lands on the REPL input.
func TestPasteIntoMonitorREPL(t *testing.T) {
	g := testGame(t)
	g.clip = &fakeClip{text: "d 2800\nRUN"}
	g.toggleMonitor()
	g.chromeAction("paste")
	if got := string(g.mon.input); got != "d 2800" {
		t.Fatalf("REPL input = %q, want first pasted line", got)
	}
}

// TestPasteErrors: clipboard failures, empty clipboards, and
// unsupported characters all land in the status line and leave the
// machine untouched.
func TestPasteErrors(t *testing.T) {
	g := testGame(t)
	before := g.m.Tstates()

	g.clip = &fakeClip{failRead: true}
	g.chromeAction("paste")
	if !strings.Contains(g.status, "clipboard:") {
		t.Fatalf("read-failure status = %q", g.status)
	}
	g.clip = &fakeClip{}
	g.chromeAction("paste")
	if !strings.Contains(g.status, "empty") {
		t.Fatalf("empty-clipboard status = %q", g.status)
	}
	g.clip = &fakeClip{text: "PRINT π"}
	g.chromeAction("paste")
	if !strings.Contains(g.status, "paste:") {
		t.Fatalf("bad-character status = %q", g.status)
	}
	if g.m.Tstates() != before {
		t.Fatal("failed pastes advanced the machine")
	}
}

// TestCopyScreen: the copy action puts the decoded screen text on the
// clipboard; write failures report.
func TestCopyScreen(t *testing.T) {
	g := testGame(t)
	g.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY

	f := &fakeClip{}
	g.clip = f
	g.chromeAction("copy")
	if !strings.Contains(f.text, "READY") {
		t.Fatalf("clipboard missing screen text:\n%s", f.text)
	}
	if !strings.Contains(g.status, "copied") {
		t.Fatalf("status = %q", g.status)
	}

	g.clip = &fakeClip{failWrit: true}
	g.chromeAction("copy")
	if !strings.Contains(g.status, "clipboard:") {
		t.Fatalf("write-failure status = %q", g.status)
	}
}
