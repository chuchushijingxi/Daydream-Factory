// Package logoimage normalizes the immutable PNG logos used by platform and model settings.
package logoimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"math"
	"strings"
)

var ErrInvalid = errors.New("invalid logo image")

func ValidID(id string) bool {
	raw, e := hex.DecodeString(id)
	return e == nil && len(raw) == 32 && strings.ToLower(id) == id
}

func Normalize(raw []byte) (string, []byte, error) {
	if len(raw) == 0 || len(raw) > 2<<20 {
		return "", nil, ErrInvalid
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return "", nil, ErrInvalid
	}
	src, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return "", nil, ErrInvalid
	}
	// 按比例缩入 96px 透明方形，双线性采样保持小 Logo 的现有显示效果。
	dst := image.NewNRGBA(image.Rect(0, 0, 96, 96))
	scale := math.Min(96/float64(cfg.Width), 96/float64(cfg.Height))
	width, height := int(math.Max(1, math.Round(float64(cfg.Width)*scale))), int(math.Max(1, math.Round(float64(cfg.Height)*scale)))
	left, top := (96-width)/2, (96-height)/2
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fx := math.Max(0, math.Min(float64(cfg.Width-1), (float64(x)+.5)/scale-.5))
			fy := math.Max(0, math.Min(float64(cfg.Height-1), (float64(y)+.5)/scale-.5))
			x0, y0 := int(fx), int(fy)
			x1, y1 := min(x0+1, cfg.Width-1), min(y0+1, cfg.Height-1)
			weights := [4]float64{(1 - (fx - float64(x0))) * (1 - (fy - float64(y0))), (fx - float64(x0)) * (1 - (fy - float64(y0))), (1 - (fx - float64(x0))) * (fy - float64(y0)), (fx - float64(x0)) * (fy - float64(y0))}
			points := [4]image.Point{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}}
			values := [4]float64{}
			for i, p := range points {
				col := color.NRGBAModel.Convert(src.At(p.X+src.Bounds().Min.X, p.Y+src.Bounds().Min.Y)).(color.NRGBA)
				values[0] += float64(col.R) * weights[i]
				values[1] += float64(col.G) * weights[i]
				values[2] += float64(col.B) * weights[i]
				values[3] += float64(col.A) * weights[i]
			}
			dst.SetNRGBA(x+left, y+top, color.NRGBA{uint8(math.Round(values[0])), uint8(math.Round(values[1])), uint8(math.Round(values[2])), uint8(math.Round(values[3]))})
		}
	}
	var out bytes.Buffer
	if e = png.Encode(&out, dst); e != nil {
		return "", nil, e
	}
	hash := sha256.Sum256(out.Bytes())
	id := hex.EncodeToString(hash[:])
	return id, out.Bytes(), nil
}
