package main

import (
	"bytes"
	"image/color"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/gomono"
)

// fontSize is the one text size the frontend uses (status line, footer
// buttons, monitor panel) — a monospace face so monitor columns align.
const fontSize = 14

var monoFace = sync.OnceValue(func() *text.GoTextFace {
	src, err := text.NewGoTextFaceSource(bytes.NewReader(gomono.TTF))
	if err != nil {
		panic(err) // embedded TTF; cannot fail
	}
	return &text.GoTextFace{Source: src, Size: fontSize}
})

// drawText draws s with its top-left corner at (x, y).
func drawText(dst *ebiten.Image, s string, x, y int, clr color.Color) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(clr)
	text.Draw(dst, s, monoFace(), op)
}

// textWidth is the advance width of s in pixels.
func textWidth(s string) int {
	return int(text.Advance(s, monoFace()))
}
