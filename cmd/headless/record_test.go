package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/gtp"
)

// TestRecordTapeCLI exercises --record-tape through the real binary: type
// a program, let it SAVE, and check the written image parses with the
// expected name and section. (Playing a recorded image back through the
// ROM's OLD is covered by core's TestRecorderFullCircle.)
func TestRecordTapeCLI(t *testing.T) {
	out := filepath.Join(t.TempDir(), "rec.gtp")
	runHeadless(t, "10 PRINT 123\nSAVE\n",
		"--type", "-", "--record-tape", out, "--frames", "1200")

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no image written: %v", err)
	}
	f, err := gtp.Parse(data)
	if err != nil {
		t.Fatalf("recorded image does not parse: %v", err)
	}
	if f.Name != "rec" {
		t.Errorf("Name = %q, want %q (output filename base)", f.Name, "rec")
	}
	secs, err := f.Sections()
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 1 || secs[0].Start != 0x2C36 {
		t.Fatalf("sections = %+v, want one at 0x2C36", secs)
	}
}

// TestRecordTapeCLIWAV: --record-tape with a .wav path writes playable
// audio; --tape accepts it back through the real CLI.
func TestRecordTapeCLIWAV(t *testing.T) {
	out := filepath.Join(t.TempDir(), "rec.wav")
	runHeadless(t, "10 PRINT 123\nSAVE\n",
		"--type", "-", "--record-tape", out, "--frames", "1200")

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no audio written: %v", err)
	}
	if string(data[:4]) != "RIFF" {
		t.Fatalf("output does not start with RIFF, got % X", data[:4])
	}
	sch, err := core.DecodeWAV(data)
	if err != nil {
		t.Fatal(err)
	}
	streams := sch.TapeStreams()
	if len(streams) != 1 {
		t.Fatalf("decoded %d streams from the written WAV, want 1", len(streams))
	}
	// And the real CLI plays it back through OLD.
	runHeadless(t, "RUN\n", "--tape", out, "--turbo", "--type", "-", "--frames", "100")
}
