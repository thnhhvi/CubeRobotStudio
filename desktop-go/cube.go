package main

// Geometric facelet model in the robot frame (port of cuberobot/cube.py).
// +x = RIGHT arm face, +y = top, +z = FRONT arm face.

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"strings"
)

type vec [3]int
type mat [3][3]int

const COLORS = "WYROBG"

var colorNames = map[byte]string{'W': "white", 'Y': "yellow", 'R': "red", 'O': "orange", 'B': "blue", 'G': "green"}
var opposite = map[byte]byte{'W': 'Y', 'Y': 'W', 'R': 'O', 'O': 'R', 'B': 'G', 'G': 'B'}

var faceFrame = map[byte][3]vec{
	'U': {{0, 1, 0}, {1, 0, 0}, {0, 0, 1}},
	'R': {{1, 0, 0}, {0, 0, -1}, {0, -1, 0}},
	'F': {{0, 0, 1}, {1, 0, 0}, {0, -1, 0}},
	'D': {{0, -1, 0}, {1, 0, 0}, {0, 0, -1}},
	'L': {{-1, 0, 0}, {0, 0, 1}, {0, -1, 0}},
	'B': {{0, 0, -1}, {-1, 0, 0}, {0, -1, 0}},
}

type geo struct{ p, n vec }

var GEOM []geo
var geoIndex = map[geo]int{}
var movePerm = map[string][54]int{}
var identM = mat{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
var rotations []mat

func vadd(a, b vec) vec     { return vec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func vmul(k int, a vec) vec { return vec{k * a[0], k * a[1], k * a[2]} }
func vdot(a, b vec) int     { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func mvec(m mat, v vec) vec {
	return vec{m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2], m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2],
		m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2]}
}
func mmul(a, b mat) mat {
	var r mat
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				r[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return r
}

// quarter turns clockwise looking from +axis
func rotAbout(axis vec, q int) mat {
	q = ((q % 4) + 4) % 4
	ang := -math.Pi / 2 * float64(q)
	c, s := int(math.Round(math.Cos(ang))), int(math.Round(math.Sin(ang)))
	x, y, z := axis[0], axis[1], axis[2]
	K := mat{{0, -z, y}, {z, 0, -x}, {-y, x, 0}}
	KK := mmul(K, K)
	var m mat
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			d := 0
			if i == j {
				d = 1
			}
			m[i][j] = d + s*K[i][j] + (1-c)*KK[i][j]
		}
	}
	return m
}

func axisOf(f byte) vec { return faceFrame[f][0] }

func permFromRotation(m mat, layer *vec) [54]int {
	var perm [54]int
	for i := range perm {
		perm[i] = i
	}
	for src, g := range GEOM {
		if layer != nil && vdot(g.p, *layer) <= 0 {
			continue
		}
		dst := geoIndex[geo{mvec(m, g.p), mvec(m, g.n)}]
		perm[dst] = src
	}
	return perm
}

func init() {
	for i := 0; i < 6; i++ {
		f := FACES[i]
		fr := faceFrame[f]
		for row := 0; row < 3; row++ {
			for col := 0; col < 3; col++ {
				p := vadd(vadd(vmul(3, fr[0]), vmul(2*(col-1), fr[1])), vmul(2*(row-1), fr[2]))
				g := geo{p, fr[0]}
				geoIndex[g] = len(GEOM)
				GEOM = append(GEOM, g)
			}
		}
	}
	sufs := []string{"", "2", "'"}
	for i := 0; i < 6; i++ {
		f := FACES[i]
		ax := axisOf(f)
		for q := 1; q <= 3; q++ {
			movePerm[string(f)+sufs[q-1]] = permFromRotation(rotAbout(ax, q), &ax)
		}
	}
	for _, r := range []struct {
		n string
		f byte
	}{{"x", 'R'}, {"y", 'U'}, {"z", 'F'}} {
		for q := 1; q <= 3; q++ {
			movePerm[r.n+sufs[q-1]] = permFromRotation(rotAbout(axisOf(r.f), q), nil)
		}
	}
	// all 24 rotations
	gens := []mat{rotAbout(axisOf('R'), 1), rotAbout(axisOf('U'), 1), rotAbout(axisOf('F'), 1)}
	seen := map[mat]bool{}
	stack := []mat{identM}
	for len(stack) > 0 {
		m := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[m] {
			continue
		}
		seen[m] = true
		rotations = append(rotations, m)
		for _, g := range gens {
			stack = append(stack, mmul(g, m))
		}
	}
}

