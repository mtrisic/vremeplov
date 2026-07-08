package main

import (
	"testing"

	"github.com/mtrisic/vremeplov/core"
)

// Tests here must stay free of Ebiten runtime calls (NewImage, RunGame,
// input polling): the gate runs headless in the devcontainer.

func TestViewRGBA(t *testing.T) {
	pix := make([]byte, core.FrameWidth*core.FrameHeight)
	pix[core.ActiveY*core.FrameWidth+core.ActiveX] = 1 // active top-left
	pix[0] = 1                                         // frame top-left (border, cropped away)

	w, h := viewSize(false)
	if w != core.ActiveW || h != core.ActiveH {
		t.Fatalf("viewSize(false) = %d×%d, want %d×%d", w, h, core.ActiveW, core.ActiveH)
	}
	dst := make([]byte, w*h*4)
	viewRGBA(pix, dst, false)
	if dst[0] != 0xFF || dst[3] != 0xFF {
		t.Fatalf("active-corner pixel = %v, want opaque white", dst[:4])
	}
	if dst[4] != 0 || dst[7] != 0xFF {
		t.Fatalf("neighbour pixel = %v, want opaque black", dst[4:8])
	}

	w, h = viewSize(true)
	if w != core.FrameWidth || h != core.FrameHeight {
		t.Fatalf("viewSize(true) = %d×%d, want %d×%d", w, h, core.FrameWidth, core.FrameHeight)
	}
	dst = make([]byte, w*h*4)
	viewRGBA(pix, dst, true)
	if dst[0] != 0xFF {
		t.Fatal("full-frame view lost the border pixel")
	}
	at := (core.ActiveY*core.FrameWidth + core.ActiveX) * 4
	if dst[at] != 0xFF {
		t.Fatal("full-frame view lost the active-corner pixel")
	}
}

func TestLayoutScreen(t *testing.T) {
	cases := []struct {
		name                   string
		winW, winH, imgW, imgH int
		panelW, footH          int
		scale, x, y            int
	}{
		// 3× fits exactly in the default window width budget.
		{"default", 1160, 700, 256, 208, 0, 26, 3, 196, 25},
		// A right dock steals width: still 3× here.
		{"panel", 1160, 700, 256, 208, 360, 26, 3, 16, 25},
		// Tiny window: scale clamps to 1 and offsets clamp to 0.
		{"tiny", 200, 150, 256, 208, 0, 26, 1, 0, 0},
		// Full frame in the default window.
		{"full", 1160, 700, 384, 320, 0, 26, 2, 196, 17},
	}
	for _, tc := range cases {
		scale, x, y := layoutScreen(tc.winW, tc.winH, tc.imgW, tc.imgH, tc.panelW, tc.footH)
		if scale != tc.scale || x != tc.x || y != tc.y {
			t.Errorf("%s: layoutScreen = (%d, %d, %d), want (%d, %d, %d)",
				tc.name, scale, x, y, tc.scale, tc.x, tc.y)
		}
	}
}
