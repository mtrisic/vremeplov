package core

import (
	"fmt"
	"strings"
)

// Typing pacing: each key is held long enough to clear the ROM's
// 256-consecutive-read debounce and released long enough for LAST_KEY to
// clear, so double letters register (tuned in Phase 3; the TUI uses the
// same hold length).
const (
	typeHoldTstates = 3 * TstatesPerFrame
	typeGapTstates  = 2 * TstatesPerFrame
	// typeLeadTstates delays the first key so a preceding command has a
	// moment to reach its input loop.
	typeLeadTstates = 2 * TstatesPerFrame
)

// charKeystroke maps a rune to its Galaksija keystroke: the matrix key
// and whether Shift is held. Letters type unshifted (the machine is
// upper-case only); the shifted punctuation follows ROM A's shift table
// (SPEC.md §3.5).
func charKeystroke(r rune) (key Key, shift bool, ok bool) {
	switch {
	case r >= 'A' && r <= 'Z':
		return KeyA + Key(r-'A'), false, true
	case r >= 'a' && r <= 'z':
		return KeyA + Key(r-'a'), false, true
	case r >= '0' && r <= '9':
		return Key0 + Key(r-'0'), false, true
	}
	unshifted := map[rune]Key{
		' ': KeySpace, '\n': KeyEnter, ';': KeySemicolon, ':': KeyColon,
		',': KeyComma, '=': KeyEquals, '.': KeyDot, '/': KeySlash,
	}
	if k, found := unshifted[r]; found {
		return k, false, true
	}
	shifted := map[rune]Key{
		'_': Key0, '!': Key1, '"': Key2, '#': Key3, '$': Key4,
		'%': Key5, '&': Key6, '(': Key8, ')': Key9,
		'+': KeySemicolon, '*': KeyColon, '<': KeyComma,
		'-': KeyEquals, '>': KeyDot, '?': KeySlash,
	}
	if k, found := shifted[r]; found {
		return k, true, true
	}
	return 0, false, false
}

// KeystrokeForRune exposes the rune → keystroke mapping to frontends,
// so terminal and browser input agree with TypeText on the machine's
// shift table.
func KeystrokeForRune(r rune) (key Key, shift bool, ok bool) {
	return charKeystroke(r)
}

// TypeText compiles s into queued key events, as if a very regular
// human typed it: hold 3 frames, gap 2, Shift wrapped around shifted
// characters. '\n' presses Return; '\r' is ignored so CRLF listings
// type cleanly. It returns the machine T-state at which typing is done
// (the last release plus the inter-key gap — the earliest safe moment
// to queue more input). Unsupported characters fail before anything is
// queued.
func (m *Machine) TypeText(s string) (uint64, error) {
	type stroke struct {
		key   Key
		shift bool
	}
	var strokes []stroke
	for _, r := range s {
		if r == '\r' {
			continue
		}
		k, shift, ok := charKeystroke(r)
		if !ok {
			return 0, fmt.Errorf("core: TypeText: no Galaksija keystroke for %q", r)
		}
		strokes = append(strokes, stroke{k, shift})
	}
	if len(strokes) == 0 {
		return m.tstates, nil
	}
	at := m.tstates + typeLeadTstates
	for _, st := range strokes {
		if st.shift {
			m.QueueKeyEvents(KeyEvent{Tstate: at, Key: KeyShift, Down: true})
		}
		m.QueueKeyEvents(
			KeyEvent{Tstate: at, Key: st.key, Down: true},
			KeyEvent{Tstate: at + typeHoldTstates, Key: st.key, Down: false},
		)
		if st.shift {
			m.QueueKeyEvents(KeyEvent{Tstate: at + typeHoldTstates, Key: KeyShift, Down: false})
		}
		at += typeHoldTstates + typeGapTstates
	}
	return at, nil
}

// TypeTextDuration returns how long typing s would take, without
// queueing anything (lead-in plus per-key pacing; '\r' ignored).
func TypeTextDuration(s string) uint64 {
	n := uint64(len(strings.ReplaceAll(s, "\r", "")))
	if n == 0 {
		return 0
	}
	return typeLeadTstates + n*(typeHoldTstates+typeGapTstates)
}