func applyMove(state string, mv string) string {
	p := movePerm[mv]
	b := make([]byte, 54)
	for i := 0; i < 54; i++ {
		b[i] = state[p[i]]
	}
	return string(b)
}

func applyMoves(state string, moves []string) string {
	for _, m := range moves {
		state = applyMove(state, m)
	}
	return state
}

var standardScheme = map[vec]byte{{0, 1, 0}: 'W', {0, -1, 0}: 'Y', {0, 0, 1}: 'G', {0, 0, -1}: 'B', {1, 0, 0}: 'R', {-1, 0, 0}: 'O'}

func centerColors(front, top byte) (map[byte]byte, error) {
	if front == top || opposite[front] == top {
		return nil, errors.New("Front and top colours must be adjacent")
	}
	for _, m := range rotations {
		col := map[vec]byte{}
		for n, c := range standardScheme {
			col[mvec(m, n)] = c
		}
		if col[vec{0, 1, 0}] == top && col[vec{0, 0, 1}] == front {
			out := map[byte]byte{}
			for i := 0; i < 6; i++ {
				out[FACES[i]] = col[axisOf(FACES[i])]
			}
			return out, nil
		}
	}
	return nil, errors.New("No orientation found")
}

func solvedColors(front, top byte) string {
	cc, err := centerColors(front, top)
	if err != nil {
		cc, _ = centerColors('R', 'W')
	}
	var sb strings.Builder
	for i := 0; i < 6; i++ {
		sb.WriteString(strings.Repeat(string(cc[FACES[i]]), 9))
	}
	return sb.String()
}

func colorsToFacelets(colors string) (string, error) {
	cmap := map[byte]byte{}
	for i := 0; i < 6; i++ {
		cmap[colors[9*i+4]] = FACES[i]
	}
	if len(cmap) != 6 {
		return "", errors.New("Centre colours are not 6 distinct colours")
	}
	b := make([]byte, 54)
	for i := 0; i < 54; i++ {
		if f, ok := cmap[colors[i]]; ok {
			b[i] = f
		} else {
			b[i] = '?'
		}
	}
	return string(b), nil
}

// ---------------------------------------------------------------- colour classification
type lab [3]float64

