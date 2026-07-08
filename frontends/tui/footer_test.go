package main

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
)

// TestFooterWrapsToWidth: on a narrow-but-tall terminal the button bar
// wraps to several rows instead of overflowing the line.
func TestFooterWrapsToWidth(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	mo.Update(tea.WindowSizeMsg{Width: 60, Height: 40})

	lines, hits := mo.renderFooterButtons(60, false)
	if len(lines) < 2 {
		t.Fatalf("60-column footer has %d button rows, want wrapping", len(lines))
	}
	if len(hits) != len(mo.footerButtons()) {
		t.Fatalf("%d hits for %d buttons", len(hits), len(mo.footerButtons()))
	}
	for _, h := range hits {
		if h.x1 > 60 {
			t.Fatalf("button %q overflows the width: x1=%d", h.key, h.x1)
		}
	}
	view := strings.Split(mo.View(), "\n")
	if got := strings.Count(strings.Join(view, "\n"), "quit"); got != 1 {
		t.Fatalf("quit button appears %d times", got)
	}
}

// TestFooterCollapsesOnShortTerminal: the 90×15 VSCode panel keeps its
// full 14 content rows — buttons give way to the screen.
func TestFooterCollapsesOnShortTerminal(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	mo.Update(tea.WindowSizeMsg{Width: 90, Height: 15})

	view := strings.Split(mo.View(), "\n")
	if len(view) != 15 {
		t.Fatalf("view has %d lines, want 15 (footer collapsed)", len(view))
	}
	if strings.Contains(view[14], "quit") {
		t.Fatalf("buttons should be hidden at 90x15: %q", view[14])
	}
	if len(mo.footHits) != 0 {
		t.Fatal("collapsed footer must not leave clickable regions")
	}
}

