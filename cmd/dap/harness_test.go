package main

// In-process DAP client harness: the adapter serves one end of a
// net.Pipe, the tests drive the other with real protocol messages.
// The machine is deterministic, so assertions are exact.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-dap"
)

type client struct {
	t      *testing.T
	conn   net.Conn
	w      *bufio.Writer
	seq    int
	resps  chan dap.ResponseMessage
	events chan dap.EventMessage
}

// startClient wires a server and returns the test client.
func startClient(t *testing.T) *client {
	t.Helper()
	a, b := net.Pipe()
	go serve(a, "")
	c := &client{
		t:      t,
		conn:   b,
		w:      bufio.NewWriter(b),
		resps:  make(chan dap.ResponseMessage, 64),
		events: make(chan dap.EventMessage, 64),
	}
	go func() {
		r := bufio.NewReader(b)
		for {
			msg, err := dap.ReadProtocolMessage(r)
			if err != nil {
				close(c.resps)
				close(c.events)
				return
			}
			switch m := msg.(type) {
			case dap.ResponseMessage:
				c.resps <- m
			case dap.EventMessage:
				c.events <- m
			}
		}
	}()
	t.Cleanup(func() { b.Close(); a.Close() })
	return c
}

// send writes one request.
func (c *client) send(msg dap.RequestMessage) {
	c.t.Helper()
	c.seq++
	msg.GetRequest().Seq = c.seq
	msg.GetRequest().Type = "request"
	if err := dap.WriteProtocolMessage(c.w, msg); err != nil {
		c.t.Fatalf("write: %v", err)
	}
	if err := c.w.Flush(); err != nil {
		c.t.Fatalf("flush: %v", err)
	}
}

const harnessTimeout = 15 * time.Second

// resp waits for the response to command and requires success.
func (c *client) resp(command string) dap.ResponseMessage {
	c.t.Helper()
	m := c.respAny(command)
	if !m.GetResponse().Success {
		c.t.Fatalf("%s failed: %s", command, m.GetResponse().Message)
	}
	return m
}

// respErr waits for the response and requires failure.
func (c *client) respErr(command string) dap.ResponseMessage {
	c.t.Helper()
	m := c.respAny(command)
	if m.GetResponse().Success {
		c.t.Fatalf("%s unexpectedly succeeded", command)
	}
	return m
}

func (c *client) respAny(command string) dap.ResponseMessage {
	c.t.Helper()
	deadline := time.After(harnessTimeout)
	for {
		select {
		case m, ok := <-c.resps:
			if !ok {
				c.t.Fatalf("connection closed awaiting %s response", command)
			}
			if m.GetResponse().Command == command {
				return m
			}
			// Responses always arrive in request order; anything else
			// here is a harness bug.
			c.t.Fatalf("awaiting %s response, got %s", command, m.GetResponse().Command)
		case <-deadline:
			c.t.Fatalf("timeout awaiting %s response", command)
		}
	}
}

// event waits for the next event with the given name, skipping others
// (output events etc.).
func (c *client) event(name string) dap.EventMessage {
	c.t.Helper()
	deadline := time.After(harnessTimeout)
	for {
		select {
		case m, ok := <-c.events:
			if !ok {
				c.t.Fatalf("connection closed awaiting %s event", name)
			}
			if m.GetEvent().Event == name {
				return m
			}
		case <-deadline:
			c.t.Fatalf("timeout awaiting %s event", name)
		}
	}
}

// stopped waits for a stopped event and returns its body.
func (c *client) stopped() dap.StoppedEventBody {
	c.t.Helper()
	ev := c.event("stopped").(*dap.StoppedEvent)
	return ev.Body
}

// --- request builders -------------------------------------------------

func newRequest(command string) dap.Request {
	return dap.Request{Command: command}
}

func (c *client) initialize() {
	c.t.Helper()
	c.send(&dap.InitializeRequest{Request: newRequest("initialize")})
	c.resp("initialize")
}

// launchMap sends a launch request with the given arguments.
func (c *client) launch(args map[string]any) {
	c.t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		c.t.Fatal(err)
	}
	c.send(&dap.LaunchRequest{Request: newRequest("launch"), Arguments: raw})
	c.resp("launch")
	c.event("initialized")
}

func (c *client) configurationDone() {
	c.t.Helper()
	c.send(&dap.ConfigurationDoneRequest{Request: newRequest("configurationDone")})
	c.resp("configurationDone")
}

// launchHello runs the standard fixture launch: hello.bin at 0x8000
// with the SLD map, stopped at entry, small history window.
func launchHello(t *testing.T) *client {
	t.Helper()
	c := startClient(t)
	c.initialize()
	c.launch(map[string]any{
		"program": "testdata/hello.bin",
		"org":     "0x8000",
		"sld":     "testdata/hello.sld",
		"history": 5,
	})
	c.configurationDone()
	if body := c.stopped(); body.Reason != "entry" {
		t.Fatalf("launch stop reason = %q, want entry", body.Reason)
	}
	return c
}

// frame fetches the current (single) stack frame.
func (c *client) frame() dap.StackFrame {
	c.t.Helper()
	c.send(&dap.StackTraceRequest{
		Request:   newRequest("stackTrace"),
		Arguments: dap.StackTraceArguments{ThreadId: threadID},
	})
	resp := c.resp("stackTrace").(*dap.StackTraceResponse)
	if len(resp.Body.StackFrames) != 1 {
		c.t.Fatalf("stack frames = %d, want 1", len(resp.Body.StackFrames))
	}
	return resp.Body.StackFrames[0]
}

// pcOf extracts the PC from the frame's instruction pointer reference.
func (c *client) pc() uint16 {
	c.t.Helper()
	f := c.frame()
	var pc uint16
	if _, err := fmt.Sscanf(f.InstructionPointerReference, "0x%04X", &pc); err != nil {
		c.t.Fatalf("bad instruction pointer reference %q", f.InstructionPointerReference)
	}
	return pc
}

// readMem reads count bytes at addr through the DAP readMemory request.
func (c *client) readMem(addr uint16, count int) []byte {
	c.t.Helper()
	c.send(&dap.ReadMemoryRequest{
		Request: newRequest("readMemory"),
		Arguments: dap.ReadMemoryArguments{
			MemoryReference: fmt.Sprintf("0x%04X", addr),
			Count:           count,
		},
	})
	resp := c.resp("readMemory").(*dap.ReadMemoryResponse)
	data, err := base64decode(resp.Body.Data)
	if err != nil {
		c.t.Fatalf("readMemory data: %v", err)
	}
	return data
}