func srgbLin(c float64) float64 {
	c /= 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func rgbToLab(rgb [3]int) lab {
	r, g, b := srgbLin(float64(rgb[0])), srgbLin(float64(rgb[1])), srgbLin(float64(rgb[2]))
	x := (0.4124*r + 0.3576*g + 0.1805*b) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := (0.0193*r + 0.1192*g + 0.9505*b) / 1.08883
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return lab{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

func labDist(a, b lab) float64 {
	return math.Sqrt(0.5*(a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2]))
}

var defaultRGB = map[byte][3]int{'W': {235, 235, 235}, 'Y': {240, 220, 40}, 'R': {200, 20, 30},
	'O': {250, 120, 20}, 'B': {20, 70, 200}, 'G': {20, 170, 70}}

func nearestDefault(rgb [3]int) byte {
	l := rgbToLab(rgb)
	best, bd := byte('W'), 1e18
	for i := 0; i < 6; i++ {
		c := COLORS[i]
		if d := labDist(l, rgbToLab(defaultRGB[c])); d < bd {
			best, bd = c, d
		}
	}
	return best
}

// classify: samples for all 54 home facelets; centres from the expected orientation
func classify(samples map[int][3]int, centers map[byte]byte) string {
	refs := map[byte]lab{}
	for i := 0; i < 6; i++ {
		c := centers[FACES[i]]
		if s, ok := samples[9*i+4]; ok && c != 'W' {
			refs[c] = rgbToLab(s)
		} else {
			refs[c] = rgbToLab(defaultRGB[c])
		}
	}
	labs := map[int]lab{}
	idx := []int{}
	for i, v := range samples {
		labs[i] = rgbToLab(v)
	}
	for i := 0; i < 54; i++ {
		if _, ok := labs[i]; ok && i%9 != 4 {
			idx = append(idx, i)
		}
	}
	// white reference: brightest low-chroma stickers
	ws := append([]int(nil), idx...)
	score := func(l lab) float64 { return l[0] - 2*math.Hypot(l[1], l[2]) }
	sort.Slice(ws, func(a, b int) bool { return score(labs[ws[a]]) > score(labs[ws[b]]) })
	if len(ws) > 6 {
		ws = ws[:6]
	}
	if len(ws) > 0 {
		var w lab
		for _, i := range ws {
			for k := 0; k < 3; k++ {
				w[k] += labs[i][k] / float64(len(ws))
			}
		}
		refs['W'] = w
	}
	out := make([]byte, 54)
	for i := range out {
		out[i] = 0
	}
	for i := 0; i < 6; i++ {
		out[9*i+4] = centers[FACES[i]]
	}
	type pair struct {
		i int
		c byte
		d float64
	}
	cost := map[int]map[byte]float64{}
	pairs := []pair{}
	for _, i := range idx {
		cost[i] = map[byte]float64{}
		for k := 0; k < 6; k++ {
			c := COLORS[k]
			d := labDist(labs[i], refs[c])
			cost[i][c] = d
			pairs = append(pairs, pair{i, c, d})
		}
	}
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a].d < pairs[b].d })
	count := map[byte]int{}
	for k := 0; k < 6; k++ {
		count[COLORS[k]] = 1
	}
	for _, p := range pairs {
		if out[p.i] == 0 && count[p.c] < 9 {
			out[p.i] = p.c
			count[p.c]++
		}
	}
	for improved := true; improved; {
		improved = false
		for x := 0; x < len(idx); x++ {
			for y := x + 1; y < len(idx); y++ {
				a, b := idx[x], idx[y]
				ca, cb := out[a], out[b]
				if ca == cb || ca == 0 || cb == 0 {
					continue
				}
				if cost[a][cb]+cost[b][ca]+1e-9 < cost[a][ca]+cost[b][cb] {
					out[a], out[b] = cb, ca
					improved = true
				}
			}
		}
	}
	for i := range out {
		if out[i] == 0 {
			out[i] = '?'
		}
	}
	return string(out)
}

var qrot = [9]int{6, 3, 0, 7, 4, 1, 8, 5, 2}

func rotateFace(colors string, fi, q int) string {
	s := []byte(colors)
	blk := append([]byte(nil), s[9*fi:9*fi+9]...)
	for t := 0; t < ((q%4)+4)%4; t++ {
		nb := make([]byte, 9)
		for k := 0; k < 9; k++ {
			nb[k] = blk[qrot[k]]
		}
		blk = nb
	}
	copy(s[9*fi:], blk)
	return string(s)
}

func validColors(c string) bool {
	f, err := colorsToFacelets(c)
	return err == nil && verifyFacelets(f) == nil
}

// autoOrient tries all 4^6 face rotations; returns fixed colours, rotations, ok
func autoOrient(colors string) (string, [6]int, bool) {
	var zero [6]int
	if validColors(colors) {
		return colors, zero, true
	}
	bestN := 99
	var best string
	var bestR [6]int
	for code := 0; code < 4096; code++ {
		var r [6]int
		s := colors
		n := 0
		c := code
		for fi := 0; fi < 6; fi++ {
			r[fi] = c % 4
			c /= 4
			if r[fi] != 0 {
				s = rotateFace(s, fi, r[fi])
				n++
			}
		}
		if n < bestN && validColors(s) {
			bestN, best, bestR = n, s, r
		}
	}
	if bestN == 99 {
		return colors, zero, false
	}
	return best, bestR, true
}

var oppositeFace = map[byte]byte{'U': 'D', 'D': 'U', 'R': 'L', 'L': 'R', 'F': 'B', 'B': 'F'}

func randomScramble(n int, r *rand.Rand) []string {
	out := []string{}
	var last byte
	for i := 0; i < n; i++ {
		var f byte
		for {
			f = FACES[r.Intn(6)]
			if f != last && !(last != 0 && oppositeFace[f] == last && r.Float64() < 0.5) {
				break
			}
		}
		last = f
		out = append(out, string(f)+[]string{"", "2", "'"}[r.Intn(3)])
	}
	return out
}
