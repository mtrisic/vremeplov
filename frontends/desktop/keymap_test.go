package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/mtrisic/vremeplov/core"
)

func TestStrokesFor(t *testing.T) {
	cases := []struct {
		name  string
		key   ebiten.Key
		shift bool
		want  strokes
	}{
		{"letter", ebiten.KeyA, false, strokes{keys: []core.Key{core.KeyA}}},
		{"shifted letter passes through", ebiten.KeyZ, true, strokes{keys: []core.Key{core.KeyZ}}},
		{"digit", ebiten.KeyDigit3, false, strokes{keys: []core.Key{core.Key3}}},
		{"space", ebiten.KeySpace, false, strokes{keys: []core.Key{core.KeySpace}}},
		{"enter", ebiten.KeyEnter, false, strokes{keys: []core.Key{core.KeyEnter}}},
		{"escape is break", ebiten.KeyEscape, false, strokes{keys: []core.Key{core.KeyBreak}}},
		{"backspace is delete", ebiten.KeyBackspace, false, strokes{keys: []core.Key{core.KeyDelete}}},
		{"tab is list", ebiten.KeyTab, false, strokes{keys: []core.Key{core.KeyList}}},
		{"arrow", ebiten.KeyArrowUp, false, strokes{keys: []core.Key{core.KeyUp}}},
		{"shift itself", ebiten.KeyShiftRight, false, strokes{keys: []core.Key{core.KeyShift}}},
		{"ctrl is repeat", ebiten.KeyControlLeft, false, strokes{keys: []core.Key{core.KeyRepeat}}},
		{"semicolon", ebiten.KeySemicolon, false, strokes{keys: []core.Key{core.KeySemicolon}}},
		// Galaksija layout translations.
		{"minus needs machine shift", ebiten.KeyMinus, false, strokes{keys: []core.Key{core.KeyShift, core.KeyEquals}}},
		{"equals", ebiten.KeyEqual, false, strokes{keys: []core.Key{core.KeyEquals}}},
		{"star lives on colon", ebiten.KeyDigit8, true, strokes{keys: []core.Key{core.KeyColon}}},
		{"plus lives on semicolon", ebiten.KeyEqual, true, strokes{keys: []core.Key{core.KeySemicolon}}},
		{"bang", ebiten.KeyDigit1, true, strokes{keys: []core.Key{core.Key1}}},
		{"paren", ebiten.KeyDigit9, true, strokes{keys: []core.Key{core.Key8}}},
		{"question", ebiten.KeySlash, true, strokes{keys: []core.Key{core.KeySlash}}},
		{"double quote", ebiten.KeyQuote, true, strokes{keys: []core.Key{core.Key2}}},
		// The dropShift case: ':' is unshifted on the Galaksija.
		{"colon drops shift", ebiten.KeySemicolon, true, strokes{keys: []core.Key{core.KeyColon}, dropShift: true}},
	}
	for _, tc := range cases {
		got, ok := strokesFor(tc.key, tc.shift)
		if !ok {
			t.Errorf("%s: strokesFor(%v, %v) not mapped", tc.name, tc.key, tc.shift)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: strokesFor(%v, %v) = %+v, want %+v", tc.name, tc.key, tc.shift, got, tc.want)
		}
	}

	unmapped := []struct {
		key   ebiten.Key
		shift bool
	}{
		{ebiten.KeyF5, false}, // chrome keys never reach the machine
		{ebiten.KeyF12, true},
		{ebiten.KeyBracketLeft, false}, // no such Galaksija character
		{ebiten.KeyDigit6, true},       // '^'
		{ebiten.KeyDigit2, true},       // '@'
		{ebiten.KeyQuote, false},       // '\'' is not in the shift table
		{ebiten.KeyBackquote, false},
	}
	for _, tc := range unmapped {
		if s, ok := strokesFor(tc.key, tc.shift); ok {
			t.Errorf("strokesFor(%v, %v) = %+v, want unmapped", tc.key, tc.shift, s)
		}
	}
}

// fakeMatrix records presses like the machine's matrix would see them.
type fakeMatrix struct {
	down map[core.Key]bool
	log  []string
}

func newFakeMatrix() *fakeMatrix { return &fakeMatrix{down: map[core.Key]bool{}} }

func (f *fakeMatrix) PressKey(k core.Key) {
	f.down[k] = true
	f.log = append(f.log, fmt.Sprintf("+%d", k))
}

func (f *fakeMatrix) ReleaseKey(k core.Key) {
	delete(f.down, k)
	f.log = append(f.log, fmt.Sprintf("-%d", k))
}

