package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/mtrisic/vremeplov/core"
)

// matrixPresser is the machine surface keyHolder drives (a *core.Machine;
// an interface so the state machine tests headless with a fake).
type matrixPresser interface {
	PressKey(core.Key)
	ReleaseKey(core.Key)
}

// keyHolder turns physical key-down/up events into matrix presses. It
// refcounts machine keys across concurrent holds — several physical
// keys may share a machine key (both Shifts, a stroke's KeyShift plus
// the physical Shift) and the matrix line must stay down until the
// last holder lets go. dropShift holds suppress machine Shift for
// their duration (the ':' case).
type keyHolder struct {
	m        matrixPresser
	held     map[ebiten.Key]strokes
	count    map[core.Key]int
	suppress int // active dropShift holds
}

func newKeyHolder(m matrixPresser) *keyHolder {
	return &keyHolder{
		m:     m,
		held:  make(map[ebiten.Key]strokes),
		count: make(map[core.Key]int),
	}
}

func (h *keyHolder) down(k ebiten.Key, s strokes) {
	if _, dup := h.held[k]; dup {
		return
	}
	h.held[k] = s
	if s.dropShift {
		if h.suppress == 0 && h.count[core.KeyShift] > 0 {
			h.m.ReleaseKey(core.KeyShift)
		}
		h.suppress++
	}
	for _, mk := range s.keys {
		h.count[mk]++
		if h.count[mk] == 1 && !(mk == core.KeyShift && h.suppress > 0) {
			h.m.PressKey(mk)
		}
	}
}

func (h *keyHolder) up(k ebiten.Key) {
	s, ok := h.held[k]
	if !ok {
		return
	}
	delete(h.held, k)
	for _, mk := range s.keys {
		h.count[mk]--
		if h.count[mk] == 0 {
			delete(h.count, mk)
			if !(mk == core.KeyShift && h.suppress > 0) {
				h.m.ReleaseKey(mk)
			}
		}
	}
	if s.dropShift {
		h.suppress--
		if h.suppress == 0 && h.count[core.KeyShift] > 0 {
			h.m.PressKey(core.KeyShift)
		}
	}
}

// releaseAll lets go of everything — used before snapshot restores and
// rewinds so no phantom key survives the state jump.
func (h *keyHolder) releaseAll() {
	for mk, n := range h.count {
		if n > 0 && !(mk == core.KeyShift && h.suppress > 0) {
			h.m.ReleaseKey(mk)
		}
	}
	h.held = make(map[ebiten.Key]strokes)
	h.count = make(map[core.Key]int)
	h.suppress = 0
}

// handleMachineKeys scans the mapped physical keys and feeds edges to
// the holder. The Shift state at press time picks the character; the
// release is by physical key, so layout-shifted characters let go
// cleanly (the wasm frontend's event.code semantics).
func (g *Game) handleMachineKeys() {
	shift := ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight)
	for _, k := range scanKeys {
		if inpututil.IsKeyJustPressed(k) {
			if s, ok := strokesFor(k, shift); ok {
				g.keys.down(k, s)
			}
		}
		if inpututil.IsKeyJustReleased(k) {
			g.keys.up(k)
		}
	}
}

// handleMachineKeyReleases processes only key-ups — while a modal
// (quit confirmation, monitor REPL) swallows presses, releases still
// land so nothing stays stuck on the matrix.
func (g *Game) handleMachineKeyReleases() {
	for _, k := range scanKeys {
		if inpututil.IsKeyJustReleased(k) {
			g.keys.up(k)
		}
	}
}
