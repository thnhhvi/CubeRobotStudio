package main

// Kociemba two-phase solver (port of desktop/cuberobot/twophase.py).

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const (
	URF = iota
	UFL
	ULB
	UBR
	DFR
	DLF
	DBL
	DRB
)
const (
	UR = iota
	UF
	UL
	UB
	DR
	DF
	DL
	DB
	FR
	FL
	BL
	BR
)

const FACES = "URFDLB"

var cornerFacelet = [8][3]int{
	{8, 9, 20}, {6, 18, 38}, {0, 36, 47}, {2, 45, 11},
	{29, 26, 15}, {27, 44, 24}, {33, 53, 42}, {35, 17, 51},
}
var edgeFacelet = [12][2]int{
	{5, 10}, {7, 19}, {3, 37}, {1, 46}, {32, 16}, {28, 25},
	{30, 43}, {34, 52}, {23, 12}, {21, 41}, {50, 39}, {48, 14},
}
var cornerColor = [8]string{"URF", "UFL", "ULB", "UBR", "DFR", "DLF", "DBL", "DRB"}
var edgeColor = [12]string{"UR", "UF", "UL", "UB", "DR", "DF", "DL", "DB", "FR", "FL", "BL", "BR"}

type Cubie struct {
	cp [8]int
	co [8]int
	ep [12]int
	eo [12]int
}

func identity() Cubie {
	var c Cubie
	for i := 0; i < 8; i++ {
		c.cp[i] = i
	}
	for i := 0; i < 12; i++ {
		c.ep[i] = i
	}
	return c
}

func (a *Cubie) cornerMul(b *Cubie) {
	var cp, co [8]int
	for i := 0; i < 8; i++ {
		cp[i] = a.cp[b.cp[i]]
		co[i] = (a.co[b.cp[i]] + b.co[i]) % 3
	}
	a.cp, a.co = cp, co
}

func (a *Cubie) edgeMul(b *Cubie) {
	var ep, eo [12]int
	for i := 0; i < 12; i++ {
		ep[i] = a.ep[b.ep[i]]
		eo[i] = (a.eo[b.ep[i]] + b.eo[i]) % 2
	}
	a.ep, a.eo = ep, eo
}

func (a *Cubie) mul(b *Cubie) { a.cornerMul(b); a.edgeMul(b) }

func binom(n, k int) int {
	if k < 0 || k > n {
		return 0
	}
	r := 1
	for i := 0; i < k; i++ {
		r = r * (n - i) / (i + 1)
	}
	return r
}

func rotLeft(a []int, l, r int) {
	t := a[l]
	for i := l; i < r; i++ {
		a[i] = a[i+1]
	}
	a[r] = t
}

func rotRight(a []int, l, r int) {
	t := a[r]
	for i := r; i > l; i-- {
		a[i] = a[i-1]
	}
	a[l] = t
}

