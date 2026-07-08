package core

import (
	"bytes"
	"testing"
)

// TestSnapshotContinuation is the core determinism guarantee: run N
// frames, snapshot, restore onto a fresh machine, run both M further
// frames — everything must be byte-identical to the uninterrupted run.
func TestSnapshotContinuation(t *testing.T) {
	a := newMachine(t, RAM6K)
	a.RunTstates(50 * TstatesPerFrame)

	// Serialize through the wire format to cover WriteTo/ReadSnapshot.
	var buf bytes.Buffer
	if _, err := a.Snapshot().WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	snap, err := ReadSnapshot(&buf)
	if err != nil {
		t.Fatal(err)
	}
	b := newMachine(t, RAM6K)
	if err := b.Restore(snap); err != nil {
		t.Fatal(err)
	}

	// Queue identical future input on both.
	for _, m := range []*Machine{a, b} {
		m.QueueKeyEvents(KeyEvent{Tstate: m.Tstates() + 5*TstatesPerFrame, Key: KeyP, Down: true},
			KeyEvent{Tstate: m.Tstates() + 8*TstatesPerFrame, Key: KeyP, Down: false})
	}
	a.RunTstates(25 * TstatesPerFrame)
	b.RunTstates(25 * TstatesPerFrame)

	if a.Tstates() != b.Tstates() {
		t.Fatalf("T-state counters diverged: %d vs %d", a.Tstates(), b.Tstates())
	}
	if a.CPU().State() != b.CPU().State() {
		t.Fatal("CPU state diverged after restore")
	}
	if !bytes.Equal(a.DumpMemory(0x2800, 0x4000), b.DumpMemory(0x2800, 0x4000)) {
		t.Fatal("RAM diverged after restore")
	}
	pa := make([]byte, FrameWidth*FrameHeight)
	pb := make([]byte, FrameWidth*FrameHeight)
	sa, sb := a.Frame(pa), b.Frame(pb)
	if sa != sb || !bytes.Equal(pa, pb) {
		t.Fatalf("framebuffers diverged after restore (seq %d vs %d)", sa, sb)
	}
}

func TestSnapshotConfigMismatch(t *testing.T) {
	a := newMachine(t, RAM6K)
	s := a.Snapshot()
	b := newMachine(t, RAM4K)
	if err := b.Restore(s); err == nil {
		t.Fatal("restoring a 6K snapshot onto a 4K machine should fail")
	}
}
