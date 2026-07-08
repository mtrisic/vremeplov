package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mtrisic/vremeplov/core"
)

func runeMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestCharMapLettersCaseInsensitiveNoShift(t *testing.T) {
	for _, r := range []rune{'a', 'A'} {
		p, ok := mapKeyMsg(runeMsg(r))
		if !ok || p.key != core.KeyA || p.shift {
			t.Errorf("%q -> %+v ok=%v, want KeyA unshifted", r, p, ok)
		}
	}
}

// TestShiftSymbolTable pins the ROM's KEY_SHIFT_SYM_TABLE mapping
// (extracted from rom_a_v28.bin at 0x0D70).
func TestShiftSymbolTable(t *testing.T) {
	cases := map[rune]matrixPress{
		'_': {core.Key0, true},
		'!': {core.Key1, true},
		'"': {core.Key2, true},
		'#': {core.Key3, true},
		'$': {core.Key4, true},
		'%': {core.Key5, true},
		'&': {core.Key6, true},
		'(': {core.Key8, true},
		')': {core.Key9, true},
		'+': {core.KeySemicolon, true},
		'*': {core.KeyColon, true},
		'<': {core.KeyComma, true},
		'-': {core.KeyEquals, true},
		'>': {core.KeyDot, true},
		'?': {core.KeySlash, true},
		';': {core.KeySemicolon, false},
		'=': {core.KeyEquals, false},
		'5': {core.Key5, false},
	}
	for r, want := range cases {
		p, ok := mapKeyMsg(runeMsg(r))
		if !ok || p != want {
			t.Errorf("%q -> %+v ok=%v, want %+v", r, p, ok, want)
		}
	}
}

func TestSpecialKeys(t *testing.T) {
	cases := map[tea.KeyType]core.Key{
		tea.KeyEnter:     core.KeyEnter,
		tea.KeyEsc:       core.KeyBreak,
		tea.KeyBackspace: core.KeyDelete,
		tea.KeyTab:       core.KeyList,
		tea.KeyCtrlR:     core.KeyRepeat,
		tea.KeyUp:        core.KeyUp,
		tea.KeyLeft:      core.KeyLeft,
	}
	for kt, want := range cases {
		p, ok := mapKeyMsg(tea.KeyMsg{Type: kt})
		if !ok || p.key != want || p.shift {
			t.Errorf("%v -> %+v ok=%v, want %v unshifted", kt, p, ok, want)
		}
	}
	if _, ok := mapKeyMsg(tea.KeyMsg{Type: tea.KeyF1}); ok {
		t.Error("F1 should not map to a machine key")
	}
}