func (c *Cubie) twist() int {
	r := 0
	for i := 0; i < 7; i++ {
		r = 3*r + c.co[i]
	}
	return r
}
func (c *Cubie) setTwist(t int) {
	s := 0
	for i := 6; i >= 0; i-- {
		c.co[i] = t % 3
		s += c.co[i]
		t /= 3
	}
	c.co[7] = ((-s)%3 + 3) % 3
}
func (c *Cubie) flip() int {
	r := 0
	for i := 0; i < 11; i++ {
		r = 2*r + c.eo[i]
	}
	return r
}
func (c *Cubie) setFlip(f int) {
	s := 0
	for i := 10; i >= 0; i-- {
		c.eo[i] = f % 2
		s += c.eo[i]
		f /= 2
	}
	c.eo[11] = ((-s)%2 + 2) % 2
}
func (c *Cubie) sliceSorted() int {
	a, x := 0, 0
	edge4 := []int{}
	for j := 11; j >= 0; j-- {
		if c.ep[j] >= FR && c.ep[j] <= BR {
			a += binom(11-j, x+1)
			edge4 = append([]int{c.ep[j]}, edge4...)
			x++
		}
	}
	b := 0
	for j := 3; j > 0; j-- {
		k := 0
		for edge4[j] != j+8 {
			rotLeft(edge4, 0, j)
			k++
		}
		b = (j+1)*b + k
	}
	return 24*a + b
}
func (c *Cubie) setSliceSorted(idx int) {
	slice := []int{FR, FL, BL, BR}
	other := []int{UR, UF, UL, UB, DR, DF, DL, DB}
	b := idx % 24
	a := idx / 24
	for j := 0; j < 12; j++ {
		c.ep[j] = -1
	}
	for j := 1; j < 4; j++ {
		k := b % (j + 1)
		b /= j + 1
		for ; k > 0; k-- {
			rotRight(slice, 0, j)
		}
	}
	x := 4
	for j := 0; j < 12; j++ {
		if x > 0 && a-binom(11-j, x) >= 0 {
			c.ep[j] = slice[4-x]
			a -= binom(11-j, x)
			x--
		}
	}
	x = 0
	for j := 0; j < 12; j++ {
		if c.ep[j] == -1 {
			c.ep[j] = other[x]
			x++
		}
	}
}
func permCoord(p []int) int {
	perm := append([]int(nil), p...)
	b := 0
	for j := len(perm) - 1; j > 0; j-- {
		k := 0
		for perm[j] != j {
			rotLeft(perm, 0, j)
			k++
		}
		b = (j+1)*b + k
	}
	return b
}
func setPerm(p []int, idx int) {
	for i := range p {
		p[i] = i
	}
	for j := 1; j < len(p); j++ {
		k := idx % (j + 1)
		idx /= j + 1
		for ; k > 0; k-- {
			rotRight(p, 0, j)
		}
	}
}
func (c *Cubie) corners() int     { return permCoord(c.cp[:]) }
func (c *Cubie) setCorners(i int) { setPerm(c.cp[:], i) }
func (c *Cubie) udEdges() int     { return permCoord(c.ep[:8]) }
func (c *Cubie) setUdEdges(i int) { setPerm(c.ep[:8], i) }
func parity(p []int) int {
	s := 0
	for i := len(p) - 1; i > 0; i-- {
		for j := i - 1; j >= 0; j-- {
			if p[j] > p[i] {
				s++
			}
		}
	}
	return s % 2
}

func (c *Cubie) verify() error {
	seenE := map[int]bool{}
	for _, e := range c.ep {
		if e < 0 || seenE[e] {
			return errors.New("Some edges are missing or duplicated")
		}
		seenE[e] = true
	}
	s := 0
	for _, o := range c.eo {
		s += o
	}
	if s%2 != 0 {
		return errors.New("An edge is flipped (flip parity error)")
	}
	seenC := map[int]bool{}
	for _, x := range c.cp {
		if x < 0 || seenC[x] {
			return errors.New("Some corners are missing or duplicated")
		}
		seenC[x] = true
	}
	s = 0
	for _, o := range c.co {
		s += o
	}
	if s%3 != 0 {
		return errors.New("A corner is twisted (twist parity error)")
	}
	if parity(c.ep[:]) != parity(c.cp[:]) {
		return errors.New("Two pieces are swapped (permutation parity error)")
	}
	return nil
}

var basicMoves = map[byte]Cubie{
	'U': {cp: [8]int{UBR, URF, UFL, ULB, DFR, DLF, DBL, DRB},
		ep: [12]int{UB, UR, UF, UL, DR, DF, DL, DB, FR, FL, BL, BR}},
	'R': {cp: [8]int{DFR, UFL, ULB, URF, DRB, DLF, DBL, UBR}, co: [8]int{2, 0, 0, 1, 1, 0, 0, 2},
		ep: [12]int{FR, UF, UL, UB, BR, DF, DL, DB, DR, FL, BL, UR}},
	'F': {cp: [8]int{UFL, DLF, ULB, UBR, URF, DFR, DBL, DRB}, co: [8]int{1, 2, 0, 0, 2, 1, 0, 0},
		ep: [12]int{UR, FL, UL, UB, DR, FR, DL, DB, UF, DF, BL, BR}, eo: [12]int{0, 1, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0}},
	'D': {cp: [8]int{URF, UFL, ULB, UBR, DLF, DBL, DRB, DFR},
		ep: [12]int{UR, UF, UL, UB, DF, DL, DB, DR, FR, FL, BL, BR}},
	'L': {cp: [8]int{URF, ULB, DBL, UBR, DFR, UFL, DLF, DRB}, co: [8]int{0, 1, 2, 0, 0, 2, 1, 0},
		ep: [12]int{UR, UF, BL, UB, DR, DF, FL, DB, FR, UL, DL, BR}},
	'B': {cp: [8]int{URF, UFL, UBR, DRB, DFR, DLF, ULB, DBL}, co: [8]int{0, 0, 1, 2, 0, 0, 2, 1},
		ep: [12]int{UR, UF, UL, BR, DR, DF, DL, BL, FR, FL, UB, DB}, eo: [12]int{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1}},
}

