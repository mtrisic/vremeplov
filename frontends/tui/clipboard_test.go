package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
)

// TestPasteTypesIntoMachine: a bracketed paste (one KeyMsg with
// Paste=true) types a whole listing through TypeText and it runs.
func TestPasteTypesIntoMachine(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10 PRINT 123\r\nRUN\r\n"), Paste: true}
	mo.Update(msg)
	if !strings.Contains(mo.status, "pasting") {
		t.Fatalf("status = %q, want pasting feedback", mo.status)
	}
	mo.m.RunTstates(400 * core.TstatesPerFrame)
	if screen := strings.Join(mo.m.ScreenText(), "\n"); !strings.Contains(screen, "123") {
		t.Fatalf("pasted listing did not run:\n%s", screen)
	}
}

// TestPasteRejectsUnsupported: validation happens before queueing — a
// bad character rejects the whole paste and the machine is untouched.
func TestPasteRejectsUnsupported(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY
	before := mo.m.Tstates()
	mo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("PRINT π"), Paste: true})
	if !strings.Contains(mo.status, "paste:") {
		t.Fatalf("status = %q, want paste error", mo.status)
	}
	if mo.m.Tstates() != before {
		t.Fatal("rejected paste advanced the machine")
	}
	mo.m.RunTstates(50 * core.TstatesPerFrame)
	if screen := strings.Join(mo.m.ScreenText(), "\n"); strings.Contains(screen, "PRINT") {
		t.Fatalf("rejected paste still typed something:\n%s", screen)
	}
}

// TestPasteIntoMonitorREPL: with the panel open the paste lands on the
// input line — first line only, control runes dropped.
func TestPasteIntoMonitorREPL(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.mon.open = true
	mo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d 2800\nRUN"), Paste: true})
	if got := string(mo.mon.input); got != "d 2800" {
		t.Fatalf("REPL input = %q, want first pasted line", got)
	}
}

// TestCopyScreenOSC52: ^X y emits one OSC 52 sequence whose payload is
// the base64 screen text.
func TestCopyScreenOSC52(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.m.RunTstates(100 * core.TstatesPerFrame) // boot to READY
	var buf bytes.Buffer
	mo.clipOut = &buf

	mo.chromeAction("y")
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("not an OSC 52 sequence: %q", out)
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "READY") {
		t.Fatalf("clipboard payload missing screen text:\n%s", payload)
	}
	if !strings.Contains(mo.status, "copied") {
		t.Fatalf("status = %q", mo.status)
	}
}
