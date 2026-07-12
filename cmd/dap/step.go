package main

import (
	"fmt"
	"strings"

	"github.com/google/go-dap"
	"github.com/mtrisic/gozilog/z80/disasm"
	"github.com/mtrisic/vremeplov/core"
	"github.com/mtrisic/vremeplov/core/monitor"
)

// Stepping semantics. Instruction granularity is the base layer; with
// an SLD map and "statement"/"line" granularity, steps repeat until
// the mapped source line changes (capped — a runaway loop inside one
// line must not hang the adapter).

// lineStepCap bounds synchronous line-granularity stepping, in
// T-states (the monitor's run budget: 5 emulated seconds).
const lineStepCap = monitor.RunBudget

// needsRunTo classifies the instruction at PC for step-over: only
// instructions that transfer control somewhere they'll come back from
// (or block) get a run-to-next breakpoint; everything else — taken
// jumps included — is a plain single step. A blind breakpoint at
// PC+len would run away forever on a taken JP/JR.
func needsRunTo(text string) bool {
	op, _, _ := strings.Cut(text, " ")
	switch op {
	case "CALL", "RST", "DJNZ", "HALT",
		"LDIR", "LDDR", "CPIR", "CPDR",
		"INIR", "INDR", "OTIR", "OTDR":
		return true
	}
	return false
}

func (s *server) onContinue(req *dap.ContinueRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	eng.do(eng.resume)
	s.respond(&req.Request, &dap.ContinueResponse{
		Body: dap.ContinueResponseBody{AllThreadsContinued: true},
	})
}

func (s *server) onPause(req *dap.PauseRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	eng.do(eng.pauseNow)
	s.respond(&req.Request, &dap.PauseResponse{})
}

// lineGranularity reports whether a stepping request should work in
// source lines (needs an SLD map).
func (e *engine) lineGranularity(granularity dap.SteppingGranularity) bool {
	return e.sld != nil && granularity != "instruction"
}

// stepOnce executes one instruction-level "next": plain step, or an
// async run-to for call-like instructions. Returns true when the step
// completed synchronously (stopped event still owed by the caller).
func (e *engine) stepOnce() bool {
	in := disasm.Decode(e.memAt, e.pc())
	if needsRunTo(in.Text) {
		e.addTempBP(in.Addr+uint16(in.Len), "step")
		e.running = true
		if e.onContinued != nil {
			e.onContinued()
		}
		return false
	}
	e.m.StepInstruction()
	return true
}

// stepLine steps synchronously until the mapped source line changes
// (or the map is left, a foreign stop hits, or the cap runs out).
// Returns the stop to report; done=false means an async run-to was
// armed instead (call-like instruction mid-line).
func (e *engine) stepLine() {
	startFile, startLine, startOK := e.sld.LineFor(e.pc())
	var spent uint64
	for spent < lineStepCap {
		in := disasm.Decode(e.memAt, e.pc())
		if needsRunTo(in.Text) {
			// Run the subroutine out synchronously (capped) so the
			// whole line completes as one step.
			target := in.Addr + uint16(in.Len)
			e.addTempBP(target, "step")
			st := e.m.RunDebug(lineStepCap - spent)
			hitTemp := st.Reason == core.StopBreakpoint && st.PC == target
			if !hitTemp && st.Reason != core.StopBudget {
				// A user breakpoint or watch fired mid-line: report
				// that instead of the step.
				e.reportStop(st)
				return
			}
			e.clearTemps()
			spent = lineStepCap // budget bookkeeping is coarse: one run-out per line step
		} else {
			spent += uint64(e.m.StepInstruction())
		}
		file, line, ok := e.sld.LineFor(e.pc())
		if !startOK || !ok || file != startFile || line != startLine {
			break
		}
	}
	e.running = false
	e.recordStop()
	e.onStopped("step", "", nil)
}

