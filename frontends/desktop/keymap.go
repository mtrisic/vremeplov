package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/mtrisic/vremeplov/core"
)

// strokes is what one physical key press puts on the machine's matrix.
type strokes struct {
	// keys in press order, machine Shift first when the Galaksija
	// character needs it.
	keys []core.Key
	// dropShift: the host types this character with Shift held, but on
	// the Galaksija it is an unshifted key (':' — Shift+Colon would
	// read as '*'), so machine Shift must stay released for the hold.
	dropShift bool
}

// specialKeys are the physical keys that map by function, not by the
// character they type. Ctrl doubles as the Galaksija REPT key.
var specialKeys = map[ebiten.Key]core.Key{
	ebiten.KeyEnter:        core.KeyEnter,
	ebiten.KeyEscape:       core.KeyBreak,
	ebiten.KeyBackspace:    core.KeyDelete,
	ebiten.KeyTab:          core.KeyList,
	ebiten.KeyArrowUp:      core.KeyUp,
	ebiten.KeyArrowDown:    core.KeyDown,
	ebiten.KeyArrowLeft:    core.KeyLeft,
	ebiten.KeyArrowRight:   core.KeyRight,
	ebiten.KeySpace:        core.KeySpace,
	ebiten.KeyShiftLeft:    core.KeyShift,
	ebiten.KeyShiftRight:   core.KeyShift,
	ebiten.KeyControlLeft:  core.KeyRepeat,
	ebiten.KeyControlRight: core.KeyRepeat,
}

// charKeys gives each character-typing physical key its US-layout
// unshifted and shifted runes; core.KeystrokeForRune translates those
// to the Galaksija layout, so the machine sees the character the user
// meant (wasm frontend semantics), not the raw key position.
var charKeys = map[ebiten.Key][2]rune{
	ebiten.KeyDigit1:    {'1', '!'},
	ebiten.KeyDigit2:    {'2', '@'},
	ebiten.KeyDigit3:    {'3', '#'},
	ebiten.KeyDigit4:    {'4', '$'},
	ebiten.KeyDigit5:    {'5', '%'},
	ebiten.KeyDigit6:    {'6', '^'},
	ebiten.KeyDigit7:    {'7', '&'},
	ebiten.KeyDigit8:    {'8', '*'},
	ebiten.KeyDigit9:    {'9', '('},
	ebiten.KeyDigit0:    {'0', ')'},
	ebiten.KeyMinus:     {'-', '_'},
	ebiten.KeyEqual:     {'=', '+'},
	ebiten.KeySemicolon: {';', ':'},
	ebiten.KeyQuote:     {'\'', '"'},
	ebiten.KeyComma:     {',', '<'},
	ebiten.KeyPeriod:    {'.', '>'},
	ebiten.KeySlash:     {'/', '?'},
}

func init() {
	for k := ebiten.KeyA; k <= ebiten.KeyZ; k++ {
		charKeys[k] = [2]rune{'a' + rune(k-ebiten.KeyA), 'A' + rune(k-ebiten.KeyA)}
	}
}

// scanKeys is every physical key the input loop watches.
var scanKeys = func() []ebiten.Key {
	var ks []ebiten.Key
	for k := range specialKeys {
		ks = append(ks, k)
	}
	for k := range charKeys {
		ks = append(ks, k)
	}
	return ks
}()

// strokesFor maps a physical key (with the host Shift state at press
// time) to machine strokes. ok=false means the key is not for the
// machine. Shifted letters stay a plain letter stroke — the machine
// Shift is already held via the Shift key's own mapping, so the matrix
// sees the same chord real hardware would.
func strokesFor(k ebiten.Key, shift bool) (strokes, bool) {
	if mk, ok := specialKeys[k]; ok {
		return strokes{keys: []core.Key{mk}}, true
	}
	chars, ok := charKeys[k]
	if !ok {
		return strokes{}, false
	}
	r := chars[0]
	if shift {
		r = chars[1]
	}
	mk, needShift, ok := core.KeystrokeForRune(r)
	if !ok {
		return strokes{}, false
	}
	switch {
	case needShift && !shift:
		// '-' and friends: the host types it unshifted, the Galaksija
		// needs Shift — press it as part of the stroke.
		return strokes{keys: []core.Key{core.KeyShift, mk}}, true
	case !needShift && shift && !isLetter(k):
		// ':' — unshifted on the Galaksija while the host holds Shift.
		return strokes{keys: []core.Key{mk}, dropShift: true}, true
	default:
		return strokes{keys: []core.Key{mk}}, true
	}
}

func isLetter(k ebiten.Key) bool {
	return k >= ebiten.KeyA && k <= ebiten.KeyZ
}