// TestFooterMouseClick: a left click on a footer button dispatches its
// chrome action — pause toggles, and the button relabels to resume.
func TestFooterMouseClick(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	mo.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	mo.View() // records footHits

	var pause footHit
	for _, h := range mo.footHits {
		if h.key == "p" {
			pause = h
		}
	}
	if pause.x1 == 0 {
		t.Fatal("no hit region for the pause button")
	}
	click := tea.MouseMsg{X: pause.x0, Y: pause.y0,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	mo.Update(click)
	if !mo.paused {
		t.Fatal("click on [p] did not pause")
	}
	if !strings.Contains(mo.View(), "resume") {
		t.Fatal("paused footer does not offer resume")
	}
	mo.Update(click)
	if mo.paused {
		t.Fatal("second click did not resume")
	}

	// A click outside any button is ignored.
	mo.Update(tea.MouseMsg{X: 0, Y: 0,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if mo.paused {
		t.Fatal("click on the screen area toggled chrome")
	}
}

// TestQuitConfirmation: quitting asks first — from the keyboard and
// from the footer button — and anything but y cancels without leaking
// the key into the machine.
func TestQuitConfirmation(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	mo.Update(tea.WindowSizeMsg{Width: 200, Height: 60})

	// ^X q arms the confirmation instead of quitting.
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	_, cmd := mo.Update(runeMsg('q'))
	if cmd != nil {
		t.Fatal("^X q quit without confirmation")
	}
	if !mo.confirmQuit || !strings.Contains(mo.status, "quit?") {
		t.Fatalf("confirmation not armed: status = %q", mo.status)
	}
	if !strings.Contains(mo.View(), "sure?") {
		t.Fatal("quit button not relabeled to sure?")
	}

	// Any other key cancels and is swallowed, not typed.
	_, cmd = mo.Update(runeMsg('n'))
	if cmd != nil || mo.confirmQuit {
		t.Fatal("cancel did not clear the confirmation")
	}
	if !strings.Contains(mo.status, "cancelled") {
		t.Fatalf("status = %q", mo.status)
	}
	if got := mo.m.MemRead(0x2000 + uint16(core.KeyN)); got&1 == 0 {
		t.Fatal("cancelling key leaked into the machine matrix")
	}

	// y confirms.
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('q'))
	_, cmd = mo.Update(runeMsg('y'))
	if cmd == nil {
		t.Fatal("y did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("cmd = %T, want tea.QuitMsg", cmd())
	}

	// Clicking another button while pending cancels and acts instead.
	mo.confirmQuit = true
	mo.View()
	pause := findHit(t, mo, "p")
	mo.Update(tea.MouseMsg{X: pause.x0, Y: pause.y0,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if mo.confirmQuit || !mo.paused {
		t.Fatal("other button did not cancel the confirmation")
	}

	// Click quit twice: armed, then quits. Labels shifted (resume,
	// sure?), so re-render and re-locate before each click.
	mo.View()
	quit := findHit(t, mo, "q")
	mo.Update(tea.MouseMsg{X: quit.x0, Y: quit.y0,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !mo.confirmQuit {
		t.Fatal("first click did not arm the confirmation")
	}
	mo.View()
	quit = findHit(t, mo, "q")
	_, cmd = mo.Update(tea.MouseMsg{X: quit.x0, Y: quit.y0,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil {
		t.Fatal("second click did not quit")
	}
}

// findHit locates a footer button's hit region in the last View render.
func findHit(t *testing.T, mo *model, key string) footHit {
	t.Helper()
	for _, h := range mo.footHits {
		if h.key == key {
			return h
		}
	}
	t.Fatalf("no hit region for %q", key)
	return footHit{}
}

// TestSnapshotSaveLoadChrome: ^X w writes a snapshot file, more frames
// run, ^X l restores the exact saved state.
func TestSnapshotSaveLoadChrome(t *testing.T) {
	mo := newModel(testMachine(t)) // before Chdir: the helper walks up from the cwd
	mo.lastTick = time.Unix(0, 0)
	t.Chdir(t.TempDir())

	for i := 0; i < 5; i++ {
		doTick(mo)
	}
	saved := mo.m.Tstates()
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('w'))
	if !strings.Contains(mo.status, "saved to vremeplov-snap-") {
		t.Fatalf("status = %q", mo.status)
	}

	for i := 0; i < 7; i++ {
		doTick(mo)
	}
	if mo.m.Tstates() == saved {
		t.Fatal("machine did not advance after save")
	}

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('l'))
	if !strings.Contains(mo.status, "restored") {
		t.Fatalf("status = %q", mo.status)
	}
	if got := mo.m.Tstates(); got != saved {
		t.Fatalf("restored Tstates = %d, want %d", got, saved)
	}
}

// TestScreenshotChrome: ^X c writes a decodable PNG; the full-frame
// toggle switches the captured geometry.
func TestScreenshotChrome(t *testing.T) {
	mo := newModel(testMachine(t)) // before Chdir: the helper walks up from the cwd
	mo.lastTick = time.Unix(0, 0)
	t.Chdir(t.TempDir())

	shoot := func(wantW, wantH int) {
		t.Helper()
		mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
		mo.Update(runeMsg('c'))
		if !strings.Contains(mo.status, "saved to vremeplov-shot-") {
			t.Fatalf("status = %q", mo.status)
		}
		name := strings.TrimPrefix(mo.status, "screenshot saved to ")
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != wantW || b.Dy() != wantH {
			t.Fatalf("shot is %dx%d, want %dx%d", b.Dx(), b.Dy(), wantW, wantH)
		}
	}
	shoot(core.ActiveW, core.ActiveH)
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('f')) // full frame
	doTick(mo)              // a new frame, so the shot name differs
	shoot(core.FrameWidth, core.FrameHeight)
}

// TestSnapshotLoadNothingThere: ^X l with no snapshot files reports it.
func TestSnapshotLoadNothingThere(t *testing.T) {
	mo := newModel(testMachine(t))
	mo.lastTick = time.Unix(0, 0)
	t.Chdir(t.TempDir())

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('l'))
	if !strings.Contains(mo.status, "no vremeplov-snap-") {
		t.Fatalf("status = %q", mo.status)
	}
	if _, err := os.Getwd(); err != nil {
		t.Fatal(err)
	}
}

// TestNewestSnapshotFile: numeric order wins over lexicographic.
func TestNewestSnapshotFile(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, n := range []string{"vremeplov-snap-9.gob", "vremeplov-snap-100.gob"} {
		if err := os.WriteFile(n, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := newestSnapshotFile()
	if err != nil || got != "vremeplov-snap-100.gob" {
		t.Fatalf("newest = %q, %v", got, err)
	}
}
