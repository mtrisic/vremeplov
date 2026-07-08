package core

import "testing"

func TestROMWriteIgnored(t *testing.T) {
	m := newMachine(t, RAM6K)
	before := m.MemRead(0x0000)
	m.MemWrite(0x0000, ^before)
	if got := m.MemRead(0x0000); got != before {
		t.Fatalf("ROM A write not ignored: 0x%02X -> 0x%02X", before, got)
	}
	if got := m.MemRead(0x1000); got != 0xFF {
		t.Fatalf("empty ROM B socket reads 0x%02X, want 0xFF", got)
	}
	m.MemWrite(0x1000, 0x12)
	if got := m.MemRead(0x1000); got != 0xFF {
		t.Fatalf("ROM B write not ignored: got 0x%02X", got)
	}
}

func TestPeripheralMirroring(t *testing.T) {
	m := newMachine(t, RAM6K)
	m.PressKey(KeyR)
	for _, base := range []uint16{0x2000, 0x2040, 0x2400, 0x27C0} {
		if got := m.MemRead(base + uint16(KeyR)); got != 0xFE {
			t.Errorf("pressed key at mirror 0x%04X reads 0x%02X, want 0xFE", base+uint16(KeyR), got)
		}
		if got := m.MemRead(base + uint16(KeyA)); got != 0xFF {
			t.Errorf("released key at mirror 0x%04X reads 0x%02X, want 0xFF", base+uint16(KeyA), got)
		}
	}
}

func TestLatchDecode(t *testing.T) {
	m := newMachine(t, RAM6K)
	// All three address forms the ROM uses must hit the latch.
	for _, addr := range []uint16{0x2038, 0x207F, 0x27FF} {
		m.MemWrite(addr, 0x00)
		m.MemWrite(addr, 0xBC)
		if m.Latch() != 0xBC {
			t.Errorf("write to 0x%04X did not reach latch", addr)
		}
	}
	// Keyboard-region addresses (offset bits 5..3 != 111) must not.
	m.MemWrite(0x2038, 0xAA)
	m.MemWrite(0x2000, 0x55)
	m.MemWrite(0x2037, 0x55)
	if m.Latch() != 0xAA {
		t.Errorf("keyboard-region write reached latch: 0x%02X", m.Latch())
	}
	// Latch reads are undefined -> 0xFF.
	if got := m.MemRead(0x2038); got != 0xFF {
		t.Errorf("latch read = 0x%02X, want 0xFF", got)
	}
}

func TestRAMSizes(t *testing.T) {
	cases := []struct {
		size RAMSize
		last uint16 // last populated address
	}{
		{RAM2K, 0x2FFF},
		{RAM4K, 0x37FF},
		{RAM6K, 0x3FFF},
		{RAMExpanded, 0xFFFF},
	}
	for _, c := range cases {
		m := newMachine(t, c.size)
		m.MemWrite(0x2038, 0x80) // clamp off for direct addressing
		m.MemWrite(c.last, 0x5A)
		if got := m.MemRead(c.last); got != 0x5A {
			t.Errorf("RAM%d: last address 0x%04X not writable (got 0x%02X)", c.size, c.last, got)
		}
		if c.size != RAMExpanded {
			beyond := c.last + 1
			m.MemWrite(beyond, 0x5A)
			if got := m.MemRead(beyond); got != 0xFF {
				t.Errorf("RAM%d: unpopulated 0x%04X reads 0x%02X, want 0xFF", c.size, beyond, got)
			}
		}
	}
}

func TestA7Clamp(t *testing.T) {
	m := newMachine(t, RAM6K)
	// Clamp off (latch bit 7 = 1): 0x2900 and 0x2980 are distinct.
	m.MemWrite(0x2038, 0x80)
	m.MemWrite(0x2900, 0x11)
	m.MemWrite(0x2980, 0x22)
	if a, b := m.MemRead(0x2900), m.MemRead(0x2980); a != 0x11 || b != 0x22 {
		t.Fatalf("clamp off: got 0x%02X/0x%02X, want 0x11/0x22", a, b)
	}
	// Clamp on (latch bit 7 = 0): A7 forced to 1 — both addresses alias
	// the upper half, for reads AND writes (AGENTS.md log 5).
	m.MemWrite(0x2038, 0x00)
	if a, b := m.MemRead(0x2900), m.MemRead(0x2980); a != 0x22 || b != 0x22 {
		t.Fatalf("clamp on: got 0x%02X/0x%02X, want 0x22/0x22", a, b)
	}
	m.MemWrite(0x2900, 0x33) // lands at 0x2980
	m.MemWrite(0x2038, 0x80)
	if a, b := m.MemRead(0x2900), m.MemRead(0x2980); a != 0x11 || b != 0x33 {
		t.Fatalf("clamped write aliasing: got 0x%02X/0x%02X, want 0x11/0x33", a, b)
	}
}

func TestLoadBinaryAndDumpMemory(t *testing.T) {
	m := newMachine(t, RAM6K)
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	if err := m.LoadBinary(0x2C3A, data); err != nil {
		t.Fatal(err)
	}
	m.MemWrite(0x2038, 0x80) // clamp off so the dump sees raw RAM
	got := m.DumpMemory(0x2C3A, 0x2C3E)
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("dump[%d] = 0x%02X, want 0x%02X", i, got[i], data[i])
		}
	}
	if err := m.LoadBinary(0x3FFF, data); err == nil {
		t.Fatal("LoadBinary past RAM top should fail")
	}
}