func mustStrokes(t *testing.T, k ebiten.Key, shift bool) strokes {
	t.Helper()
	s, ok := strokesFor(k, shift)
	if !ok {
		t.Fatalf("strokesFor(%v, %v) not mapped", k, shift)
	}
	return s
}

// TestKeyHolderSharedShift: physical Shift and a stroke's KeyShift
// refcount onto one matrix line; the line stays down until the last
// holder releases.
func TestKeyHolderSharedShift(t *testing.T) {
	f := newFakeMatrix()
	h := newKeyHolder(f)

	h.down(ebiten.KeyShiftLeft, mustStrokes(t, ebiten.KeyShiftLeft, false))
	if !f.down[core.KeyShift] {
		t.Fatal("shift down did not press machine Shift")
	}
	// '-' presses [Shift, Equals]; Shift is already down — no re-press.
	h.down(ebiten.KeyMinus, mustStrokes(t, ebiten.KeyMinus, false))
	if !f.down[core.KeyEquals] {
		t.Fatal("minus did not press Equals")
	}
	// Physical Shift up: the minus stroke still holds machine Shift.
	h.up(ebiten.KeyShiftLeft)
	if !f.down[core.KeyShift] {
		t.Fatal("machine Shift released while a stroke still holds it")
	}
	h.up(ebiten.KeyMinus)
	if len(f.down) != 0 {
		t.Fatalf("keys stuck after release: %v", f.down)
	}
}

// TestKeyHolderDropShift: while ':' is held the machine must not see
// Shift, even though the host holds it — and Shift returns when ':'
// lifts if the physical key is still down.
func TestKeyHolderDropShift(t *testing.T) {
	f := newFakeMatrix()
	h := newKeyHolder(f)

	h.down(ebiten.KeyShiftLeft, mustStrokes(t, ebiten.KeyShiftLeft, false))
	h.down(ebiten.KeySemicolon, mustStrokes(t, ebiten.KeySemicolon, true)) // ':'
	if f.down[core.KeyShift] {
		t.Fatal("machine Shift held during a dropShift stroke")
	}
	if !f.down[core.KeyColon] {
		t.Fatal("colon not pressed")
	}
	h.up(ebiten.KeySemicolon)
	if !f.down[core.KeyShift] {
		t.Fatal("machine Shift not restored after the dropShift stroke")
	}
	h.up(ebiten.KeyShiftLeft)
	if len(f.down) != 0 {
		t.Fatalf("keys stuck after release: %v", f.down)
	}
}

// TestKeyHolderBothShifts: the ':' suppression must survive either
// physical Shift being involved.
func TestKeyHolderBothShifts(t *testing.T) {
	f := newFakeMatrix()
	h := newKeyHolder(f)

	h.down(ebiten.KeyShiftLeft, mustStrokes(t, ebiten.KeyShiftLeft, false))
	h.down(ebiten.KeyShiftRight, mustStrokes(t, ebiten.KeyShiftRight, false))
	h.down(ebiten.KeySemicolon, mustStrokes(t, ebiten.KeySemicolon, true))
	if f.down[core.KeyShift] {
		t.Fatal("machine Shift held during dropShift with both Shifts down")
	}
	h.up(ebiten.KeyShiftLeft)
	if f.down[core.KeyShift] {
		t.Fatal("releasing one physical Shift re-pressed machine Shift mid-suppression")
	}
	h.up(ebiten.KeySemicolon)
	if !f.down[core.KeyShift] {
		t.Fatal("machine Shift not restored (right Shift still down)")
	}
	h.up(ebiten.KeyShiftRight)
	if len(f.down) != 0 {
		t.Fatalf("keys stuck after release: %v", f.down)
	}
}

func TestKeyHolderReleaseAll(t *testing.T) {
	f := newFakeMatrix()
	h := newKeyHolder(f)
	h.down(ebiten.KeyShiftLeft, mustStrokes(t, ebiten.KeyShiftLeft, false))
	h.down(ebiten.KeyA, mustStrokes(t, ebiten.KeyA, true))
	h.releaseAll()
	if len(f.down) != 0 {
		t.Fatalf("keys stuck after releaseAll: %v", f.down)
	}
	// Stale up events after a releaseAll must be harmless no-ops.
	h.up(ebiten.KeyA)
	h.up(ebiten.KeyShiftLeft)
	if len(f.log) == 0 {
		t.Fatal("no matrix traffic recorded")
	}
}
