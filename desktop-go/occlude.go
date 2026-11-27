package main

// Gripper occlusion. In the scan pose the black front/back grippers touch the cube and,
// seen from the camera, can cover part of the top face. Black plastic is treated as an
// external object: its pixels are ignored, a sticker that is mostly hidden becomes
// unknown ('?'), and unknown stickers are filled in afterwards from the rest of the cube
// (every piece exists exactly once, colour counts, twist/flip/parity).

import (
	"bytes"
	"encoding/base64"
	"image"
)

// isGripperPixel: black / very dark grey (gripper, shadow under it). Coloured stickers
// stay saturated even in shadow, so dark + unsaturated is the gripper.
func isGripperPixel(r, g, b float64) bool {
	_, s, v := hsv(r, g, b)
	return v < 0.20 || (v < 0.36 && s < 0.35)
}

const minVisible = 0.40 // a sticker needs 40% visible sticker pixels to be read

type tileVis struct {
	Visible float64
	RGB     [3]int // mean of the visible pixels
	OK      bool   // RGB is valid
}

// thumbTiles measures each of the 9 stickers in the face thumbnail (same layout as the
// tiles: row-major, u = column). Only the inner 50% of a sticker is used (no gaps).
func thumbTiles(b64 string) ([9]tileVis, bool) {
	var out [9]tileVis
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return out, false
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return out, false
	}
	r := img.Bounds()
	w, h := float64(r.Dx()), float64(r.Dy())
	var sum [9][3]float64
	var vis, tot [9]int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			u := (float64(x-r.Min.X) + 0.5) / w * 3
			v := (float64(y-r.Min.Y) + 0.5) / h * 3
			c, rr := int(u), int(v)
			fu, fv := u-float64(c), v-float64(rr)
			if c > 2 || rr > 2 || fu < 0.25 || fu > 0.75 || fv < 0.25 || fv > 0.75 {
				continue
			}
			k := rr*3 + c
			R, G, B, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(R>>8), float64(G>>8), float64(B>>8)
			tot[k]++
			if isGripperPixel(fr, fg, fb) {
				continue
			}
			vis[k]++
			sum[k][0] += fr
			sum[k][1] += fg
			sum[k][2] += fb
		}
	}
	for k := 0; k < 9; k++ {
		if tot[k] == 0 {
			continue
		}
		out[k].Visible = float64(vis[k]) / float64(tot[k])
		if vis[k] > 0 {
			n := float64(vis[k])
			out[k].RGB = [3]int{int(sum[k][0] / n), int(sum[k][1] / n), int(sum[k][2] / n)}
			out[k].OK = true
		}
	}
	return out, true
}

