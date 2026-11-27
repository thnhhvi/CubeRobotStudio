package main

// Robot move planner.
//
// The robot can turn any face that points at one of its four arms (front, back, right,
// left); the two faces pointing up and down cannot be turned. A roll brings other faces
// to the arms:
//   z / z'  front+back arms carry the cube, rotating about the front-back axis
//   x / x'  right+left arms carry the cube, rotating about the right-left axis
// A roll takes several times as long as a face turn, so the planner tracks all 24
// orientations of the cube and picks, for a whole move list, the rolls that cost the
// least time (dynamic programming over the orientations, including the way back home).
// The solver then picks, among many solutions, the one with the least robot time.

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// orientation: which cube face (in home-frame letters) points to each robot position
type orientState struct {
	at   [6]byte // robot position URFDLB -> cube face letter
	pos  map[byte]byte
	word []string // shortest roll word from home (for the 3D view)
}

var (
	orients   []orientState
	orientKey = map[[6]byte]int{}
	rollGens  = []string{"z", "z'", "x", "x'"}
	rollTo    [24][4]int // orientation after roll g
	rollDist  [24][24]int
	rollFirst [24][24]int // first roll (index into rollGens) on a shortest path
)

const homeOrient = 0

func rotateOrient(at [6]byte, g string) [6]byte {
	st := []byte(strings.Repeat(".", 54))
	for i := 0; i < 6; i++ {
		st[9*i+4] = at[i]
	}
	n := applyMove(string(st), g)
	var out [6]byte
	for i := 0; i < 6; i++ {
		out[i] = n[9*i+4]
	}
	return out
}

func init() {
	var home [6]byte
	copy(home[:], FACES)
	add := func(at [6]byte, word []string) int {
		if i, ok := orientKey[at]; ok {
			return i
		}
		pos := map[byte]byte{}
		for i := 0; i < 6; i++ {
			pos[at[i]] = FACES[i]
		}
		orients = append(orients, orientState{at: at, pos: pos, word: word})
		orientKey[at] = len(orients) - 1
		return len(orients) - 1
	}
	add(home, nil)
	for q := 0; q < len(orients); q++ { // BFS: shortest roll words
		for gi, g := range rollGens {
			nat := rotateOrient(orients[q].at, g)
			if _, ok := orientKey[nat]; !ok {
				add(nat, append(append([]string{}, orients[q].word...), g))
			}
			_ = gi
		}
	}
	for o := range orients {
		for gi, g := range rollGens {
			rollTo[o][gi] = orientKey[rotateOrient(orients[o].at, g)]
		}
	}
	for a := range orients { // all-pairs shortest roll paths
		for b := range orients {
			rollDist[a][b] = -1
		}
		rollDist[a][a] = 0
		queue := []int{a}
		for len(queue) > 0 {
			o := queue[0]
			queue = queue[1:]
			for gi := range rollGens {
				n := rollTo[o][gi]
				if rollDist[a][n] < 0 {
					rollDist[a][n] = rollDist[a][o] + 1
					if o == a {
						rollFirst[a][n] = gi
					} else {
						rollFirst[a][n] = rollFirst[a][o]
					}
					queue = append(queue, n)
				}
			}
		}
	}
}

// poseName is the roll word of an orientation for the 3D view ("" = home).
func poseName(o int) string { return strings.Join(orients[o].word, " ") }

// RobotStep is one roll or one quarter face turn.
type RobotStep struct {
	Roll   string // "z", "z'", "x", "x'" or "" for a face turn
	Move   byte   // face turn: cube face (home frame), upper = clockwise
	Arm    byte   // face turn: f b r l
	Orient int    // orientation after this step
}

func turnMs(d int) int { return 2*spinWait(d) + 2*slider1Wait(d) }
func rollMs(d int) int { return 5*sliderWait(d) + 2*pairWait(d) }

// armFor returns the arm that holds cube face f in orientation o (0 if it points up/down).
func armFor(o int, f byte) byte {
	p := orients[o].pos[byte(unicode.ToUpper(rune(f)))]
	switch p {
	case 'F', 'B', 'R', 'L':
		return byte(unicode.ToLower(rune(p)))
	}
	return 0
}

// planRobot finds the fastest way to perform rm (quarter turns, upper = clockwise)
// starting in orientation start, optionally ending back home.
func planRobot(rm string, start int, home bool, d int) ([]RobotStep, int, int) {
	const inf = 1 << 30
	n := len(rm)
	T, R := turnMs(d), rollMs(d)
	cost := make([][24]int, n+1)
	from := make([][24]int, n+1)
	for o := 0; o < 24; o++ {
		cost[0][o] = rollDist[start][o] * R
		from[0][o] = start
	}
	for k := 0; k < n; k++ {
		for o := 0; o < 24; o++ {
			cost[k+1][o] = inf
			if armFor(o, rm[k]) == 0 {
				continue
			}
			for p := 0; p < 24; p++ {
				if cost[k][p] >= inf {
					continue
				}
				c := cost[k][p] + rollDist[p][o]*R + T
				// ties: stay put (fewer rolls now), then lower index
				if c < cost[k+1][o] || (c == cost[k+1][o] && p == o) {
					cost[k+1][o], from[k+1][o] = c, p
				}
			}
		}
	}
	end, best := start, inf
	if n > 0 {
		for o := 0; o < 24; o++ {
			c := cost[n][o]
			if home && c < inf {
				c += rollDist[o][homeOrient] * R
			}
			if c < best {
				best, end = c, o
			}
		}
	} else {
		best = 0
		if home {
			best = rollDist[start][homeOrient] * R
		}
	}
	// orientation used for each move, back to front
	at := make([]int, n)
	o := end
	for k := n; k >= 1; k-- {
		at[k-1] = o
		o = from[k][o]
	}
	steps := []RobotStep{}
	cur := start
	rollPath := func(to int) {
		for cur != to {
			gi := rollFirst[cur][to]
			cur = rollTo[cur][gi]
			steps = append(steps, RobotStep{Roll: rollGens[gi], Orient: cur})
		}
	}
	for k := 0; k < n; k++ {
		rollPath(at[k])
		steps = append(steps, RobotStep{Move: rm[k], Arm: armFor(cur, rm[k]), Orient: cur})
	}
	if home {
		rollPath(homeOrient)
	}
	return steps, cur, best
}