func (s *server) onNext(req *dap.NextRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	eng.do(func() {
		if eng.lineGranularity(req.Arguments.Granularity) {
			eng.stepLine()
			return
		}
		if eng.stepOnce() {
			eng.recordStop()
			eng.onStopped("step", "", nil)
		}
	})
	s.respond(&req.Request, &dap.NextResponse{})
}

func (s *server) onStepIn(req *dap.StepInRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	eng.do(func() {
		if eng.lineGranularity(req.Arguments.Granularity) {
			// Step-in by line: single instructions until the line
			// changes — naturally descends into calls.
			startFile, startLine, startOK := eng.sld.LineFor(eng.pc())
			var spent uint64
			for spent < lineStepCap {
				spent += uint64(eng.m.StepInstruction())
				file, line, ok := eng.sld.LineFor(eng.pc())
				if !startOK || !ok || file != startFile || line != startLine {
					break
				}
			}
		} else {
			eng.m.StepInstruction()
		}
		eng.recordStop()
		eng.onStopped("step", "", nil)
	})
	s.respond(&req.Request, &dap.StepInResponse{})
}

// onStepOut runs to the return address currently at (SP) — the
// standard 8-bit heuristic: exact immediately after stepping into a
// routine, approximate once the routine has pushed to the stack.
func (s *server) onStepOut(req *dap.StepOutRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	eng.do(func() {
		ret := eng.memRead16(eng.m.CPU().State().SP)
		eng.addTempBP(ret, "step")
		eng.running = true
		if eng.onContinued != nil {
			eng.onContinued()
		}
	})
	s.respond(&req.Request, &dap.StepOutResponse{})
}

// onStepBack reverse-steps on the rewind history.
func (s *server) onStepBack(req *dap.StepBackRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	var stepErr error
	eng.do(func() {
		if eng.lineGranularity(req.Arguments.Granularity) {
			startFile, startLine, startOK := eng.sld.LineFor(eng.pc())
			for i := 0; i < 32; i++ { // each StepBack replays a snapshot — cap hard
				if stepErr = eng.m.StepBack(1); stepErr != nil {
					break
				}
				file, line, ok := eng.sld.LineFor(eng.pc())
				if !startOK || !ok || file != startFile || line != startLine {
					break
				}
			}
		} else {
			stepErr = eng.m.StepBack(1)
		}
		if stepErr == nil {
			eng.onStopped("step", fmt.Sprintf("rewound to T=%d", eng.m.Tstates()), nil)
		}
	})
	if stepErr != nil {
		s.fail(&req.Request, "step back: %v", stepErr)
		return
	}
	s.respond(&req.Request, &dap.StepBackResponse{})
}

// onReverseContinue bounces back through recorded stops: pop the most
// recent past stop and rewind exactly to it; with none left, rewind a
// couple of seconds (clamped to the oldest history).
func (s *server) onReverseContinue(req *dap.ReverseContinueRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	var revErr error
	eng.do(func() {
		now := eng.m.Tstates()
		for len(eng.stopStack) > 0 && eng.stopStack[len(eng.stopStack)-1] >= now {
			eng.stopStack = eng.stopStack[:len(eng.stopStack)-1]
		}
		if len(eng.stopStack) > 0 {
			target := eng.stopStack[len(eng.stopStack)-1]
			eng.stopStack = eng.stopStack[:len(eng.stopStack)-1]
			revErr = eng.m.RewindTo(target)
		} else {
			revErr = eng.m.Rewind(100 * core.TstatesPerFrame)
		}
		if revErr == nil {
			eng.onStopped("step", fmt.Sprintf("rewound to T=%d", eng.m.Tstates()), nil)
		}
	})
	if revErr != nil {
		s.fail(&req.Request, "reverse continue: %v", revErr)
		return
	}
	s.respond(&req.Request, &dap.ReverseContinueResponse{})
}

// pc is the current program counter (engine goroutine only).
func (e *engine) pc() uint16 { return e.m.CPU().State().PC }
