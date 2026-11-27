package main

// Centre-colour detection: instead of assuming which colour sits in the front arm and
// on top, the scan measures the centres. Two adjacent centres fix the orientation of a
// standard cube (24 possibilities), so it is decided after the first two faces and
// re-checked with all six at the end. A GAN logo on a centre means that face is white.

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	"math"
	"sort"
)

type centerObs struct {
	Face byte   // home-frame face letter (U R F D L B) of the scanned centre
	RGB  [3]int // measured centre colour
	Logo bool   // GAN logo seen on this centre
}

type orientChoice struct {
	Front, Top byte
	Cost       float64
}

// rankOrientations scores every valid (front, top) pair against the observations,
// best first.
func rankOrientations(obs []centerObs) []orientChoice {
	out := []orientChoice{}
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			f, t := COLORS[i], COLORS[j]
			cc, err := centerColors(f, t)
			if err != nil {
				continue
			}
			cost := 0.0
			for _, o := range obs {
				want := cc[o.Face]
				switch {
				case o.Logo && want == 'W':
				case o.Logo:
					cost += 500 // logo is only on the white centre
				default:
					cost += labDist(rgbToLab(o.RGB), rgbToLab(defaultRGB[want]))
				}
			}
			out = append(out, orientChoice{f, t, cost})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Cost < out[b].Cost })
	return out
}

// faceLabel names a home-frame face by where it sits in the robot.
func faceLabel(f byte) string {
	return map[byte]string{'U': "top", 'D': "bottom", 'F': "front-arm", 'B': "back-arm",
		'R': "right-arm", 'L': "left-arm"}[f] + " face"
}

// logoInThumb looks for the GAN logo (white cap with a blue mark) in the centre of a
// face picture. It samples a disc, so it does not matter which way the logo faces or
// whether the face is twisted 45 degrees.
func logoInThumb(b64 string) (bool, bool) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return false, false
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return false, false
	}
	r := img.Bounds()
	w, h := float64(r.Dx()), float64(r.Dy())
	cx, cy := float64(r.Min.X)+w/2, float64(r.Min.Y)+h/2
	rad := 0.45 * w / 3 // disc inside the centre sticker
	white, blue, total := 0, 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) > rad {
				continue
			}
			R, G, B, _ := img.At(x, y).RGBA()
			hh, s, v := hsv(float64(R>>8), float64(G>>8), float64(B>>8))
			total++
			if s < 0.28 && v > 0.45 {
				white++
			}
			if hh >= 190 && hh <= 265 && s > 0.35 && v > 0.18 {
				blue++
			}
		}
	}
	if total == 0 {
		return false, false
	}
	wf, bf := float64(white)/float64(total), float64(blue)/float64(total)
	return wf >= 0.30 && bf >= 0.03 && bf <= 0.55, true
}

func hsv(r, g, b float64) (h, s, v float64) {
	r, g, b = r/255, g/255, b/255
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	d := mx - mn
	switch {
	case d == 0:
		h = 0
	case mx == r:
		h = 60 * math.Mod((g-b)/d, 6)
	case mx == g:
		h = 60 * ((b-r)/d + 2)
	default:
		h = 60 * ((r-g)/d + 4)
	}
	if h < 0 {
		h += 360
	}
	if mx > 0 {
		s = d / mx
	}
	return h, s, mx
}
