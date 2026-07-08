package core

import (
	"bytes"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate golden files (review the diff!)")

// bootFrames is enough for ROM A to size RAM, initialize BASIC, and
// settle on the READY prompt with the cursor blinking in a known phase.
const bootFrames = 100

func bootMachine(t *testing.T) *Machine {
	t.Helper()
	m := newMachine(t, RAM6K)
	m.RunTstates(bootFrames * TstatesPerFrame)
	return m
}

// TestBootReady boots ROM A headless and asserts the READY prompt two
// independent ways: through the video RAM text content, and against the
// committed golden framebuffer (Gate 1).
func TestBootReady(t *testing.T) {
	m := bootMachine(t)

	// (a) Text-level assertion, independent of pixel calibration.
	screen := m.ScreenText()
	found := false
	for _, row := range screen {
		if strings.Contains(row, "READY") {
			found = true
		}
	}
	if !found {
		t.Fatalf("READY not on screen after %d frames:\n%s", bootFrames, strings.Join(screen, "\n"))
	}

	// (b) Golden full-frame compare.
	pix := make([]byte, FrameWidth*FrameHeight)
	m.Frame(pix)
	got := encodePNG(t, pix)
	golden := filepath.Join("testdata", "boot_ready.png")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden missing (run with -update once, then eyeball it): %v", err)
	}
	if !bytes.Equal(got, want) {
		actual := golden + ".actual.png"
		_ = os.WriteFile(actual, got, 0o644)
		t.Fatalf("framebuffer differs from golden %s (actual written to %s)", golden, actual)
	}
}

// TestBootDeterminism runs two independent machines and requires
// byte-identical video RAM, framebuffers, and CPU state.
func TestBootDeterminism(t *testing.T) {
	a, b := bootMachine(t), bootMachine(t)
	pa := make([]byte, FrameWidth*FrameHeight)
	pb := make([]byte, FrameWidth*FrameHeight)
	if sa, sb := a.Frame(pa), b.Frame(pb); sa != sb {
		t.Fatalf("frame seq %d != %d", sa, sb)
	}
	if !bytes.Equal(pa, pb) {
		t.Fatal("framebuffers differ between identical runs")
	}
	if !bytes.Equal(a.DumpMemory(0x2800, 0x4000), b.DumpMemory(0x2800, 0x4000)) {
		t.Fatal("RAM differs between identical runs")
	}
	if a.CPU().State() != b.CPU().State() {
		t.Fatal("CPU state differs between identical runs")
	}
	if a.Tstates() != b.Tstates() {
		t.Fatal("T-state counters differ between identical runs")
	}
}

// encodePNG renders the luminance framebuffer as an 8-bit grayscale PNG
// (stdlib encoder; deterministic for identical input).
func encodePNG(t *testing.T, pix []byte) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, FrameWidth, FrameHeight))
	for i, p := range pix {
		img.Pix[i] = p * 0xFF
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
