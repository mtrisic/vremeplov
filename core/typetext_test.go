package core

import (
	"strings"
	"testing"
)

// TestTypeTextListing pipes a listing with the tricky cases — double
// letters (LL), shifted quotes, and multiple lines — through the key
// queue, RUNs it, and checks the output. This is the Phase-3 gate's
// listing-piping test at the core level.
func TestTypeTextListing(t *testing.T) {
	m := bootMachine(t)
	listing := "10 PRINT \"HELLO\"\n20 GOTO 10\n"
	end, err := m.TypeText(listing)
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(end - m.Tstates() + 50*TstatesPerFrame)

	screen := strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "10 PRINT \"HELLO\"") || !strings.Contains(screen, "20 GOTO 10") {
		t.Fatalf("listing not echoed faithfully (double letters?):\n%s", screen)
	}

	end, err = m.TypeText("RUN\n")
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(end - m.Tstates() + 100*TstatesPerFrame)
	screen = strings.Join(m.ScreenText(), "\n")
	if strings.Count(screen, "HELLO") < 10 {
		t.Fatalf("looping program should flood the screen with HELLO:\n%s", screen)
	}
}

func TestTypeTextShiftedPunctuation(t *testing.T) {
	m := bootMachine(t)
	end, err := m.TypeText("PRINT (2+3)*4-1\n")
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(end - m.Tstates() + 100*TstatesPerFrame)
	screen := strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "PRINT (2+3)*4-1") {
		t.Fatalf("shifted characters not typed correctly:\n%s", screen)
	}
	if !strings.Contains(screen, "19") {
		t.Fatalf("expression result missing:\n%s", screen)
	}
}

func TestTypeTextUnsupported(t *testing.T) {
	m := newMachine(t, RAM6K)
	if _, err := m.TypeText("čačak"); err == nil {
		t.Fatal("expected error for unmappable rune")
	}
	if len(m.queue) != 0 {
		t.Fatal("failed TypeText left events queued")
	}
}

func TestTypeTextDuration(t *testing.T) {
	m := newMachine(t, RAM6K)
	end, err := m.TypeText("RUN\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := TypeTextDuration("RUN\r\n"); end != want {
		t.Fatalf("TypeText end %d, TypeTextDuration %d", end, want)
	}
	if TypeTextDuration("") != 0 {
		t.Fatal("empty duration not zero")
	}
}