var moveNames [18]string
var moveCubes [18]Cubie
var phase2Moves []int
var isP2 [18]bool

func init() {
	k := 0
	for i := 0; i < 6; i++ {
		f := FACES[i]
		c := identity()
		b := basicMoves[f]
		for p := 0; p < 3; p++ {
			c.mul(&b)
			moveCubes[k] = c
			moveNames[k] = string(f) + []string{"", "2", "'"}[p]
			k++
		}
	}
	for m := 0; m < 18; m++ {
		if moveNames[m][0] == 'U' || moveNames[m][0] == 'D' || strings.HasSuffix(moveNames[m], "2") {
			phase2Moves = append(phase2Moves, m)
			isP2[m] = true
		}
	}
}

func moveIndex(name string) int {
	for i, n := range moveNames {
		if n == name {
			return i
		}
	}
	return -1
}

func faceletsToCubie(s string) (Cubie, error) {
	var cc Cubie
	if len(s) != 54 {
		return cc, errors.New("Cube string must have 54 facelets")
	}
	for i := 0; i < 6; i++ {
		if n := strings.Count(s, string(FACES[i])); n != 9 {
			return cc, fmt.Errorf("Each color must appear 9 times (%c: %d)", FACES[i], n)
		}
	}
	for i := range cc.cp {
		cc.cp[i] = -1
	}
	for i := range cc.ep {
		cc.ep[i] = -1
	}
	for i := 0; i < 8; i++ {
		fac := cornerFacelet[i]
		ori := 0
		for ori = 0; ori < 3; ori++ {
			if s[fac[ori]] == 'U' || s[fac[ori]] == 'D' {
				break
			}
		}
		if ori == 3 {
			return cc, errors.New("Invalid corner (no U/D sticker)")
		}
		c1 := s[fac[(ori+1)%3]]
		c2 := s[fac[(ori+2)%3]]
		for j := 0; j < 8; j++ {
			if c1 == cornerColor[j][1] && c2 == cornerColor[j][2] {
				cc.cp[i] = j
				cc.co[i] = ori
				break
			}
		}
	}
	for i := 0; i < 12; i++ {
		a, b := s[edgeFacelet[i][0]], s[edgeFacelet[i][1]]
		for j := 0; j < 12; j++ {
			if a == edgeColor[j][0] && b == edgeColor[j][1] {
				cc.ep[i], cc.eo[i] = j, 0
				break
			}
			if a == edgeColor[j][1] && b == edgeColor[j][0] {
				cc.ep[i], cc.eo[i] = j, 1
				break
			}
		}
	}
	return cc, nil
}

func cubieToFacelets(cc *Cubie) string {
	s := make([]byte, 54)
	for i := 0; i < 6; i++ {
		s[9*i+4] = FACES[i]
	}
	for i := 0; i < 8; i++ {
		for n := 0; n < 3; n++ {
			s[cornerFacelet[i][(n+cc.co[i])%3]] = cornerColor[cc.cp[i]][n]
		}
	}
	for i := 0; i < 12; i++ {
		for n := 0; n < 2; n++ {
			s[edgeFacelet[i][(n+cc.eo[i])%2]] = edgeColor[cc.ep[i]][n]
		}
	}
	return string(s)
}

func verifyFacelets(s string) error {
	cc, err := faceletsToCubie(s)
	if err != nil {
		return err
	}
	for _, x := range cc.cp {
		if x < 0 {
			return errors.New("Impossible piece (sticker colors do not form a real piece)")
		}
	}
	for _, x := range cc.ep {
		if x < 0 {
			return errors.New("Impossible piece (sticker colors do not form a real piece)")
		}
	}
	return cc.verify()
}

