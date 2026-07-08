package core

import (
	"encoding/gob"
	"fmt"
	"io"

	"github.com/mtrisic/gozilog/z80"
)

// snapshotVersion identifies the Snapshot wire format. Version 2 added
// the tape deck fields.
const snapshotVersion = 2

// Snapshot is a complete, restorable machine state (ROMs excepted: a
// snapshot restores onto a Machine built with the same Config). Restore
// followed by identical inputs reproduces execution byte-for-byte.
type Snapshot struct {
	Version int
	RAMSize RAMSize

	CPU     z80.State
	RAM     []byte
	Latch   byte
	Tstates uint64

	Keys  [keyCount]bool
	Queue []KeyEvent // pending (not yet applied) key events

	Tape        *TapeSchedule // nil when no tape is inserted
	TapeOrigin  uint64
	TapePlaying bool

	ShReg       byte
	CharCode    byte
	PrevRfsh    bool
	IntAsserted bool
	Back        []byte
	Front       []byte
	FrameSeq    uint64
}

// Snapshot captures the current machine state.
func (m *Machine) Snapshot() *Snapshot {
	s := &Snapshot{
		Version:     snapshotVersion,
		RAMSize:     m.ramSize,
		CPU:         m.cpu.State(),
		RAM:         append([]byte(nil), m.ram...),
		Latch:       m.latch,
		Tstates:     m.tstates,
		Keys:        m.keys,
		Queue:       append([]KeyEvent(nil), m.queue[m.qpos:]...),
		TapeOrigin:  m.tapeOrigin,
		TapePlaying: m.tapePlaying,
		ShReg:       m.shreg,
		CharCode:    m.charCode,
		PrevRfsh:    m.prevRfsh,
		IntAsserted: m.intAsserted,
		Back:        append([]byte(nil), m.back...),
		Front:       append([]byte(nil), m.front...),
		FrameSeq:    m.frameSeq,
	}
	if m.tape != nil {
		s.Tape = &TapeSchedule{
			Starts: append([]uint64(nil), m.tape.Starts...),
			Width:  m.tape.Width,
		}
	}
	return s
}

// Restore replaces the machine state with a snapshot. The machine must
// have been built with the same RAM configuration (ROM contents are not
// part of the snapshot). A manual Restore while rewind history is
// enabled rebases the history — the old timeline is unreachable.
func (m *Machine) Restore(s *Snapshot) error {
	if s.Version != snapshotVersion {
		return fmt.Errorf("core: unsupported snapshot version %d", s.Version)
	}
	if s.RAMSize != m.ramSize {
		return fmt.Errorf("core: snapshot RAM size %d does not match machine %d", s.RAMSize, m.ramSize)
	}
	if len(s.RAM) != len(m.ram) || len(s.Back) != len(m.back) || len(s.Front) != len(m.front) {
		return fmt.Errorf("core: snapshot buffer sizes do not match machine")
	}
	m.cpu.SetState(s.CPU)
	copy(m.ram, s.RAM)
	m.latch = s.Latch
	m.tstates = s.Tstates
	m.keys = s.Keys
	m.queue = append([]KeyEvent(nil), s.Queue...)
	m.qpos = 0
	m.tape = nil
	if s.Tape != nil {
		m.tape = &TapeSchedule{
			Starts: append([]uint64(nil), s.Tape.Starts...),
			Width:  s.Tape.Width,
		}
	}
	m.tapeOrigin = s.TapeOrigin
	m.tapePlaying = s.TapePlaying
	m.shreg = s.ShReg
	m.charCode = s.CharCode
	m.prevRfsh = s.PrevRfsh
	m.intAsserted = s.IntAsserted
	// The INT line lives in the machine, not in z80.State; re-drive it.
	m.cpu.SetINT(s.IntAsserted)
	copy(m.back, s.Back)
	copy(m.front, s.Front)
	m.frameSeq = s.FrameSeq
	if m.histEnabled && !m.histReplaying {
		m.HistoryRebase()
	}
	return nil
}

// WriteTo serializes the snapshot (encoding/gob, deterministic for this
// fixed shape).
func (s *Snapshot) WriteTo(w io.Writer) (int64, error) {
	cw := &countWriter{w: w}
	err := gob.NewEncoder(cw).Encode(s)
	return cw.n, err
}

// ReadSnapshot deserializes a snapshot written by WriteTo.
func ReadSnapshot(r io.Reader) (*Snapshot, error) {
	var s Snapshot
	if err := gob.NewDecoder(r).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
