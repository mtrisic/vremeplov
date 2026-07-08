package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
)

// matrixPress is one Galaksija key combination: a matrix key, optionally
// with Shift held.
type matrixPress struct {
	key   core.Key
	shift bool
}

// charMap maps typeable characters to matrix presses. The shifted
// entries come from ROM A's KEY_SHIFT_SYM_TABLE at 0x0D70 (extracted
// from the binary; SPEC.md §5.2). Letters are case-insensitive and
// never use Shift — on the Galaksija, Shift+letter produces Yugoslav
// characters, which host keyboards reach via their plain letters.
var charMap = buildCharMap()

func buildCharMap() map[rune]matrixPress {
	m := make(map[rune]matrixPress, 96)
	for i := 0; i < 26; i++ {
		p := matrixPress{key: core.KeyA + core.Key(i)}
		m[rune('a'+i)] = p
		m[rune('A'+i)] = p
	}
	for i := 0; i < 10; i++ {
		m[rune('0'+i)] = matrixPress{key: core.Key0 + core.Key(i)}
	}
	unshifted := map[rune]core.Key{
		' ': core.KeySpace, ';': core.KeySemicolon, ':': core.KeyColon,
		',': core.KeyComma, '=': core.KeyEquals, '.': core.KeyDot,
		'/': core.KeySlash,
	}
	for r, k := range unshifted {
		m[r] = matrixPress{key: k}
	}
	// KEY_SHIFT_SYM_TABLE: shifted symbol -> scan code.
	shifted := map[rune]core.Key{
		'_': core.Key0, '!': core.Key1, '"': core.Key2, '#': core.Key3,
		'$': core.Key4, '%': core.Key5, '&': core.Key6, '(': core.Key8,
		')': core.Key9, '+': core.KeySemicolon, '*': core.KeyColon,
		'<': core.KeyComma, '-': core.KeyEquals, '>': core.KeyDot,
		'?': core.KeySlash,
	}
	for r, k := range shifted {
		m[r] = matrixPress{key: k, shift: true}
	}
	return m
}

// specialKeyMap maps non-rune terminal keys to matrix keys (SPEC.md
// §5.2): arrows→arrows, Enter→Return, Esc→Break, Backspace→Delete,
// Tab→List, Ctrl+R→Repeat.
var specialKeyMap = map[tea.KeyType]core.Key{
	tea.KeyUp:        core.KeyUp,
	tea.KeyDown:      core.KeyDown,
	tea.KeyLeft:      core.KeyLeft,
	tea.KeyRight:     core.KeyRight,
	tea.KeyEnter:     core.KeyEnter,
	tea.KeyEsc:       core.KeyBreak,
	tea.KeyBackspace: core.KeyDelete,
	tea.KeyTab:       core.KeyList,
	tea.KeyCtrlR:     core.KeyRepeat,
	tea.KeySpace:     core.KeySpace,
}

// mapKeyMsg resolves a terminal key event to a Galaksija matrix press.
// ok is false for keys with no Galaksija meaning (chrome keys are
// handled before this).
func mapKeyMsg(msg tea.KeyMsg) (matrixPress, bool) {
	if k, found := specialKeyMap[msg.Type]; found {
		return matrixPress{key: k}, true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		p, found := charMap[msg.Runes[0]]
		return p, found
	}
	return matrixPress{}, false
}
