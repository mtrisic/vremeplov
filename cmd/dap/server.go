package main

import (
	"bufio"
	"fmt"
	"io"
	"sync"

	"github.com/google/go-dap"
)

// server is one DAP session: it reads requests off the transport,
// dispatches them (marshaling machine work onto the engine goroutine
// once a debuggee is launched), and serializes every outgoing message
// through one mutex-guarded writer.
type server struct {
	w          io.Writer
	wmu        sync.Mutex
	seq        int
	eng        *engine // nil until launch succeeds
	screenAddr string  // --screen flag; launch arg overrides
	quit       chan struct{}
}

func newServer(w io.Writer, screenAddr string) *server {
	return &server{w: w, screenAddr: screenAddr, quit: make(chan struct{})}
}

// loop reads and dispatches until the client disconnects or the
// transport closes.
func (s *server) loop(r *bufio.Reader) error {
	for {
		msg, err := dap.ReadProtocolMessage(r)
		if err != nil {
			s.shutdown()
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.dispatch(msg)
		select {
		case <-s.quit:
			return nil
		default:
		}
	}
}

// shutdown stops the engine (and its screen server) if one is running.
func (s *server) shutdown() {
	if s.eng != nil {
		s.eng.stop()
		s.eng = nil
	}
}

// send assigns the outgoing sequence number and writes one message.
func (s *server) send(msg dap.Message) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.seq++
	switch m := msg.(type) {
	case dap.ResponseMessage:
		m.GetResponse().Seq = s.seq
		m.GetResponse().Type = "response"
	case dap.EventMessage:
		m.GetEvent().Seq = s.seq
		m.GetEvent().Type = "event"
	}
	// A write error means the client is gone; the read loop will see
	// it too, so it is safe to drop here.
	_ = dap.WriteProtocolMessage(s.w, msg)
}

// respond fills the response envelope for req and sends it. resp must
// be a pointer to a concrete *Response-embedding type.
func (s *server) respond(req *dap.Request, resp dap.ResponseMessage) {
	r := resp.GetResponse()
	r.RequestSeq = req.Seq
	r.Command = req.Command
	r.Success = true
	s.send(resp)
}

// fail sends an error response for req.
func (s *server) fail(req *dap.Request, format string, args ...any) {
	resp := &dap.ErrorResponse{}
	resp.RequestSeq = req.Seq
	resp.Command = req.Command
	resp.Success = false
	resp.Message = fmt.Sprintf(format, args...)
	s.send(resp)
}

// output sends an output event (console category) — the adapter's
// channel for human-facing notes like the screen-view URL.
func (s *server) output(format string, args ...any) {
	ev := &dap.OutputEvent{}
	ev.Event.Event = "output"
	ev.Body.Category = "console"
	ev.Body.Output = fmt.Sprintf(format, args...) + "\n"
	s.send(ev)
}

// dispatch routes one incoming message.
func (s *server) dispatch(msg dap.Message) {
	switch req := msg.(type) {
	case *dap.InitializeRequest:
		resp := &dap.InitializeResponse{Body: capabilities()}
		s.respond(&req.Request, resp)

	case *dap.LaunchRequest:
		s.onLaunch(req)

	case *dap.AttachRequest:
		s.fail(&req.Request, "attach is not supported: vremeplov-dap hosts the machine itself — use a launch configuration")

	case *dap.ConfigurationDoneRequest:
		s.onConfigurationDone(req)

	case *dap.SetBreakpointsRequest:
		s.onSetBreakpoints(req)

	case *dap.SetInstructionBreakpointsRequest:
		s.onSetInstructionBreakpoints(req)

	case *dap.ThreadsRequest:
		resp := &dap.ThreadsResponse{Body: dap.ThreadsResponseBody{
			Threads: []dap.Thread{{Id: threadID, Name: "Z80"}},
		}}
		s.respond(&req.Request, resp)

	case *dap.ContinueRequest:
		s.onContinue(req)
	case *dap.PauseRequest:
		s.onPause(req)
	case *dap.NextRequest:
		s.onNext(req)
	case *dap.StepInRequest:
		s.onStepIn(req)
	case *dap.StepOutRequest:
		s.onStepOut(req)
	case *dap.StepBackRequest:
		s.onStepBack(req)
	case *dap.ReverseContinueRequest:
		s.onReverseContinue(req)

	case *dap.StackTraceRequest:
		s.onStackTrace(req)
	case *dap.ScopesRequest:
		s.onScopes(req)
	case *dap.VariablesRequest:
		s.onVariables(req)
	case *dap.SetVariableRequest:
		s.onSetVariable(req)
	case *dap.ReadMemoryRequest:
		s.onReadMemory(req)
	case *dap.WriteMemoryRequest:
		s.onWriteMemory(req)
	case *dap.DisassembleRequest:
		s.onDisassemble(req)
	case *dap.EvaluateRequest:
		s.onEvaluate(req)
	case *dap.RestartRequest:
		s.onRestart(req)

	case *dap.DisconnectRequest:
		s.shutdown()
		s.respond(&req.Request, &dap.DisconnectResponse{})
		close(s.quit)

	case *dap.TerminateRequest:
		s.shutdown()
		s.respond(&req.Request, &dap.TerminateResponse{})
		ev := &dap.TerminatedEvent{}
		ev.Event.Event = "terminated"
		s.send(ev)

	default:
		if rm, ok := msg.(dap.RequestMessage); ok {
			s.fail(rm.GetRequest(), "unsupported request %q", rm.GetRequest().Command)
		}
	}
}

// threadID is the single Z80 "thread".
const threadID = 1

// capabilities is what the adapter promises. Data breakpoints are
// deliberately absent in v1 (core watchpoints are reachable through
// the debug console's monitor REPL: `w ADDR[-END] [r|w|rw]`).
func capabilities() dap.Capabilities {
	return dap.Capabilities{
		SupportsConfigurationDoneRequest: true,
		SupportsInstructionBreakpoints:   true,
		SupportsDisassembleRequest:       true,
		SupportsReadMemoryRequest:        true,
		SupportsWriteMemoryRequest:       true,
		SupportsStepBack:                 true,
		SupportsSetVariable:              true,
		SupportsRestartRequest:           true,
		SupportsTerminateRequest:         true,
		SupportsSteppingGranularity:      true,
	}
}

// requireEngine fetches the launched engine or fails the request.
func (s *server) requireEngine(req *dap.Request) *engine {
	if s.eng == nil {
		s.fail(req, "no program launched")
		return nil
	}
	return s.eng
}
