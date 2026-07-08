package core

import "fmt"

// Rewind history: a ring of periodic snapshots plus a journal of every
// external input between them. Because the core is deterministic —
// same state + same T-state-stamped inputs ⇒ byte-identical execution —
// restoring the nearest snapshot at or before a target and replaying the
// journaled inputs forward reproduces the machine at that T-state
// exactly. That is RewindTo; StepBack uses it to land precisely one (or
// n) instruction boundaries back.
//
// Semantics of a moment in time: "the machine at boundary T" includes
// every input applied at T — mutations always precede the automatic
// snapshot taken at the same boundary (snapshots happen inside
// StepInstruction; frontend calls happen between them), and replay
// applies journal entries with t ≤ target the same way.
//
// History is infrastructure around snapshots, not machine state: it is
// itself excluded from Snapshot/Restore, and a manual Restore while
// history is enabled rebases it (the old timeline is unreachable).

// Journal event kinds.
const (
	histKey = iota
	histQueue
	histInsertTape
	histPlayTape
	histStopTape
	histReset
	histLoadBinary
)

// histEvent is one journaled external input at boundary t.
type histEvent struct {
	t     uint64
	kind  int
	key   KeyEvent      // histKey
	batch []KeyEvent    // histQueue
	tape  *TapeSchedule // histInsertTape (shared; schedules are immutable)
	addr  uint16        // histLoadBinary
	data  []byte        // histLoadBinary
}

// journal records an external input at the current T-state. No-op when
// history is off or a replay is in progress (replay re-applies events
// through the same entry points).
func (m *Machine) journal(e histEvent) {
	if !m.histEnabled || m.histReplaying {
		return
	}
	e.t = m.tstates
	m.histJournal = append(m.histJournal, e)
}

// EnableHistory starts recording rewind history: an automatic snapshot
// every interval T-states (taken at instruction boundaries), keeping at
// most depth snapshots. Memory cost is ≈250 KB per snapshot (dominated
// by the two framebuffers). Any previous history is discarded and the
// current state becomes the oldest reachable point.
func (m *Machine) EnableHistory(interval uint64, depth int) {
	if interval == 0 {
		interval = TstatesPerFrame
	}
	if depth < 1 {
		depth = 1
	}
	m.histEnabled = true
	m.histInterval = interval
	m.histDepth = depth
	m.histJournal = nil
	m.histRing = []*Snapshot{m.Snapshot()}
	m.histNextAt = m.tstates + interval
}

// DisableHistory stops recording and releases all history.
func (m *Machine) DisableHistory() {
	m.histEnabled = false
	m.histRing = nil
	m.histJournal = nil
}

// HistoryEnabled reports whether rewind history is being recorded.
func (m *Machine) HistoryEnabled() bool { return m.histEnabled }

// HistorySpan returns the reachable rewind range: the oldest recorded
// T-state and the current one. ok is false when history is off.
func (m *Machine) HistorySpan() (oldest, newest uint64, ok bool) {
	if !m.histEnabled || len(m.histRing) == 0 {
		return 0, 0, false
	}
	return m.histRing[0].Tstates, m.tstates, true
}

// histSnapshot appends a ring snapshot and prunes history beyond depth.
func (m *Machine) histSnapshot() {
	m.histRing = append(m.histRing, m.Snapshot())
	if len(m.histRing) > m.histDepth {
		drop := len(m.histRing) - m.histDepth
		m.histRing = append(m.histRing[:0], m.histRing[drop:]...)
		// Journal entries at or before the new oldest snapshot are baked
		// into it and unreachable — prune them.
		oldest := m.histRing[0].Tstates
		i := 0
		for i < len(m.histJournal) && m.histJournal[i].t <= oldest {
			i++
		}
		m.histJournal = append(m.histJournal[:0], m.histJournal[i:]...)
	}
	m.histNextAt = m.tstates + m.histInterval
}

// applyHistEvent re-applies a journaled input during replay.
func (m *Machine) applyHistEvent(e histEvent) {
	switch e.kind {
	case histKey:
		if e.key.Down {
			m.PressKey(e.key.Key)
		} else {
			m.ReleaseKey(e.key.Key)
		}
	case histQueue:
		m.QueueKeyEvents(e.batch...)
	case histInsertTape:
		m.InsertTape(e.tape)
	case histPlayTape:
		m.PlayTape()
	case histStopTape:
		m.StopTape()
	case histReset:
		m.Reset()
	case histLoadBinary:
		m.LoadBinary(e.addr, e.data) //nolint:errcheck // succeeded originally
	}
}

