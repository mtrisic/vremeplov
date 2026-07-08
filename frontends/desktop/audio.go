package main

import (
	"math"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"
)

// sndRate is the playback sample rate the machine mix is rendered at.
const sndRate = 48000

// sndBacklogCap bounds the FIFO to ~200 ms of f32le stereo so a
// producer/consumer clock drift turns into a dropped burst, not
// ever-growing latency.
const sndBacklogCap = sndRate / 5 * 8

// audioPipe is the io.Reader Ebiten's player pulls from: a
// mutex-guarded FIFO of raw float32 LE stereo PCM. Update pushes the
// machine mix; the player's goroutine reads, getting silence on
// underrun (never an error, never EOF).
type audioPipe struct {
	mu  sync.Mutex
	buf []byte
}

func (p *audioPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	n := copy(b, p.buf)
	p.buf = p.buf[n:]
	p.mu.Unlock()
	for i := n; i < len(b); i++ {
		b[i] = 0
	}
	return len(b), nil
}

// push interleaves the mono machine mix to both channels as f32le.
func (p *audioPipe) push(mono []float32) {
	if len(mono) == 0 {
		return
	}
	raw := make([]byte, len(mono)*8)
	for i, v := range mono {
		bits := math.Float32bits(v)
		le := [4]byte{byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)}
		copy(raw[8*i:], le[:])
		copy(raw[8*i+4:], le[:])
	}
	p.mu.Lock()
	p.buf = append(p.buf, raw...)
	if over := len(p.buf) - sndBacklogCap; over > 0 {
		p.buf = p.buf[over:]
	}
	p.mu.Unlock()
}

// drop empties the FIFO — transport bursts (loads, resets, snapshot
// jumps, rewinds) must not play as noise.
func (p *audioPipe) drop() {
	p.mu.Lock()
	p.buf = p.buf[:0]
	p.mu.Unlock()
}

// initAudio opens the sound device and starts the pull stream; sound
// is on by default — the desktop has no browser-style gesture rule,
// and the stream is silence until a program toggles the tape output.
func (g *Game) initAudio() error {
	ctx := audio.NewContext(sndRate)
	g.aud = &audioPipe{}
	p, err := ctx.NewPlayerF32(g.aud)
	if err != nil {
		return err
	}
	p.SetBufferSize(50 * time.Millisecond)
	p.Play()
	g.player = p
	g.m.EnableAudio()
	return nil
}

// pumpAudio moves the machine time elapsed this tick into the FIFO.
// A nil render with audio enabled is the rewind/restore resync signal.
func (g *Game) pumpAudio() {
	if g.aud == nil {
		return
	}
	s := g.m.RenderAudio(sndRate)
	if s == nil {
		g.aud.drop()
		return
	}
	g.aud.push(s)
}

// dropAudio discards machine sound rendered across a transport burst
// (fast boot, tape load, snapshot restore) plus whatever is queued.
func (g *Game) dropAudio() {
	if g.aud == nil {
		return
	}
	g.m.RenderAudio(sndRate) // consume the burst
	g.aud.drop()
}

// toggleSound flips the cassette-port speaker.
func (g *Game) toggleSound() {
	if g.aud == nil {
		return
	}
	if g.m.AudioEnabled() {
		g.m.DisableAudio()
		g.aud.drop()
		g.status = "sound off"
	} else {
		g.m.EnableAudio()
		g.status = "sound on"
	}
}