// ---------------------------------------------------------------- tables
const (
	nTwist   = 2187
	nFlip    = 2048
	nSlice   = 495
	nSS      = 11880
	nPerm8   = 40320
	nSlice24 = 24
)

type Tables struct {
	twistMove, flipMove, ssMove, cornMove, udeMove []uint16
	stp, sfp, csp, esp                             []uint8
}

func buildMove(n int, set func(*Cubie, int), get func(*Cubie) int, corner bool, moves []int) []uint16 {
	t := make([]uint16, n*18)
	for i := 0; i < n; i++ {
		a := identity()
		set(&a, i)
		for _, m := range moves {
			b := a
			if corner {
				b.cornerMul(&moveCubes[m])
			} else {
				b.edgeMul(&moveCubes[m])
			}
			t[i*18+m] = uint16(get(&b))
		}
	}
	return t
}

func bfs2(moveA []uint16, moveB []uint16, na, nb int, moves []int, sliceA bool) []uint8 {
	n := na * nb
	dist := make([]uint8, n)
	for i := range dist {
		dist[i] = 255
	}
	dist[0] = 0
	frontier := []int{0}
	depth := uint8(0)
	for len(frontier) > 0 {
		next := []int{}
		for _, idx := range frontier {
			a, b := idx/nb, idx%nb
			for _, m := range moves {
				var a2 int
				if sliceA {
					a2 = int(moveA[a*24*18+m]) / 24
				} else {
					a2 = int(moveA[a*18+m])
				}
				b2 := int(moveB[b*18+m])
				j := a2*nb + b2
				if dist[j] == 255 {
					dist[j] = depth + 1
					next = append(next, j)
				}
			}
		}
		depth++
		frontier = next
	}
	return dist
}

func NewTables() *Tables {
	all := make([]int, 18)
	for i := range all {
		all[i] = i
	}
	t := &Tables{}
	t.twistMove = buildMove(nTwist, (*Cubie).setTwist, (*Cubie).twist, true, all)
	t.flipMove = buildMove(nFlip, (*Cubie).setFlip, (*Cubie).flip, false, all)
	t.ssMove = buildMove(nSS, (*Cubie).setSliceSorted, (*Cubie).sliceSorted, false, all)
	t.cornMove = buildMove(nPerm8, (*Cubie).setCorners, (*Cubie).corners, true, all)
	t.udeMove = buildMove(nPerm8, (*Cubie).setUdEdges, (*Cubie).udEdges, false, phase2Moves)
	// slice (0..494) x twist / flip ; slice index uses slice_sorted = slice*24
	t.stp = bfs2(t.ssMove, t.twistMove, nSlice, nTwist, all, true)
	t.sfp = bfs2(t.ssMove, t.flipMove, nSlice, nFlip, all, true)
	t.csp = bfs2(t.cornMove, t.ssMove, nPerm8, nSlice24, phase2Moves, false)
	t.esp = bfs2(t.udeMove, t.ssMove, nPerm8, nSlice24, phase2Moves, false)
	return t
}

// ---------------------------------------------------------------- search
type Solver struct {
	t         *Tables
	cc        Cubie
	deadline  time.Time
	best      int
	solutions [][]string
	path      []int
	p2path    []int
	stop      bool
}

func (t *Tables) h1(tw, fl, sl int) uint8 {
	a := t.stp[sl*nTwist+tw]
	b := t.sfp[sl*nFlip+fl]
	if a > b {
		return a
	}
	return b
}

func Solve(t *Tables, cube string, maxLen int, timeout time.Duration) ([][]string, error) {
	if err := verifyFacelets(cube); err != nil {
		return nil, err
	}
	if cube == strings.Repeat("U", 9)+strings.Repeat("R", 9)+strings.Repeat("F", 9)+
		strings.Repeat("D", 9)+strings.Repeat("L", 9)+strings.Repeat("B", 9) {
		return [][]string{{}}, nil
	}
	cc, _ := faceletsToCubie(cube)
	s := &Solver{t: t, cc: cc, deadline: time.Now().Add(timeout), best: maxLen + 1}
	tw, fl, ss, co := cc.twist(), cc.flip(), cc.sliceSorted(), cc.corners()
	hardStop := time.Now().Add(timeout + 30*time.Second)
	for togo := int(t.h1(tw, fl, ss/24)); togo <= maxLen; togo++ {
		if togo >= s.best || s.stop || time.Now().After(hardStop) {
			break
		}
		s.phase1(tw, fl, ss, co, togo)
	}
	if len(s.solutions) == 0 {
		return nil, errors.New("No solution found within the time / length limit")
	}
	return s.solutions, nil
}

