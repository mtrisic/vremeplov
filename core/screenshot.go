package core

import (
	"bytes"
	"image"
	"image/png"
)

// FramePNG renders the current frame as a grayscale PNG — the full
// 384×320 frame, or cropped to the 256×208 active area. Deterministic:
// identical machine state encodes identical bytes, in every frontend.
func (m *Machine) FramePNG(crop bool) ([]byte, error) {
	pix := make([]byte, FrameWidth*FrameHeight)
	m.Frame(pix)
	img := image.NewGray(image.Rect(0, 0, FrameWidth, FrameHeight))
	for i, p := range pix {
		img.Pix[i] = p * 0xFF
	}
	var out image.Image = img
	if crop {
		out = img.SubImage(image.Rect(ActiveX, ActiveY,
			ActiveX+ActiveW, ActiveY+ActiveH))
	}
	var b bytes.Buffer
	if err := png.Encode(&b, out); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
