package logoimage

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestNormalizePreservesAspectTransparencyAndContentIdentity(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 160, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 160; x++ {
			src.Set(x, y, color.NRGBA{R: 120, A: 255})
		}
	}
	for _, format := range []string{"png", "jpeg"} {
		var raw bytes.Buffer
		if format == "png" {
			png.Encode(&raw, src)
		} else {
			jpeg.Encode(&raw, src, nil)
		}
		id, out, err := Normalize(raw.Bytes())
		if err != nil || !ValidID(id) || len(out) > 65536 {
			t.Fatal("image normalization failed", format, err)
		}
		decoded, err := png.Decode(bytes.NewReader(out))
		if err != nil || decoded.Bounds() != image.Rect(0, 0, 96, 96) {
			t.Fatal("normalized logo is not a 96px PNG", err)
		}
		_, _, _, outside := decoded.At(48, 0).RGBA()
		_, _, _, inside := decoded.At(48, 48).RGBA()
		if outside != 0 || inside != 65535 {
			t.Fatal("logo aspect or transparent margin lost")
		}
		again, output, _ := Normalize(raw.Bytes())
		if again != id || !bytes.Equal(output, out) {
			t.Fatal("same source produced a different immutable logo")
		}
	}
	for _, raw := range [][]byte{nil, []byte("<svg/>"), []byte("GIF89a"), bytes.Repeat([]byte("x"), (2<<20)+1)} {
		if _, _, err := Normalize(raw); err == nil {
			t.Fatal("accepted invalid or oversized image")
		}
	}
	if ValidID(strings.Repeat("A", 64)) || ValidID(strings.Repeat("a", 63)) || ValidID("../../private") {
		t.Fatal("accepted an invalid content id")
	}
}
