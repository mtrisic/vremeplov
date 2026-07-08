package main

// Phase-3 gate tests: the three program-input paths, each compared to a
// committed golden frame. Regenerate goldens with:
//
//	go test ./... -run Golden -update
//
// after eyeballing the new frames.

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files from current output")

// repoRoot walks up to the go.work root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.work not found")
		}
		dir = parent
	}
}

var buildOnce sync.Once
var builtBin string

// buildHeadless builds the actual binary once per test run, so the gate
// exercises the real CLI, flags and all.
func buildHeadless(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		bin := filepath.Join(os.TempDir(), "vremeplov-headless-test")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("building headless: %v\n%s", err, out)
		}
		builtBin = bin
	})
	if builtBin == "" {
		t.Fatal("headless build failed earlier")
	}
	return builtBin
}

func runHeadless(t *testing.T, stdin string, args ...string) {
	t.Helper()
	cmd := exec.Command(buildHeadless(t), args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("headless %v: %v\n%s", args, err, out)
	}
}

func checkGolden(t *testing.T, got []byte, goldenName string) {
	t.Helper()
	golden := filepath.Join("testdata", goldenName)
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden (run with -update after eyeballing): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from golden %s", goldenName, golden)
	}
}

// TestTapePlaybackGolden: hackaday.gtp through the ROM's own OLD routine
// (faithful pulse playback), then RUN typed at the READY prompt.
func TestTapePlaybackGolden(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "tape.png")
	runHeadless(t, "RUN\n",
		"--tape", filepath.Join(root, "core", "gtp", "testdata", "hackaday.gtp"),
		"--turbo", "--type", "-", "--frames", "300", "--dump-frame", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, got, "tape_hackaday.png")
}

// TestFastLoadGolden: same program poked directly into memory, then RUN.
func TestFastLoadGolden(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "fast.png")
	runHeadless(t, "RUN\n",
		"--load-gtp", filepath.Join(root, "core", "gtp", "testdata", "hackaday.gtp"),
		"--type", "-", "--frames", "300", "--dump-frame", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, got, "fast_hackaday.png")
}

// TestTurboBlockGolden: hackaday.gtp with its data block re-typed as
// turbo (0x01) must load identically to the standard original through
// both paths — turbo payloads share the standard layout and compile at
// standard speed (SPEC §3.7) — so both existing goldens must match.
func TestTurboBlockGolden(t *testing.T) {
	root := repoRoot(t)
	img, err := os.ReadFile(filepath.Join(root, "core", "gtp", "testdata", "hackaday.gtp"))
	if err != nil {
		t.Fatal(err)
	}
	flipped := 0
	for i := 0; i+5 <= len(img); i += 5 + (int(img[i+1]) | int(img[i+2])<<8) {
		if img[i] == 0x00 {
			img[i] = 0x01
			flipped++
		}
	}
	if flipped != 1 {
		t.Fatalf("flipped %d standard blocks, want 1", flipped)
	}
	turbo := filepath.Join(t.TempDir(), "hackaday-turbo.gtp")
	if err := os.WriteFile(turbo, img, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "tape.png")
	runHeadless(t, "RUN\n",
		"--tape", turbo, "--turbo", "--type", "-",
		"--frames", "300", "--dump-frame", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, got, "tape_hackaday.png")

	out = filepath.Join(t.TempDir(), "fast.png")
	runHeadless(t, "RUN\n",
		"--load-gtp", turbo, "--type", "-",
		"--frames", "300", "--dump-frame", out)
	if got, err = os.ReadFile(out); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, got, "fast_hackaday.png")
}

// TestTypedListingGolden: the examples/hello.bas listing typed through
// the keyboard, RUN included in the file.
func TestTypedListingGolden(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "typed.png")
	runHeadless(t, "",
		"--type", filepath.Join(root, "examples", "hello.bas"),
		"--frames", "200", "--dump-frame", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, got, "typed_hello.png")
}
