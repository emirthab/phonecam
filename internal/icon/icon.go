// Package icon renders the PhoneCam icon (a phone with a camera lens and
// broadcast waves on a gradient tile) at any size, with an optional status dot.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Status dot colors for the tray icon.
var (
	StatusNone = color.RGBA{}
	StatusOK   = color.RGBA{34, 197, 94, 255}
	StatusWait = color.RGBA{245, 158, 11, 255}
	StatusErr  = color.RGBA{239, 68, 68, 255}
)

var (
	gradA = [3]float64{99, 102, 241} // indigo-500
	gradB = [3]float64{6, 182, 212}  // cyan-500
	lensC = [3]float64{49, 46, 129}  // indigo-900
)

// signed distance to a rounded box centered at (cx,cy) with half size (hw,hh)
func sdRoundBox(x, y, cx, cy, hw, hh, r float64) float64 {
	qx := math.Abs(x-cx) - hw + r
	qy := math.Abs(y-cy) - hh + r
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

func sdCircle(x, y, cx, cy, r float64) float64 { return math.Hypot(x-cx, y-cy) - r }

// ring segment centered at (cx,cy), radius r, half thickness t, between angles a0..a1 (radians, y up)
func sdArc(x, y, cx, cy, r, t, a0, a1 float64) float64 {
	dx, dy := x-cx, cy-y
	ang := math.Atan2(dy, dx)
	if ang >= a0 && ang <= a1 {
		return math.Abs(math.Hypot(dx, dy)-r) - t
	}
	// distance to the rounded end caps
	e0x, e0y := cx+r*math.Cos(a0), cy-r*math.Sin(a0)
	e1x, e1y := cx+r*math.Cos(a1), cy-r*math.Sin(a1)
	return math.Min(math.Hypot(x-e0x, y-e0y), math.Hypot(x-e1x, y-e1y)) - t
}

type layer struct {
	rgb [3]float64
	a   float64
}

func over(dst *layer, rgb [3]float64, a float64) {
	if a <= 0 {
		return
	}
	na := a + dst.a*(1-a)
	for i := 0; i < 3; i++ {
		dst.rgb[i] = (rgb[i]*a + dst.rgb[i]*dst.a*(1-a)) / na
	}
	dst.a = na
}

// coverage from a signed distance, for a pixel of size px (in unit coords)
func cov(d, px float64) float64 { return math.Max(0, math.Min(1, 0.5-d/px)) }

// Render draws the icon at size x size pixels. A non-zero status adds a dot.
func Render(size int, status color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	px := 1.0 / float64(size)
	small := size <= 24
	const ss = 4 // supersampling per axis
	for py := 0; py < size; py++ {
		for pxi := 0; pxi < size; pxi++ {
			var acc [4]float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(pxi) + (float64(sx)+0.5)/ss) * px
					y := (float64(py) + (float64(sy)+0.5)/ss) * px
					l := sample(x, y, px*ss, small, status)
					acc[0] += l.rgb[0] * l.a
					acc[1] += l.rgb[1] * l.a
					acc[2] += l.rgb[2] * l.a
					acc[3] += l.a
				}
			}
			n := float64(ss * ss)
			a := acc[3] / n
			c := color.RGBA{}
			if a > 0 {
				c = color.RGBA{
					R: uint8(math.Round(acc[0] / acc[3])),
					G: uint8(math.Round(acc[1] / acc[3])),
					B: uint8(math.Round(acc[2] / acc[3])),
					A: uint8(math.Round(a * 255)),
				}
			}
			img.SetRGBA(pxi, py, color.RGBA{
				R: uint8(uint16(c.R) * uint16(c.A) / 255),
				G: uint8(uint16(c.G) * uint16(c.A) / 255),
				B: uint8(uint16(c.B) * uint16(c.A) / 255),
				A: c.A,
			})
		}
	}
	return img
}

func sample(x, y, px float64, small bool, status color.RGBA) layer {
	var l layer
	// gradient tile
	t := math.Max(0, math.Min(1, (x+y)/2))
	var bg [3]float64
	for i := range bg {
		bg[i] = gradA[i]*(1-t) + gradB[i]*t
	}
	over(&l, bg, cov(sdRoundBox(x, y, 0.5, 0.5, 0.47, 0.47, 0.22), px))

	white := [3]float64{255, 255, 255}
	// phone body
	pcx, pcy := 0.42, 0.56
	over(&l, white, cov(sdRoundBox(x, y, pcx, pcy, 0.19, 0.31, 0.075), px))
	lcy, lr := 0.335, 0.05
	if small {
		lcy, lr = 0.36, 0.075
	} else {
		// screen: a lighter tint of the tile, leaving a white bezel
		var scr [3]float64
		for i := range scr {
			scr[i] = bg[i]*0.82 + 255*0.18
		}
		over(&l, scr, cov(sdRoundBox(x, y, pcx, pcy+0.06, 0.145, 0.2, 0.035), px))
	}
	// camera lens sitting in the top bezel
	over(&l, lensC, cov(sdCircle(x, y, pcx, lcy, lr), px))
	over(&l, white, 0.8*cov(sdCircle(x, y, pcx-lr*0.35, lcy-lr*0.35, lr*0.32), px))

	// broadcast waves to the upper right
	wcx, wcy := 0.60, 0.40
	th := 0.032
	if small {
		th = 0.05
	}
	over(&l, white, cov(sdArc(x, y, wcx, wcy, 0.17, th, -0.15, 1.15), px))
	if !small {
		over(&l, white, 0.9*cov(sdArc(x, y, wcx, wcy, 0.27, th, -0.05, 1.05), px))
	}

	// status dot
	if status.A != 0 {
		dcx, dcy := 0.79, 0.79
		over(&l, white, cov(sdCircle(x, y, dcx, dcy, 0.19), px))
		over(&l, [3]float64{float64(status.R), float64(status.G), float64(status.B)}, cov(sdCircle(x, y, dcx, dcy, 0.14), px))
	}
	return l
}

// ICO encodes PNG-compressed images of the given sizes into a .ico file.
func ICO(sizes []int) []byte {
	var imgs [][]byte
	for _, s := range sizes {
		var b bytes.Buffer
		png.Encode(&b, Render(s, StatusNone))
		imgs = append(imgs, b.Bytes())
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := uint8(s)
		if s >= 256 {
			dim = 0
		}
		binary.Write(&out, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(imgs[i])), uint32(offset)})
		offset += len(imgs[i])
	}
	for _, b := range imgs {
		out.Write(b)
	}
	return out.Bytes()
}
