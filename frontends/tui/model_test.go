package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
)

func testMachine(t *testing.T) *core.Machine {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.work not found")
		}
		dir = parent
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, "roms", "bin", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	m, err := core.New(core.Config{
		ROMA:    read("rom_a_v28.bin"),
		Chargen: read("chrgen_elektronika.bin"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func doTick(mo *model) {
	mo.advance(mo.lastTick.Add(framePeriod))
}

func TestModelKeyPressAndHoldExpiry(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)

	mo.Update(runeMsg('+')) // Shift + ;
	before := mo.m.Tstates()
	if got := mo.m.MemRead(0x2000 + uint16(core.KeySemicolon)); got&1 != 0 {
		t.Fatal("semicolon key not pressed after '+'")
	}
	if got := mo.m.MemRead(0x2000 + uint16(core.KeyShift)); got&1 != 0 {
		t.Fatal("shift not pressed after '+'")
	}
	// Run past the hold window: both keys must auto-release.
	for i := 0; i < holdFrames+2; i++ {
		doTick(mo)
	}
	if mo.m.Tstates() <= before {
		t.Fatal("machine did not advance on ticks")
	}
	if got := mo.m.MemRead(0x2000 + uint16(core.KeySemicolon)); got&1 != 1 {
		t.Fatal("semicolon key still pressed after hold window")
	}
	if len(mo.holds) != 0 {
		t.Fatalf("holds not empty: %v", mo.holds)
	}
}

func TestModelStickyMode(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('s')) // sticky on
	if !mo.sticky {
		t.Fatal("sticky not enabled")
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyUp})
	for i := 0; i < holdFrames+2; i++ {
		doTick(mo)
	}
	if got := mo.m.MemRead(0x2000 + uint16(core.KeyUp)); got&1 != 0 {
		t.Fatal("sticky key released by hold expiry")
	}
	// Next key releases the previous one.
	mo.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := mo.m.MemRead(0x2000 + uint16(core.KeyUp)); got&1 != 1 {
		t.Fatal("previous sticky key not released on new press")
	}
	if got := mo.m.MemRead(0x2000 + uint16(core.KeyDown)); got&1 != 0 {
		t.Fatal("new sticky key not pressed")
	}
}

func TestModelPauseAndChrome(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if !mo.prefix {
		t.Fatal("Ctrl+X did not arm the prefix")
	}
	mo.Update(runeMsg('p'))
	if !mo.paused {
		t.Fatal("^X p did not pause")
	}
	before := mo.m.Tstates()
	doTick(mo)
	if mo.m.Tstates() != before {
		t.Fatal("machine advanced while paused")
	}
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('p'))
	doTick(mo)
	if mo.m.Tstates() == before {
		t.Fatal("machine did not resume")
	}
	// 'p' with no prefix must type the letter P, not toggle pause.
	mo.Update(runeMsg('p'))
	if mo.paused {
		t.Fatal("bare 'p' toggled pause")
	}
	if got := mo.m.MemRead(0x2000 + uint16(core.KeyP)); got&1 != 0 {
		t.Fatal("bare 'p' did not press the P key")
	}
}

func TestModelMemoryDump(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('d'))
	if !strings.Contains(mo.status, "dumped") {
		t.Fatalf("dump status: %q", mo.status)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one dump file, got %v (%v)", entries, err)
	}
	info, _ := entries[0].Info()
	if info.Size() != 0x10000 {
		t.Fatalf("dump size %d, want 65536", info.Size())
	}
}

// TestModelViewSmallTerminal covers the VSCode-panel case that motivated
// the text/scaled fallbacks: a 90×15 terminal must auto-pick text mode,
// crop from the BOTTOM (bubbletea discards the top of oversized views,
// where the READY prompt lives), and say so in the status line.
func TestModelViewSmallTerminal(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	mo.Update(tea.WindowSizeMsg{Width: 90, Height: 15})
	if mo.renderer != rendererText {
		t.Fatalf("renderer = %s, want text for 90x15", mo.renderer)
	}
	view := mo.View()
	lines := strings.Split(view, "\n")
	// 14 text rows (of 16, bottom two cropped) + status line.
	if len(lines) != 15 {
		t.Fatalf("view has %d lines, want 15", len(lines))
	}
	if !strings.Contains(lines[14], "cropped 14/16") {
		t.Fatalf("status missing crop hint: %q", lines[14])
	}
	// Manual switch to scaled braille must fit without cropping.
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('v')) // text -> scaled-braille
	if mo.renderer != rendererScaled {
		t.Fatalf("renderer after ^X v = %s, want scaled-braille", mo.renderer)
	}
	lines = strings.Split(mo.View(), "\n")
	if len(lines) != 15 {
		t.Fatalf("scaled view has %d lines, want 15 (14 braille rows + status)", len(lines))
	}
	if strings.Contains(lines[14], "cropped") {
		t.Fatalf("scaled mode should not crop: %q", lines[14])
	}
}

func TestModelViewRenders(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	mo.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	if mo.renderer != rendererBraille {
		t.Fatalf("renderer = %s, want braille for 200x60", mo.renderer)
	}
	view := mo.View()
	lines := strings.Split(view, "\n")
	// 208 px / 4 px per row = 52 pixel rows + status line + two wrapped
	// rows of bordered footer buttons, three lines each (60 rows leave
	// room for borders; 14 buttons no longer fit one 200-column row).
	if len(lines) != 59 {
		t.Fatalf("view has %d lines, want 59", len(lines))
	}
	if !strings.Contains(lines[52], "vremeplov") {
		t.Fatalf("status line missing: %q", lines[52])
	}
	if !strings.Contains(lines[54], "pause") {
		t.Fatalf("first footer row missing pause: %q", lines[54])
	}
	if !strings.Contains(lines[57], "quit") {
		t.Fatalf("second footer row missing quit: %q", lines[57])
	}
	if !strings.Contains(lines[53], "╭") || !strings.Contains(lines[58], "╰") {
		t.Fatal("bordered buttons missing their frames")
	}
}
