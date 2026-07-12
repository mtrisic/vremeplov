package main

import (
	"fmt"
	"strconv"

	"github.com/google/go-dap"
)

// onSetBreakpoints handles source breakpoints: DAP semantics are
// replace-all-for-this-source. Lines resolve to addresses through the
// SLD map; a line with no mapped instruction stays unverified (shown
// hollow in the editor).
func (s *server) onSetBreakpoints(req *dap.SetBreakpointsRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	path := req.Arguments.Source.Path
	out := make([]dap.Breakpoint, 0, len(req.Arguments.Breakpoints))
	eng.do(func() {
		bps := make([]srcBP, 0, len(req.Arguments.Breakpoints))
		for _, want := range req.Arguments.Breakpoints {
			eng.nextID++
			bp := srcBP{id: eng.nextID, line: want.Line}
			msg := ""
			if eng.sld == nil {
				msg = `no "sld" file configured — source breakpoints need a sjasmplus --sld map`
			} else if addr, ok := eng.sld.AddrFor(path, want.Line); ok {
				bp.addr, bp.verified = addr, true
				msg = fmt.Sprintf("0x%04X", addr)
			} else {
				msg = "no machine code maps to this line"
			}
			bps = append(bps, bp)
			out = append(out, dap.Breakpoint{
				Id:       bp.id,
				Verified: bp.verified,
				Line:     want.Line,
				Message:  msg,
			})
			if bp.verified {
				eng.bpIDs[bp.addr] = bp.id
			}
		}
		eng.srcBPs[path] = bps
		eng.reconcileBreakpoints()
	})
	s.respond(&req.Request, &dap.SetBreakpointsResponse{
		Body: dap.SetBreakpointsResponseBody{Breakpoints: out},
	})
}

// onSetInstructionBreakpoints: address-level breakpoints from the
// disassembly view — always verifiable. Replace-all semantics.
func (s *server) onSetInstructionBreakpoints(req *dap.SetInstructionBreakpointsRequest) {
	eng := s.requireEngine(&req.Request)
	if eng == nil {
		return
	}
	out := make([]dap.Breakpoint, 0, len(req.Arguments.Breakpoints))
	eng.do(func() {
		addrs := make([]uint16, 0, len(req.Arguments.Breakpoints))
		for _, want := range req.Arguments.Breakpoints {
			ref, err := strconv.ParseUint(want.InstructionReference, 0, 64)
			addr := uint16(int64(ref) + int64(want.Offset))
			eng.nextID++
			bp := dap.Breakpoint{Id: eng.nextID, Line: 0}
			if err != nil {
				bp.Verified = false
				bp.Message = fmt.Sprintf("bad instruction reference %q", want.InstructionReference)
			} else {
				bp.Verified = true
				bp.InstructionReference = fmt.Sprintf("0x%04X", addr)
				addrs = append(addrs, addr)
				eng.bpIDs[addr] = eng.nextID
			}
			out = append(out, bp)
		}
		eng.instBPs = addrs
		eng.reconcileBreakpoints()
	})
	s.respond(&req.Request, &dap.SetInstructionBreakpointsResponse{
		Body: dap.SetInstructionBreakpointsResponseBody{Breakpoints: out},
	})
}
