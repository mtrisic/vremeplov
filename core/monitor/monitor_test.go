package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtrisic/vremeplov/core"
)

// newSession builds a session on the default test machine (ROM A v28,
// Elektronika chargen, 6 KB RAM), loading ROMs from the go.work root so
// the core module does not import roms (SPEC.md §4.1).
func newSession(t *testing.T) *Session {
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
			t.Fatal("go.work not found above test working directory")
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
	return New(m)
}

func execJoined(s *Session, line string) string {
	return strings.Join(s.Exec(line), "\n")
}

func TestExecPauseContinue(t *testing.T) {
	s := newSession(t)
	if out := execJoined(s, "p"); !s.Paused || !strings.Contains(out, "paused at") {
		t.Fatalf("p: Paused=%v out=%q", s.Paused, out)
	}
	if out := execJoined(s, "c"); s.Paused || out != "running" {
		t.Fatalf("c: Paused=%v out=%q", s.Paused, out)
	}
}

func TestExecStepAdvances(t *testing.T) {
	s := newSession(t)
	before := s.M.Tstates()
	out := execJoined(s, "s 3")
	if s.M.Tstates() <= before {
		t.Fatal("s 3 did not advance the machine")
	}
	if !s.Paused {
		t.Fatal("s must set Paused")
	}
	if !strings.Contains(out, " ") || len(out) == 0 {
		t.Fatalf("s output = %q", out)
	}
}

func TestExecPokeDumpSetStepOver(t *testing.T) {
	s := newSession(t)
	// Addresses with bit 7 set survive the power-on A7 clamp untranslated.
	s.Exec("poke 2cc0 cd e0 2c") // CALL 2CE0
	s.Exec("poke 2ce0 c9")       // RET
	if out := execJoined(s, "x 2cc0 3"); !strings.Contains(out, "CD E0 2C") {
		t.Fatalf("x = %q, want the poked bytes", out)
	}
	s.Exec("set sp 2cf0")
	s.Exec("set pc 2cc0")
	if st := s.M.CPU().State(); st.PC != 0x2CC0 || st.SP != 0x2CF0 {
		t.Fatalf("set failed: PC=0x%04X SP=0x%04X", st.PC, st.SP)
	}
	s.Exec("n") // step over the CALL: runs CALL+RET, lands at 2CC3
	if pc := s.M.CPU().State().PC; pc != 0x2CC3 {
		t.Fatalf("step-over landed at 0x%04X, want 0x2CC3", pc)
	}
	if bps := s.M.Breakpoints(); len(bps) != 0 {
		t.Fatalf("temporary breakpoint leaked: %v", bps)
	}
}

func TestExecBreakpointsAndWatchpoints(t *testing.T) {
	s := newSession(t)
	s.Exec("b 0038")
	s.Exec("b 2c3a")
	if out := execJoined(s, "bl"); !strings.Contains(out, "0038 2C3A") {
		t.Fatalf("bl = %q", out)
	}
	s.Exec("bd 0038")
	if bps := s.M.Breakpoints(); len(bps) != 1 || bps[0] != 0x2C3A {
		t.Fatalf("Breakpoints() = %v", bps)
	}

	s.Exec("w 2800-29ff w")
	if out := execJoined(s, "wl"); !strings.Contains(out, "watch 2800-29FF w") {
		t.Fatalf("wl = %q", out)
	}
	s.Exec("wd 2800")
	if ws := s.M.Watches(); len(ws) != 0 {
		t.Fatalf("Watches() = %v", ws)
	}
}

func TestExecDisasmROMStart(t *testing.T) {
	s := newSession(t)
	out := execJoined(s, "d 0000 2")
	// ROM A starts with DI (F3) at 0x0000.
	if !strings.Contains(out, "0000 F3") || !strings.Contains(out, "DI") {
		t.Fatalf("d 0000 = %q, want the reset vector", out)
	}
}

func TestDisplayWatches(t *testing.T) {
	s := newSession(t)
	s.Exec("poke 2cd0 34 12")
	s.Exec("watch 2cd0 w")
	if out := strings.Join(s.WatchLines(), "\n"); !strings.Contains(out, "2CD0 w 1234") {
		t.Fatalf("WatchLines = %q", out)
	}
	s.Exec("unwatch 2cd0")
	if len(s.WatchLines()) != 0 {
		t.Fatal("unwatch did not clear the display watch")
	}
}