// completeHidden fills the '?' stickers of a colour net (centres known). status:
// "ok" unique fill (or nothing to fill), "ambiguous" several fills possible,
// "impossible" the known stickers do not make a valid cube.
func completeHidden(colors string) (string, string) {
	if !bytes.Contains([]byte(colors), []byte("?")) {
		if validColors(colors) {
			return colors, "ok"
		}
		return colors, "impossible"
	}
	fac, err := colorsToFacelets(colors)
	if err != nil {
		return colors, "impossible"
	}
	toCol := map[byte]byte{}
	for i := 0; i < 6; i++ {
		toCol[FACES[i]] = colors[9*i+4]
	}
	s := []byte(fac)

	type cand struct{ piece, ori int }
	var cc [8][]cand
	for i := 0; i < 8; i++ {
		for k := 0; k < 8; k++ {
			for o := 0; o < 3; o++ {
				ok := true
				for n := 0; n < 3; n++ {
					f := s[cornerFacelet[i][(n+o)%3]]
					if f != '?' && f != cornerColor[k][n] {
						ok = false
						break
					}
				}
				if ok {
					cc[i] = append(cc[i], cand{k, o})
				}
			}
		}
		if len(cc[i]) == 0 {
			return colors, "impossible"
		}
	}
	var ec [12][]cand
	for i := 0; i < 12; i++ {
		for k := 0; k < 12; k++ {
			for o := 0; o < 2; o++ {
				ok := true
				for n := 0; n < 2; n++ {
					f := s[edgeFacelet[i][(n+o)%2]]
					if f != '?' && f != edgeColor[k][n] {
						ok = false
						break
					}
				}
				if ok {
					ec[i] = append(ec[i], cand{k, o})
				}
			}
		}
		if len(ec[i]) == 0 {
			return colors, "impossible"
		}
	}

	var cp, co [8]int
	var ep, eo [12]int
	var usedC [8]bool
	var usedE [12]bool
	found := 0
	var first []byte
	nodes := 0
	const nodeLimit = 3000000
	parity := func(p []int) int {
		n := 0
		for i := 0; i < len(p); i++ {
			for j := i + 1; j < len(p); j++ {
				if p[i] > p[j] {
					n++
				}
			}
		}
		return n % 2
	}
	var cpPar int
	var edges func(i int) bool
	edges = func(i int) bool { // returns true to stop
		nodes++
		if nodes > nodeLimit {
			return true
		}
		if i == 12 {
			sum := 0
			for _, o := range eo {
				sum += o
			}
			if sum%2 != 0 || parity(ep[:]) != cpPar {
				return false
			}
			out := []byte(colors)
			for j := 0; j < 8; j++ {
				for n := 0; n < 3; n++ {
					out[cornerFacelet[j][(n+co[j])%3]] = toCol[cornerColor[cp[j]][n]]
				}
			}
			for j := 0; j < 12; j++ {
				for n := 0; n < 2; n++ {
					out[edgeFacelet[j][(n+eo[j])%2]] = toCol[edgeColor[ep[j]][n]]
				}
			}
			found++
			if found == 1 {
				first = out
				return false
			}
			return !bytes.Equal(first, out)
		}
		for _, c := range ec[i] {
			if usedE[c.piece] {
				continue
			}
			usedE[c.piece] = true
			ep[i], eo[i] = c.piece, c.ori
			stop := edges(i + 1)
			usedE[c.piece] = false
			if stop {
				return true
			}
		}
		return false
	}
	var corners func(i int) bool
	corners = func(i int) bool {
		nodes++
		if nodes > nodeLimit {
			return true
		}
		if i == 8 {
			sum := 0
			for _, o := range co {
				sum += o
			}
			if sum%3 != 0 {
				return false
			}
			cpPar = parity(cp[:])
			return edges(0)
		}
		for _, c := range cc[i] {
			if usedC[c.piece] {
				continue
			}
			usedC[c.piece] = true
			cp[i], co[i] = c.piece, c.ori
			stop := corners(i + 1)
			usedC[c.piece] = false
			if stop {
				return true
			}
		}
		return false
	}
	corners(0)
	switch {
	case found == 0 && nodes <= nodeLimit:
		return colors, "impossible"
	case found == 1 && nodes <= nodeLimit:
		return string(first), "ok"
	default:
		return colors, "ambiguous"
	}
}

// fixScan turns the classified scan (with '?' for hidden stickers) into a valid cube:
// unique fill first; otherwise also try up to two faces turned (thumbnail rotated).
func fixScan(cols string) (string, [6]int, string) {
	var zero [6]int
	if !bytes.Contains([]byte(cols), []byte("?")) {
		fixed, rots, ok := autoOrient(cols)
		if ok {
			return fixed, rots, "ok"
		}
		return cols, zero, "impossible"
	}
	best, status := completeHidden(cols)
	if status != "impossible" {
		return best, zero, status
	}
	for n := 1; n <= 2; n++ {
		for code := 0; code < 4096; code++ {
			var r [6]int
			c, cnt := code, 0
			s := cols
			for fi := 0; fi < 6; fi++ {
				r[fi] = c % 4
				c /= 4
				if r[fi] != 0 {
					cnt++
				}
			}
			if cnt != n {
				continue
			}
			for fi := 0; fi < 6; fi++ {
				if r[fi] != 0 {
					s = rotateFace(s, fi, r[fi])
				}
			}
			if f, st := completeHidden(s); st != "impossible" {
				return f, r, st
			}
		}
	}
	return cols, zero, "impossible"
}
