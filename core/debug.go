package core

import (
	"fmt"
	"sort"

	"github.com/mtrisic/gozilog/z80"
)

// Debugger support: breakpoints on instruction addresses and watchpoints
// on CPU bus accesses, honored by RunDebug. Breakpoints and watchpoints
// are debugger state, not machine state — they are observational, never
// alter execution, and are deliberately excluded from Snapshot/Restore.
//
// Watchpoints hook the bus pins in Tick (the T-state the CPU actually
// asserts /MREQ with /RD or /WR), so they see exactly the CPU's own data
// accesses: opcode fetches, the video refresh fetch, and DumpMemory never
// trigger them.

// StopReason says why RunDebug returned.
type StopReason int

const (
	// StopBudget means the requested T-state budget elapsed.
	StopBudget StopReason = iota
	// StopBreakpoint means the next instruction address is a breakpoint;
	// the instruction has not been executed yet.
	StopBreakpoint
	// StopWatch means the just-completed instruction touched a watched
	// address.
	StopWatch
)

// Stop describes why RunDebug stopped. PC is always the address of the
// next instruction; Addr, Write, and Data are set for StopWatch only.
type Stop struct {
	Reason StopReason
	PC     uint16
	Addr   uint16
	Write  bool
	Data   byte
}

// WatchKind selects which bus accesses a watchpoint observes.
type WatchKind int

const (
	WatchRead  WatchKind = 1 << iota // CPU data reads (not opcode fetches)
	WatchWrite                       // CPU writes
	WatchRW    = WatchRead | WatchWrite
)

// Watch is an address-range watchpoint; the range is inclusive on both
// ends so it can reach 0xFFFF.
type Watch struct {
	Start, End uint16
	Kind       WatchKind
}

// AddBreakpoint sets an instruction breakpoint at pc (idempotent).
func (m *Machine) AddBreakpoint(pc uint16) {
	if m.breakpoints == nil {
		m.breakpoints = make(map[uint16]struct{})
	}
	m.breakpoints[pc] = struct{}{}
}

// RemoveBreakpoint clears the breakpoint at pc, if any.
func (m *Machine) RemoveBreakpoint(pc uint16) {
	delete(m.breakpoints, pc)
}

// Breakpoints returns the current breakpoints in ascending order.
func (m *Machine) Breakpoints() []uint16 {
	out := make([]uint16, 0, len(m.breakpoints))
	for pc := range m.breakpoints {
		out = append(out, pc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AddWatch sets a watchpoint on the inclusive address range [start, end].
func (m *Machine) AddWatch(start, end uint16, kind WatchKind) error {
	if start > end {
		return fmt.Errorf("core: AddWatch: start 0x%04X > end 0x%04X", start, end)
	}
	if kind&WatchRW == 0 || kind&^WatchRW != 0 {
		return fmt.Errorf("core: AddWatch: invalid kind %d", kind)
	}
	m.watches = append(m.watches, Watch{Start: start, End: end, Kind: kind})
	return nil
}

// RemoveWatch clears every watchpoint whose range starts at start.
func (m *Machine) RemoveWatch(start uint16) {
	kept := m.watches[:0]
	for _, w := range m.watches {
		if w.Start != start {
			kept = append(kept, w)
		}
	}
	m.watches = kept
	if len(m.watches) == 0 {
		m.watches = nil
	}
}

// Watches returns a copy of the current watchpoints in insertion order.
func (m *Machine) Watches() []Watch {
	out := make([]Watch, len(m.watches))
	copy(out, m.watches)
	return out
}

// RunDebug runs like RunTstates but stops early when the next instruction
// address is a breakpoint or when an instruction touches a watchpoint.
// When called with the CPU already at a breakpoint it executes that
// instruction first (resume semantics), so repeated RunDebug calls always
// make progress. Runs that never stop early consume T-states exactly as
// RunTstates would.
func (m *Machine) RunDebug(n uint64) Stop {
	target := m.tstates + n
	first := true
	for m.tstates < target {
		pc := m.cpu.State().PC
		if !first {
			if _, ok := m.breakpoints[pc]; ok {
				return Stop{Reason: StopBreakpoint, PC: pc}
			}
		}
		first = false
		m.watchHit = false
		m.StepInstruction()
		if m.watchHit {
			s := m.watchStop
			s.Reason = StopWatch
			s.PC = m.cpu.State().PC
			return s
		}
	}
	m.watchHit = false
	return Stop{Reason: StopBudget, PC: m.cpu.State().PC}
}

// checkWatch is called from Tick for every T-state while watchpoints
// exist. It latches the first watched CPU data access of the current
// instruction; RunDebug reads and clears the latch at the next
// instruction boundary. Opcode fetches (M1) and non-memory cycles never
// match, and the video refresh fetch bypasses the bus pins entirely.
func (m *Machine) checkWatch(addr uint16, data int16, pins z80.Pins) {
	if m.watchHit || pins&z80.MREQ == 0 || pins&z80.M1 != 0 {
		return
	}
	var write bool
	switch {
	case pins&z80.WR != 0:
		write = true
	case pins&z80.RD != 0:
		write = false
	default:
		return
	}
	for _, w := range m.watches {
		if addr < w.Start || addr > w.End {
			continue
		}
		if write && w.Kind&WatchWrite == 0 {
			continue
		}
		if !write && w.Kind&WatchRead == 0 {
			continue
		}
		b := byte(data)
		if !write {
			// The read value rides the bus one T-state later; MemRead is
			// side-effect free and returns the same byte the CPU will see.
			b = m.MemRead(addr)
		}
		m.watchStop = Stop{Addr: addr, Write: write, Data: b}
		m.watchHit = true
		return
	}
}