func TestPanelLines(t *testing.T) {
	s := newSession(t)
	regs := s.RegisterLines()
	if len(regs) != 3 || !strings.HasPrefix(regs[0], "PC ") {
		t.Fatalf("RegisterLines = %q", regs)
	}
	s.Exec("b 0000") // power-on PC, so the marker is the combined one
	dis := s.DisasmLines(3)
	if len(dis) != 3 || !strings.HasPrefix(dis[0], "◉0000") {
		t.Fatalf("DisasmLines = %q", dis)
	}
}

func TestFormatStop(t *testing.T) {
	if got := FormatStop(core.Stop{Reason: core.StopBreakpoint, PC: 0x38}); got != "break at 0038" {
		t.Fatalf("FormatStop breakpoint = %q", got)
	}
	got := FormatStop(core.Stop{Reason: core.StopWatch, Addr: 0x2CD0, Write: true, Data: 0x42, PC: 0x1234})
	if got != "watch write 2CD0=42 (PC 1234)" {
		t.Fatalf("FormatStop watch = %q", got)
	}
	if got := FormatStop(core.Stop{Reason: core.StopBudget}); got != "" {
		t.Fatalf("FormatStop budget = %q", got)
	}
}

func TestExecErrorsAndHelp(t *testing.T) {
	s := newSession(t)
	for cmd, want := range map[string]string{
		"bogus":        "unknown command",
		"b zz":         "bad hex",
		"to":           "usage: to ADDR",
		"w 2800 q":     "bad kind",
		"set xx 1":     "unknown register",
		"poke 0100 ff": "outside populated RAM",
	} {
		if out := execJoined(s, cmd); !strings.Contains(out, want) {
			t.Errorf("Exec(%q) = %q, want %q", cmd, out, want)
		}
	}
	if out := execJoined(s, "help"); !strings.Contains(out, "step-over") || !strings.Contains(out, "A7 clamp") {
		t.Fatal("help output incomplete")
	}
	if out := s.Exec("   "); out != nil {
		t.Fatalf("blank line produced output %q", out)
	}
}

func TestExecStepBackAndRewind(t *testing.T) {
	s := newSession(t)
	s.M.EnableHistory(10*core.TstatesPerFrame, 100)
	s.M.RunTstates(150 * core.TstatesPerFrame)
	t0 := s.M.Tstates()
	pc0 := s.M.CPU().State().PC

	s.Exec("s 5")
	out := strings.Join(s.Exec("bs 5"), "\n")
	if s.M.Tstates() != t0 || s.M.CPU().State().PC != pc0 {
		t.Fatalf("bs 5 landed at T=%d PC=%04X, want T=%d PC=%04X (out %q)",
			s.M.Tstates(), s.M.CPU().State().PC, t0, pc0, out)
	}
	if !s.Paused {
		t.Fatal("bs must pause the session")
	}

	out = strings.Join(s.Exec("rw 1"), "\n")
	if !strings.Contains(out, "rewound") {
		t.Fatalf("rw output = %q", out)
	}
	if s.M.Tstates() >= t0 {
		t.Fatalf("rw 1 did not go back (T=%d, was %d)", s.M.Tstates(), t0)
	}
}

func TestExecRewindWithoutHistory(t *testing.T) {
	s := newSession(t)
	for _, cmd := range []string{"bs", "rw"} {
		if out := strings.Join(s.Exec(cmd), "\n"); !strings.Contains(out, "history is not enabled") {
			t.Errorf("%s without history: %q", cmd, out)
		}
	}
}

func TestExecSetRebasesHistory(t *testing.T) {
	s := newSession(t)
	s.M.EnableHistory(10*core.TstatesPerFrame, 100)
	s.M.RunTstates(50 * core.TstatesPerFrame)
	s.Exec("set a ff")
	oldest, newest, ok := s.M.HistorySpan()
	if !ok || oldest != newest {
		t.Fatalf("set did not rebase history: span (%d,%d,%v)", oldest, newest, ok)
	}
}
