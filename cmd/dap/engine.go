package main

import (
	"sync"
	"time"

	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/monitor"
)

// engine owns the Machine. Exactly one goroutine (loop) touches it:
// request handlers marshal work through do(), and while the debuggee
// runs, the loop executes real-time-paced one-frame RunDebug slices —
// so the program behaves like hardware, the screen view stays live,
// and pause/breakpoint requests land between slices (≤20 ms away).
type engine struct {
	m    *core.Machine
	mon  *monitor.Session
	sld  *SLD // nil without a source map
	args launchArgs

	cmds  chan func()
	quitc chan struct{}
	wg    sync.WaitGroup

	running bool

	// tempBPs are the adapter's own transient breakpoints (step
	// targets, entry stops): address → the DAP stop reason to report.
	// tempOwned tracks which of them we added to the machine (a user
	// breakpoint at the same address stays).
	tempBPs   map[uint16]string
	tempOwned map[uint16]bool

	// user breakpoints: per-source verified line breakpoints and
	// instruction breakpoints, reconciled as a union onto the machine.
	srcBPs  map[string][]srcBP
	instBPs []uint16
	bpIDs   map[uint16]int // machine address → DAP breakpoint id
	nextID  int

	// stopStack records the T-state of every reported stop, for
	// reverseContinue (bounce back through past stops).
	stopStack []uint64

	// entryAddr is where the debuggee starts (bin entry PC / tape
	// entry breakpoint), for stack-frame naming before SLD kicks in.
	entryAddr uint16

	// event plumbing back to the DAP writer (set by the server).
	onStopped   func(reason, description string, hitIDs []int)
	onContinued func()

	screen *screenServer
}

// srcBP is one source-line breakpoint after SLD resolution.
type srcBP struct {
	id       int
	line     int
	addr     uint16
	verified bool
}

func newEngine(m *core.Machine, args launchArgs) *engine {
	return &engine{
		m:         m,
		mon:       monitor.New(m),
		args:      args,
		cmds:      make(chan func()),
		quitc:     make(chan struct{}),
		tempBPs:   map[uint16]string{},
		tempOwned: map[uint16]bool{},
		srcBPs:    map[string][]srcBP{},
		bpIDs:     map[uint16]int{},
	}
}

// start launches the engine goroutine.
func (e *engine) start() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.loop()
	}()
}

// stop tears the engine down (idempotent).
func (e *engine) stop() {
	select {
	case <-e.quitc:
	default:
		close(e.quitc)
	}
	e.wg.Wait()
	if e.screen != nil {
		e.screen.stop()
		e.screen = nil
	}
}

// do runs f on the engine goroutine and waits for it — the only safe
// way for request handlers to touch the machine.
func (e *engine) do(f func()) {
	done := make(chan struct{})
	select {
	case e.cmds <- func() { f(); close(done) }:
		<-done
	case <-e.quitc:
	}
}

// slicePeriod paces the run loop at the machine's 50 Hz frame rate.
const slicePeriod = 20 * time.Millisecond

func (e *engine) loop() {
	tick := time.NewTicker(slicePeriod)
	defer tick.Stop()
	for {
		if !e.running {
			select {
			case f := <-e.cmds:
				f()
			case <-e.quitc:
				return
			}
			continue
		}
		select {
		case f := <-e.cmds:
			f()
		case <-e.quitc:
			return
		case <-tick.C:
			// One frame per slice; RunDebug resumes correctly from a
			// breakpoint (it steps first), so slicing is transparent.
			st := e.m.RunDebug(core.TstatesPerFrame)
			if st.Reason != core.StopBudget {
				e.reportStop(st)
			}
		}
	}
}

// resume flips to running (engine goroutine only).
func (e *engine) resume() {
	e.clearTemps()
	e.running = true
	if e.onContinued != nil {
		e.onContinued()
	}
}

// pauseNow stops between slices and reports (engine goroutine only).
func (e *engine) pauseNow() {
	if !e.running {
		return
	}
	e.running = false
	e.clearTemps()
	e.recordStop()
	e.onStopped("pause", "", nil)
}

// reportStop classifies a RunDebug stop and emits the stopped event
// (engine goroutine only).
func (e *engine) reportStop(st core.Stop) {
	e.running = false
	e.recordStop()
	switch st.Reason {
	case core.StopBreakpoint:
		if reason, ok := e.tempBPs[st.PC]; ok {
			e.clearTemps()
			e.onStopped(reason, "", nil)
			return
		}
		e.clearTemps()
		var hits []int
		if id, ok := e.bpIDs[st.PC]; ok {
			hits = []int{id}
		}
		e.onStopped("breakpoint", "", hits)
	case core.StopWatch:
		e.clearTemps()
		e.onStopped("data breakpoint", monitor.FormatStop(st), nil)
	default:
		e.onStopped("pause", "", nil)
	}
}

// recordStop pushes the stop T-state for reverseContinue. Entries at
// or past the current time (stale after a rewind) are dropped first.
func (e *engine) recordStop() {
	now := e.m.Tstates()
	for len(e.stopStack) > 0 && e.stopStack[len(e.stopStack)-1] >= now {
		e.stopStack = e.stopStack[:len(e.stopStack)-1]
	}
	e.stopStack = append(e.stopStack, now)
}

// addTempBP arms a transient breakpoint that reports as reason when
// hit (engine goroutine only).
func (e *engine) addTempBP(addr uint16, reason string) {
	if _, mine := e.tempBPs[addr]; mine {
		return
	}
	e.tempBPs[addr] = reason
	if !e.hasUserBP(addr) {
		e.m.AddBreakpoint(addr)
		e.tempOwned[addr] = true
	}
}

// clearTemps drops every transient breakpoint — any stop cancels a
// pending step (engine goroutine only).
func (e *engine) clearTemps() {
	for addr := range e.tempBPs {
		if e.tempOwned[addr] {
			e.m.RemoveBreakpoint(addr)
		}
	}
	e.tempBPs = map[uint16]string{}
	e.tempOwned = map[uint16]bool{}
}

// hasUserBP reports whether a user breakpoint (source or instruction)
// wants addr.
func (e *engine) hasUserBP(addr uint16) bool {
	for _, bps := range e.srcBPs {
		for _, b := range bps {
			if b.verified && b.addr == addr {
				return true
			}
		}
	}
	for _, a := range e.instBPs {
		if a == addr {
			return true
		}
	}
	return false
}

// reconcileBreakpoints syncs the machine's breakpoint set to the union
// of user breakpoints plus the adapter's transient ones (engine
// goroutine only).
func (e *engine) reconcileBreakpoints() {
	want := map[uint16]bool{}
	for _, bps := range e.srcBPs {
		for _, b := range bps {
			if b.verified {
				want[b.addr] = true
			}
		}
	}
	for _, a := range e.instBPs {
		want[a] = true
	}
	for a := range e.tempBPs {
		want[a] = true
	}
	have := map[uint16]bool{}
	for _, a := range e.m.Breakpoints() {
		have[a] = true
	}
	for a := range want {
		if !have[a] {
			e.m.AddBreakpoint(a)
		}
	}
	for a := range have {
		if !want[a] {
			e.m.RemoveBreakpoint(a)
		}
	}
}

// memAt is a side-effect-free CPU-eye reader for the disassembler.
func (e *engine) memAt(a uint16) byte { return e.m.MemRead(a) }

// memRead16 reads a little-endian word (for the stepOut heuristic).
func (e *engine) memRead16(a uint16) uint16 {
	return uint16(e.m.MemRead(a)) | uint16(e.m.MemRead(a+1))<<8
}
