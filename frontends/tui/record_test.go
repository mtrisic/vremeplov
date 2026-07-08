package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
)

// TestTapeRecordChrome drives ^X t around a real SAVE and checks that a
// valid .gtp lands in the working directory.
func TestTapeRecordChrome(t *testing.T) {
	m := testMachine(t) // before Chdir: the helper walks up from the cwd
	t.Chdir(t.TempDir())
	mo := newModel(m)
	mo.lastTick = time.Unix(0, 0)

	mo.m.RunTstates(150 * core.TstatesPerFrame) // boot to READY
	end, err := mo.m.TypeText("10 PRINT 5\n")
	if err != nil {
		t.Fatal(err)
	}
	mo.m.RunTstates(end - mo.m.Tstates() + 10*core.TstatesPerFrame)

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('t'))
	if !mo.m.TapeRecording() {
		t.Fatal("^X t did not arm the recorder")
	}
	if !strings.Contains(mo.View(), "[REC]") {
		t.Fatal("REC flag missing from the status line")
	}

	end, err = mo.m.TypeText("SAVE\n")
	if err != nil {
		t.Fatal(err)
	}
	mo.m.RunTstates(end - mo.m.Tstates() + 400*core.TstatesPerFrame)

	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('t'))
	if mo.m.TapeRecording() {
		t.Fatal("^X t did not stop the recorder")
	}
	if !strings.Contains(mo.status, "{gtp,wav}") {
		t.Fatalf("status = %q, want the written file pair", mo.status)
	}

	gtps, err := filepath.Glob("vremeplov-tape-*.gtp")
	if err != nil || len(gtps) != 1 {
		t.Fatalf("written .gtp files = %v (err %v), want exactly one", gtps, err)
	}
	data, err := os.ReadFile(gtps[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gtp.Parse(data); err != nil {
		t.Fatalf("written image does not parse: %v", err)
	}

	wavs, err := filepath.Glob("vremeplov-tape-*.wav")
	if err != nil || len(wavs) != 1 {
		t.Fatalf("written .wav files = %v (err %v), want exactly one", wavs, err)
	}
	audio, err := os.ReadFile(wavs[0])
	if err != nil {
		t.Fatal(err)
	}
	sch, err := core.DecodeWAV(audio)
	if err != nil {
		t.Fatalf("written audio does not decode: %v", err)
	}
	if streams := sch.TapeStreams(); len(streams) != 1 {
		t.Fatalf("written audio decodes to %d streams, want 1", len(streams))
	}
}

// TestTapeRecordChromeEmpty: stopping with nothing captured reports it
// and writes no file.
func TestTapeRecordChromeEmpty(t *testing.T) {
	m := testMachine(t)
	t.Chdir(t.TempDir())
	mo := newModel(m)
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('t'))
	mo.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	mo.Update(runeMsg('t'))
	if !strings.Contains(mo.status, "no tape output") {
		t.Fatalf("status = %q", mo.status)
	}
	if matches, _ := filepath.Glob("*.gtp"); len(matches) != 0 {
		t.Fatalf("unexpected files: %v", matches)
	}
}
