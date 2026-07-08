package core

import (
	"strings"
	"testing"
)

func TestKeyMatrixAddresses(t *testing.T) {
	m := newMachine(t, RAM6K)
	// Spot-check the documented layout (SPEC.md §3.5).
	cases := map[Key]uint16{
		KeyA: 0x2001, KeyZ: 0x201A, KeyUp: 0x201B, KeySpace: 0x201F,
		Key0: 0x2020, Key9: 0x2029, KeyEnter: 0x2030, KeyShift: 0x2035,
	}
	for k, addr := range cases {
		m.PressKey(k)
		if got := m.MemRead(addr); got&1 != 0 {
			t.Errorf("key 0x%02X pressed: D0 at 0x%04X = 1, want 0", k, addr)
		}
		m.ReleaseKey(k)
		if got := m.MemRead(addr); got&1 != 1 {
			t.Errorf("key 0x%02X released: D0 at 0x%04X = 0, want 1", k, addr)
		}
	}
	// The comparator offset is not a key.
	m.PressKey(Key(0))
	if got := m.MemRead(0x2000); got != 0xFF {
		t.Errorf("comparator offset affected by PressKey: 0x%02X", got)
	}
}

func TestKeyQueueOrdering(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.QueueKeyEvents(
		KeyEvent{Tstate: 5000, Key: KeyB, Down: true},
		KeyEvent{Tstate: 1000, Key: KeyA, Down: true},
		KeyEvent{Tstate: 9000, Key: KeyA, Down: false},
	)
	m.RunTstates(2000)
	if !m.keys[KeyA] {
		t.Fatal("A not pressed after its timestamp")
	}
	if m.keys[KeyB] {
		t.Fatal("B pressed before its timestamp")
	}
	m.RunTstates(8000)
	if m.keys[KeyA] {
		t.Fatal("A not released after its timestamp")
	}
	if !m.keys[KeyB] {
		t.Fatal("B not pressed after its timestamp")
	}
}

// TestTypedKeysReachBASIC boots to READY and types "PRINT 2+2\n" via the
// deterministic key queue, asserting the ROM echoes the input and prints
// the result — end-to-end proof that the matrix, debounce, and ISR keyboard
// scan work.
func TestTypedKeysReachBASIC(t *testing.T) {
	m := bootMachine(t)
	typeString(m, "PRINT 2+2\n")
	m.RunTstates(200 * TstatesPerFrame)

	screen := strings.Join(m.ScreenText(), "\n")
	if !strings.Contains(screen, "PRINT 2+2") {
		t.Fatalf("typed text not echoed:\n%s", screen)
	}
	if !strings.Contains(screen, "\n 4") && !strings.Contains(screen, "\n4") {
		t.Fatalf("BASIC did not print 4:\n%s", screen)
	}
}

// typeString queues key events that type s via TypeText, failing the
// caller loudly on unsupported characters.
func typeString(m *Machine, s string) {
	if _, err := m.TypeText(s); err != nil {
		panic(err)
	}
}
