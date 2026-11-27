package main

import (
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSeqBatches(t *testing.T) {
	rm := toRobotMoves([]string{"U", "F'", "R2", "D", "B", "L'", "U2", "F", "D'", "R"})
	bs := buildSeqBatches(rm, NORMAL, 210, true)
	all := ""
	end := -1
	for _, b := range bs {
		if len("SEQ "+b.Seq) > 254 {
			t.Fatalf("too long: %d", len("SEQ "+b.Seq))
		}
		all += b.Moves
		// timing of the SEQ must match the animation timeline
		_, total := timeline(b.Steps, 210)
		sum := 100
		for _, m := range regexp.MustCompile(`\d+`).FindAllString(b.Seq, -1) {
			n, _ := strconv.Atoi(m)
			sum += n
		}
		if sum != total {
			t.Fatalf("timing mismatch seq=%d timeline=%d", sum, total)
		}
		end = b.EndOrient
	}
	if all != rm {
		t.Fatalf("moves lost: %s vs %s", all, rm)
	}
	if end != homeOrient {
		t.Fatalf("not back home: %d", end)
	}
	t.Logf("%d batches; first: SEQ %s", len(bs), bs[0].Seq)
}

// The planner uses all 24 orientations and both roll types: a move list that the old
// two-pose scheme had to flip for every U/D <-> L/R change needs far fewer rolls.
func TestPlannerRolls(t *testing.T) {
	if len(orients) != 24 {
		t.Fatalf("%d orientations", len(orients))
	}
	rm := toRobotMoves([]string{"U", "R", "U'", "R'", "U", "R", "U'", "R'"})
	steps, end, _ := planRobot(rm, homeOrient, true, 300)
	if n := countRolls(steps); n > 2 || end != homeOrient {
		t.Fatalf("U R U' R' x2 needs %d rolls (end %d)", n, end)
	}
	// F/B/L/R only: no roll at all
	steps, _, _ = planRobot(toRobotMoves([]string{"F", "R", "B'", "L2"}), homeOrient, true, 300)
	if countRolls(steps) != 0 {
		t.Fatalf("rolls for arm faces only")
	}
	// every turn is on a face that points at an arm
	r := rand.New(rand.NewSource(5))
	for n := 0; n < 200; n++ {
		mv := randomScramble(25, r)
		steps, end, _ := planRobot(toRobotMoves(mv), homeOrient, true, 300)
		if end != homeOrient {
			t.Fatal("not home")
		}
		for _, s := range steps {
			if s.Roll == "" && (s.Arm == 0 || armFor(s.Orient, s.Move) != s.Arm) {
				t.Fatalf("bad arm for %c", s.Move)
			}
		}
	}
}

func TestSolveForRobot(t *testing.T) {
	tb := NewTables()
	r := rand.New(rand.NewSource(11))
	totalRolls, totalMoves := 0, 0
	for n := 0; n < 5; n++ {
		colors := applyMoves(solvedColors('R', 'W'), randomScramble(25, r))
		ch, err := solveForRobot(tb, colors, 700*time.Millisecond, homeOrient, true, 300)
		if err != nil {
			t.Fatal(err)
		}
		if applyMoves(colors, ch.Moves) != solvedColors('R', 'W') {
			t.Fatalf("does not solve: %v", ch.Moves)
		}
		totalRolls += ch.Rolls
		totalMoves += len(ch.Moves)
		t.Logf("%d moves (shortest %d), %d rolls, %.1f s, from %d candidates", len(ch.Moves), ch.Shortest, ch.Rolls, float64(ch.Ms)/1000, ch.Tried)
	}
	t.Logf("average %.1f moves, %.1f rolls", float64(totalMoves)/5, float64(totalRolls)/5)
}

func TestSoftenHolds(t *testing.T) {
	in := "RLLL250FRBR250fRbL210FLBL250RRLR250fCfCbCbC210RLLL250lR210LR250lClC210LL"
	want := "RCLC250FRBR250fRbL210FCBC250RRLR250fCfCbCbC210RCLC250lR210LR250lClC210LC"
	if got := softenHolds(in); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := softenHolds("FLBL500RLLL"); got != "FCBC500RCLC" {
		t.Fatalf("close: %s", got)
	}
}

func TestCalibrationFirmwareBlocked(t *testing.T) {
	sent := []string{}
	l := &Link{onLine: func(string) {}, onStatus: func(bool, string) {}}
	q := NewRobotQueue(l, func(map[string]any) {})
	l.sim = &Simulator{out: func(s string) {}} // pretend connected
	q.OnLine("Selected Servo: 0 (RIGHT_SPINNER) | Mode: CENTER | Center us: 1520 | Deviation: 0")
	if !q.CalibrationFirmware() {
		t.Fatal("calibration firmware not recognised")
	}
	if err := q.Run("SEQ RRLRFRBR"); err == nil || !strings.Contains(err.Error(), "CALIBRATE") {
		t.Fatalf("command not blocked: %v (sent %v)", err, sent)
	}
	q.OnLine("Listening for API commands, type 'HELP' for list of commands.")
	if q.CalibrationFirmware() {
		t.Fatal("normal firmware banner should clear the flag")
	}
}

func TestGlideStockFirmware(t *testing.T) {
	l := &Link{onLine: func(string) {}, onStatus: func(bool, string) {}}
	q := NewRobotQueue(l, func(map[string]any) {})
	l.sim = &Simulator{out: func(s string) {}} // swallows GLIDE: answer like the stock firmware
	go func() { time.Sleep(50 * time.Millisecond); q.OnLine("ERR cmd: GLIDE") }()
	if q.EnableGlide() || q.Glide() {
		t.Fatal("stock firmware reported as glide-capable")
	}
	go func() { time.Sleep(50 * time.Millisecond); q.OnLine("OK GLIDE 1 v2.5") }()
	if !q.EnableGlide() || q.FirmwareVersion() != "2.5" {
		t.Fatalf("glide firmware not recognised (%q)", q.FirmwareVersion())
	}
}

func TestHardGrip(t *testing.T) {
	in := rollSeq("z'", 210)
	if !strings.Contains(in, "FHBH") {
		t.Fatalf("flip does not clamp hard: %s", in)
	}
	if got := noHardGrip(in); strings.Contains(got, "H") || len(got) != len(in) {
		t.Fatalf("stock firmware conversion: %s", got)
	}
	if got := softenHolds("FHBH400RLLL"); got != "FCBC400RCLC" {
		t.Fatalf("low power: %s", got)
	}
}
