package core

import (
	"bytes"
	"image/png"
	"testing"
)

// TestFramePNG: both modes decode to the right geometry, contain lit
// pixels once the machine is at READY, and are deterministic.
func TestFramePNG(t *testing.T) {
	m := bootMachine(t)

	for _, tc := range []struct {
		name     string
		crop     bool
		wid, hgt int
	}{
		{"full frame", false, FrameWidth, FrameHeight},
		{"active area", true, ActiveW, ActiveH},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := m.FramePNG(tc.crop)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			b := img.Bounds()
			if b.Dx() != tc.wid || b.Dy() != tc.hgt {
				t.Fatalf("decoded %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.wid, tc.hgt)
			}
			lit := 0
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if r, _, _, _ := img.At(x, y).RGBA(); r > 0 {
						lit++
					}
				}
			}
			if lit == 0 {
				t.Fatal("READY screen rendered all black")
			}
			again, err := m.FramePNG(tc.crop)
			if err != nil || !bytes.Equal(data, again) {
				t.Fatal("FramePNG is not deterministic")
			}
		})
	}
}
