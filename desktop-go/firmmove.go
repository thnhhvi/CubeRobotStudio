package main

// Solving the way the original Yonni123 CubeSolver app does it: the Kociemba two-phase
// solver gives the first solution of at most 21 moves (1 s), and the robot performs it
// with the firmware's own MOVE command ("MOVE <delay> <orientation> <moves>"). The
// firmware flips the cube itself when needed (U/D on the left/right arms, "INVERT";
// L/R back on them, "NORMAL") and holds the cube with the sliders at C.
//
// The functions here mirror the firmware's MOVE code exactly, so the app knows how long
// each move takes (3D animation) and where the cube ends up.

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const INVERT = 1

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

// poseName is the cube orientation for the 3D view ("" = home, INVERT = rolled z').
func poseName(o int) string {
	if o == INVERT {
		return "z'"
	}
	return ""
}

// orientationAfter: the firmware rolls to INVERT for U/D and back to NORMAL for L/R.
func orientationAfter(ch byte, cur int) int {
	switch unicode.ToUpper(rune(ch)) {
	case 'U', 'D':
		return INVERT
	case 'L', 'R':
		return NORMAL
	}
	return cur
}

// fwRotateSeq is the firmware's rotateCube (MOVE) as a SEQ string.
func fwRotateSeq(o, d int) string {
	pre := "fLbR"
	if o == INVERT {
		pre = "fRbL"
	}
	return fmt.Sprintf("RLLLFRBR%d%s%dFLBL%dRRLR%dfCfCbCbC%dRCLC%dFCBC", d, pre, d, d, d, d, d)
}

// fwMoveSeq is the firmware's servo string for one MOVE character (orient = before).
func fwMoveSeq(ch byte, orient, d int) (string, int) {
	no := orientationAfter(ch, orient)
	out := ""
	if no != orient {
		out = fwRotateSeq(no, d)
	}
	var arm byte
	switch unicode.ToUpper(rune(ch)) {
	case 'F':
		arm = 'f'
	case 'B':
		arm = 'b'
	case 'U', 'L':
		arm = 'l'
	case 'D', 'R':
		arm = 'r'
	}
	dir := "R"
	if unicode.IsLower(rune(ch)) {
		dir = "L"
	}
	sl := byte(unicode.ToUpper(rune(arm)))
	out += fmt.Sprintf("%c%s%d%cR%d%c%c%c%c%d%cC%d", arm, dir, d, sl, d, arm, 'C', arm, 'C', d, sl, d)
	return out, no
}

// fwTimeline: when each move / flip of "MOVE d orient rm" happens (ms after BUSY; the
// firmware starts 100 ms after the command) and the orientation at the end.
func fwTimeline(rm string, orient, d int) ([]*TLEvent, int, int) {
	t := 100
	ev := []*TLEvent{}
	for i := 0; i < len(rm); i++ {
		ch := rm[i]
		no := orientationAfter(ch, orient)
		if no != orient {
			ev = append(ev, &TLEvent{At: t + 4*d, Dur: d, Kind: "pose", Pose: poseName(no)})
			t += 6 * d
			orient = no
		}
		mv := string(unicode.ToUpper(rune(ch)))
		if unicode.IsLower(rune(ch)) {
			mv += "'"
		}
		ev = append(ev, &TLEvent{At: t, Dur: d, Kind: "move", Move: mv})
		t += 4 * d
	}
	return ev, orient, t
}

// moveCommands splits a robot move string into MOVE commands that fit the firmware.
func moveCommands(rm string, orient, d int) []struct {
	Cmd         string
	Moves       string
	StartOrient int
	EndOrient   int
} {
	out := []struct {
		Cmd         string
		Moves       string
		StartOrient int
		EndOrient   int
	}{}
	for _, part := range chunkMoves(rm) {
		_, end, _ := fwTimeline(part, orient, d)
		out = append(out, struct {
			Cmd         string
			Moves       string
			StartOrient int
			EndOrient   int
		}{fmt.Sprintf("MOVE %d %d %s", d, orient, part), part, orient, end})
		orient = end
	}
	return out
}

// fwSeqOfMoves expands MOVE into the exact servo string the firmware runs (for checks).
func fwSeqOfMoves(rm string, orient, d int) (string, int) {
	var b strings.Builder
	for i := 0; i < len(rm); i++ {
		s, no := fwMoveSeq(rm[i], orient, d)
		b.WriteString(s)
		orient = no
	}
	return b.String(), orient
}