// replayTo restores snap and re-executes forward to target, re-applying
// journal entries with snap.Tstates < t ≤ target at their boundaries.
// When keep > 0 it records the last keep instruction-boundary T-states
// visited strictly before target (for StepBack).
func (m *Machine) replayTo(snap *Snapshot, target uint64, keep int) ([]uint64, error) {
	m.histReplaying = true
	defer func() { m.histReplaying = false }()
	if err := m.Restore(snap); err != nil {
		return nil, err
	}
	ji := 0
	var bounds []uint64
	for {
		for ji < len(m.histJournal) && m.histJournal[ji].t <= m.tstates {
			e := m.histJournal[ji]
			ji++
			if e.t <= snap.Tstates || e.t > target {
				continue
			}
			m.applyHistEvent(e)
		}
		if m.tstates >= target {
			break
		}
		if keep > 0 {
			if len(bounds) == keep {
				copy(bounds, bounds[1:])
				bounds[keep-1] = m.tstates
			} else {
				bounds = append(bounds, m.tstates)
			}
		}
		m.StepInstruction()
	}
	return bounds, nil
}

// RewindTo runs the machine back (or, within recorded history, forward)
// to the instruction boundary of target: the nearest ring snapshot at or
// before target is restored and the journaled inputs replay forward —
// byte-identical to the original run by the determinism guarantee.
// History after target is discarded: rewinding starts a new timeline.
func (m *Machine) RewindTo(target uint64) error {
	if !m.histEnabled || len(m.histRing) == 0 {
		return fmt.Errorf("core: RewindTo: history is not enabled")
	}
	if target > m.tstates {
		return fmt.Errorf("core: RewindTo: target %d is in the future (now %d)", target, m.tstates)
	}
	if target < m.histRing[0].Tstates {
		return fmt.Errorf("core: RewindTo: target %d is before recorded history (oldest %d)",
			target, m.histRing[0].Tstates)
	}
	idx := 0
	for i, s := range m.histRing {
		if s.Tstates > target {
			break
		}
		idx = i
	}
	if _, err := m.replayTo(m.histRing[idx], target, 0); err != nil {
		return err
	}

	// Truncate the abandoned future: snapshots and inputs past target,
	// and any recorder pulses captured there.
	m.histRing = m.histRing[:idx+1]
	j := 0
	for j < len(m.histJournal) && m.histJournal[j].t <= target {
		j++
	}
	m.histJournal = m.histJournal[:j]
	for len(m.recStarts) > 0 && m.recStarts[len(m.recStarts)-1] > target {
		m.recStarts = m.recStarts[:len(m.recStarts)-1]
	}
	m.histNextAt = m.tstates + m.histInterval
	return nil
}

// Rewind steps dt T-states into the past, clamped to the oldest recorded
// point.
func (m *Machine) Rewind(dt uint64) error {
	if !m.histEnabled || len(m.histRing) == 0 {
		return fmt.Errorf("core: Rewind: history is not enabled")
	}
	target := uint64(0)
	if m.tstates > dt {
		target = m.tstates - dt
	}
	if oldest := m.histRing[0].Tstates; target < oldest {
		target = oldest
	}
	return m.RewindTo(target)
}

// StepBack lands the machine exactly n instruction boundaries before the
// current one — the debugger's reverse-step. It reports an error when
// history is off or does not reach back far enough.
func (m *Machine) StepBack(n int) error {
	if n < 1 {
		return fmt.Errorf("core: StepBack: count must be ≥ 1")
	}
	if !m.histEnabled || len(m.histRing) == 0 {
		return fmt.Errorf("core: StepBack: history is not enabled")
	}
	now := m.tstates
	// Newest snapshot strictly before now; walk older until the replayed
	// segment contains at least n boundaries.
	idx := -1
	for i, s := range m.histRing {
		if s.Tstates >= now {
			break
		}
		idx = i
	}
	for ; idx >= 0; idx-- {
		bounds, err := m.replayTo(m.histRing[idx], now, n)
		if err != nil {
			return err
		}
		if len(bounds) >= n {
			return m.RewindTo(bounds[len(bounds)-n])
		}
		if idx == 0 {
			return fmt.Errorf("core: StepBack: only %d boundaries in recorded history, need %d", len(bounds), n)
		}
	}
	return fmt.Errorf("core: StepBack: no history before the current instruction")
}

// HistoryRebase makes the current state the new history baseline,
// discarding the ring and journal. Call it after mutating the machine
// outside its journaled entry points (e.g. writing CPU registers through
// CPU().SetState).
func (m *Machine) HistoryRebase() {
	if !m.histEnabled {
		return
	}
	m.histJournal = nil
	m.histRing = []*Snapshot{m.Snapshot()}
	m.histNextAt = m.tstates + m.histInterval
}
