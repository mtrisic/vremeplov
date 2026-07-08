package core

import (
	"strings"
	"testing"
)

// TestROMBBoot verifies the ROM B configuration: ROM A v29 boots through
// its unconditional CALL 0x1000 into ROM B init and reaches READY.
func TestROMBBoot(t *testing.T) {
	m, err := New(Config{
		ROMA:    loadROM(t, "rom_a_v29.bin"),
		ROMB:    loadROM(t, "rom_b.bin"),
		Chargen: loadROM(t, "chrgen_elektronika.bin"),
		RAM:     RAM6K,
	})
	if err != nil {
		t.Fatal(err)
	}
	m.RunTstates(100 * TstatesPerFrame)
	screen := strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "READY") {
		t.Fatalf("v29 + ROM B did not reach READY:\n%s", screen)
	}
}
