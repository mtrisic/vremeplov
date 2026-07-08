package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
)

// TestRewindChrome: ^X b steps the machine back in time.
func TestRewindChrome(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.m.RunTstates(150 * core.TstatesPerFrame)
	before := mo.m.Tstates()

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('b'))
	if mo.m.Tstates() >= before {
		t.Fatalf("^X b did not rewind (T=%d, was %d)", mo.m.Tstates(), before)
	}
	if !strings.Contains(mo.status, "rewound") {
		t.Fatalf("status = %q", mo.status)
	}
}
