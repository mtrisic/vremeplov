package core

import (
	"fmt"
	"sort"
)

// Key identifies one position in the Galaksija keyboard matrix: the
// memory offset of the key's address at 0x2000+Key (SPEC.md §3.5).
type Key uint8

// keyCount covers matrix offsets 0x00–0x37; offset 0x00 is the tape
// comparator input, never a key.
const keyCount = 0x38

// comparatorOffset is the peripheral offset where the tape comparator
// output appears (the ROM's LOAD routine polls 0x2000).
const comparatorOffset = 0x00

// Keyboard matrix positions. Letters are at their ASCII value minus
// 0x40 (the ROM's scan-code trick).
const (
	KeyA Key = iota + 0x01
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
	KeyUp    // 0x1B
	KeyDown  // 0x1C
	KeyLeft  // 0x1D
	KeyRight // 0x1E
	KeySpace // 0x1F
	Key0     // 0x20
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
	KeySemicolon // 0x2A ';'
	KeyColon     // 0x2B ':'
	KeyComma     // 0x2C ','
	KeyEquals    // 0x2D '='
	KeyDot       // 0x2E '.'
	KeySlash     // 0x2F '/'
	KeyEnter     // 0x30 Return
	KeyBreak     // 0x31
	KeyRepeat    // 0x32
	KeyDelete    // 0x33
	KeyList      // 0x34
	KeyShift     // 0x35 (both physical Shift keys share this line)
)

// KeyByName maps stable symbolic names (used by headless key scripts and
// frontends) to matrix positions: "A".."Z", "0".."9", and the specials
// below.
var KeyByName = func() map[string]Key {
	m := make(map[string]Key, 54)
	for i := 0; i < 26; i++ {
		m[string(rune('A'+i))] = KeyA + Key(i)
	}
	for i := 0; i < 10; i++ {
		m[string(rune('0'+i))] = Key0 + Key(i)
	}
	for name, k := range map[string]Key{
		"SEMI": KeySemicolon, "COLON": KeyColon, "COMMA": KeyComma,
		"EQUALS": KeyEquals, "DOT": KeyDot, "SLASH": KeySlash,
		"SPACE": KeySpace, "UP": KeyUp, "DOWN": KeyDown,
		"LEFT": KeyLeft, "RIGHT": KeyRight, "ENTER": KeyEnter,
		"BREAK": KeyBreak, "REPEAT": KeyRepeat, "DELETE": KeyDelete,
		"LIST": KeyList, "SHIFT": KeyShift,
	} {
		m[name] = k
	}
	return m
}()

func validKey(k Key) bool { return k >= KeyA && k <= KeyShift }

// PressKey holds a key down in the matrix until ReleaseKey.
func (m *Machine) PressKey(k Key) {
	if validKey(k) {
		m.journal(histEvent{kind: histKey, key: KeyEvent{Key: k, Down: true}})
		m.keys[k] = true
	}
}

// ReleaseKey releases a key.
func (m *Machine) ReleaseKey(k Key) {
	if validKey(k) {
		m.journal(histEvent{kind: histKey, key: KeyEvent{Key: k, Down: false}})
		m.keys[k] = false
	}
}

// KeyEvent is a timestamped press or release, applied when the machine's
// T-state counter reaches Tstate. Deterministic input (headless scripts,
// TypeText) uses these instead of the immediate Press/Release calls.
type KeyEvent struct {
	Tstate uint64
	Key    Key
	Down   bool
}

// QueueKeyEvents schedules events. Events are merged into the pending
// queue and applied in Tstate order at instruction boundaries; events
// stamped in the past apply before the next instruction.
func (m *Machine) QueueKeyEvents(events ...KeyEvent) {
	m.journal(histEvent{kind: histQueue, batch: append([]KeyEvent(nil), events...)})
	for _, e := range events {
		if !validKey(e.Key) {
			continue
		}
		m.queue = append(m.queue, e)
	}
	pending := m.queue[m.qpos:]
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Tstate < pending[j].Tstate })
}

// drainKeyQueue applies all events due at the current T-state counter.
func (m *Machine) drainKeyQueue() {
	for m.qpos < len(m.queue) && m.queue[m.qpos].Tstate <= m.tstates {
		e := m.queue[m.qpos]
		m.keys[e.Key] = e.Down
		m.qpos++
	}
}

// ParseKey resolves a symbolic key name (see KeyByName) or a hex matrix
// offset like "0x31".
func ParseKey(s string) (Key, error) {
	if k, ok := KeyByName[s]; ok {
		return k, nil
	}
	var v uint8
	if _, err := fmt.Sscanf(s, "0x%02X", &v); err == nil && validKey(Key(v)) {
		return Key(v), nil
	}
	return 0, fmt.Errorf("core: unknown key %q", s)
}