func (s *Solver) phase1(tw, fl, ss, co, togo int) {
	if s.stop {
		return
	}
	if togo == 0 {
		if len(s.path) > 0 && isP2[s.path[len(s.path)-1]] {
			return
		}
		s.startPhase2(ss, co)
		return
	}
	if len(s.solutions) > 0 && time.Now().After(s.deadline) {
		s.stop = true
		return
	}
	t := s.t
	last := -1
	if len(s.path) > 0 {
		last = s.path[len(s.path)-1]
	}
	lf := last / 3
	for m := 0; m < 18; m++ {
		f := m / 3
		if last >= 0 && (f == lf || f == lf-3) {
			continue
		}
		tw2 := int(t.twistMove[tw*18+m])
		fl2 := int(t.flipMove[fl*18+m])
		ss2 := int(t.ssMove[ss*18+m])
		if int(t.h1(tw2, fl2, ss2/24)) >= togo {
			continue
		}
		s.path = append(s.path, m)
		s.phase1(tw2, fl2, ss2, int(t.cornMove[co*18+m]), togo-1)
		s.path = s.path[:len(s.path)-1]
		if s.stop {
			return
		}
	}
}

func (s *Solver) startPhase2(ss, co int) {
	c := Cubie{ep: s.cc.ep}
	for _, m := range s.path {
		c.edgeMul(&moveCubes[m])
	}
	ude := c.udEdges()
	t := s.t
	n1 := len(s.path)
	limit := s.best - 1 - n1
	if limit < 0 {
		return
	}
	h2 := t.csp[co*24+ss]
	if e := t.esp[ude*24+ss]; e > h2 {
		h2 = e
	}
	if int(h2) > limit {
		return
	}
	for d2 := int(h2); d2 <= limit; d2++ {
		s.p2path = s.p2path[:0]
		if s.phase2(co, ude, ss, d2) {
			sol := []string{}
			for _, m := range s.path {
				sol = append(sol, moveNames[m])
			}
			for _, m := range s.p2path {
				sol = append(sol, moveNames[m])
			}
			s.solutions = append(s.solutions, sol)
			s.best = len(sol)
			return
		}
	}
}

func (s *Solver) phase2(co, ude, ss, togo int) bool {
	if togo == 0 {
		return co == 0 && ude == 0 && ss == 0
	}
	t := s.t
	prev := -1
	if len(s.p2path) > 0 {
		prev = s.p2path[len(s.p2path)-1]
	} else if len(s.path) > 0 {
		prev = s.path[len(s.path)-1]
	}
	lf := -9
	if prev >= 0 {
		lf = prev / 3
	}
	for _, m := range phase2Moves {
		f := m / 3
		if f == lf || f == lf-3 {
			continue
		}
		c2 := int(t.cornMove[co*18+m])
		e2 := int(t.udeMove[ude*18+m])
		s2 := int(t.ssMove[ss*18+m])
		a, b := t.csp[c2*24+s2], t.esp[e2*24+s2]
		if a < b {
			a = b
		}
		if int(a) >= togo {
			continue
		}
		s.p2path = append(s.p2path, m)
		if s.phase2(c2, e2, s2, togo-1) {
			return true
		}
		s.p2path = s.p2path[:len(s.p2path)-1]
	}
	return false
}

func applyMovesFacelets(s string, moves []string) string {
	cc, _ := faceletsToCubie(s)
	for _, mv := range moves {
		cc.mul(&moveCubes[moveIndex(mv)])
	}
	return cubieToFacelets(&cc)
}

func randomCubeString(r *rand.Rand) string {
	cc := identity()
	cc.setTwist(r.Intn(nTwist))
	cc.setFlip(r.Intn(nFlip))
	for {
		cc.setCorners(r.Intn(nPerm8))
		p := r.Perm(12)
		copy(cc.ep[:], p)
		if parity(cc.ep[:]) == parity(cc.cp[:]) {
			break
		}
	}
	return cubieToFacelets(&cc)
}