// rollSeq: the carrying pair releases, pre-turns its free grippers, clamps hard (H),
// the other pair lets go, the grippers turn back to vertical (the roll), the other
// pair clamps again, the carriers ease back to L.
func rollSeq(g string, d int) string {
	s, p := sliderWait(d), pairWait(d)
	switch g {
	case "z", "z'":
		pre := "fLbR" // f turns +90 while clamped: z
		if g == "z'" {
			pre = "fRbL"
		}
		return fmt.Sprintf("FRBR%d%s%dFHBH%dRRLR%dfCfCbCbC%dRLLL%dFLBL%d", s, pre, p, s, s, p, s, s)
	default:
		pre := "rLlR" // r turns +90 while clamped: x
		if g == "x'" {
			pre = "rRlL"
		}
		return fmt.Sprintf("RRLR%d%s%dRHLH%dFRBR%drCrClClC%dFLBL%dRLLL%d", s, pre, p, s, s, p, s, s)
	}
}

// turnSeq turns the face held by arm a (spinner +90 = clockwise seen from the arm =
// the face's own clockwise): turn with all four arms clamped, release this arm only
// (the other pair still holds), gripper back to vertical, re-clamp.
func turnSeq(a byte, cw bool, d int) string {
	dir := "R"
	if !cw {
		dir = "L"
	}
	sl := string(unicode.ToUpper(rune(a)))
	sw, sp := slider1Wait(d), spinWait(d)
	return fmt.Sprintf("%c%s%d%sR%d%c%c%c%c%d%sL%d", a, dir, sp, sl, sw, a, 'C', a, 'C', sp, sl, sw)
}

func stepSeq(st RobotStep, d int) string {
	if st.Roll != "" {
		return rollSeq(st.Roll, d)
	}
	return turnSeq(st.Arm, unicode.IsUpper(rune(st.Move)), d)
}

// SeqBatch is one SEQ command (fits the firmware buffer) and the steps it performs.
type SeqBatch struct {
	Seq         string
	Steps       []RobotStep
	Moves       string // face turns in it (quarter turns)
	StartOrient int
	EndOrient   int
}

func batchSteps(steps []RobotStep, start, d int) []SeqBatch {
	out := []SeqBatch{}
	cur := SeqBatch{StartOrient: start, EndOrient: start}
	for _, st := range steps {
		part := stepSeq(st, d)
		if len(cur.Seq)+len(part) > seqBuf && cur.Seq != "" {
			out = append(out, cur)
			cur = SeqBatch{StartOrient: cur.EndOrient, EndOrient: cur.EndOrient}
		}
		cur.Seq += part
		cur.Steps = append(cur.Steps, st)
		if st.Roll == "" {
			cur.Moves += string(st.Move)
		}
		cur.EndOrient = st.Orient
	}
	if cur.Seq != "" {
		out = append(out, cur)
	}
	return out
}

// buildSeqBatches plans rm from orientation start and splits it into SEQ commands.
func buildSeqBatches(rm string, start, d int, home bool) []SeqBatch {
	steps, _, _ := planRobot(rm, start, home, d)
	return batchSteps(steps, start, d)
}

type TLEvent struct {
	At    int    `json:"at"`
	Dur   int    `json:"dur"`
	Kind  string `json:"kind"`
	Move  string `json:"move,omitempty"`
	Pose  string `json:"pose"`
	Index int    `json:"index"`
	fired bool
	timer *time.Timer
}

// timeline: when each face turn / roll happens within one SEQ (ms from its start;
// the firmware starts 100 ms after BUSY), and the total time.
func timeline(steps []RobotStep, d int) ([]*TLEvent, int) {
	t := 100
	ev := []*TLEvent{}
	for _, st := range steps {
		if st.Roll != "" {
			s, p := sliderWait(d), pairWait(d)
			ev = append(ev, &TLEvent{At: t + 3*s + p, Dur: p, Kind: "pose", Pose: poseName(st.Orient)})
			t += rollMs(d)
			continue
		}
		mv := string(unicode.ToUpper(rune(st.Move)))
		if unicode.IsLower(rune(st.Move)) {
			mv += "'"
		}
		ev = append(ev, &TLEvent{At: t, Dur: spinWait(d), Kind: "move", Move: mv})
		t += turnMs(d)
	}
	return ev, t
}

// countRolls counts the rolls in a plan.
func countRolls(steps []RobotStep) int {
	n := 0
	for _, s := range steps {
		if s.Roll != "" {
			n++
		}
	}
	return n
}
