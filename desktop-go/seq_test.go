package main

import (
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The app's timeline for MOVE must match the firmware's own servo strings exactly.
func TestFirmwareMoveTiming(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for n := 0; n < 100; n++ {
		rm := toRobotMoves(randomScramble(21, r))
		d := 120 + 10*r.Intn(30)
		for _, o := range []int{NORMAL, INVERT} {
			_, end, total := fwTimeline(rm, o, d)
			seq, end2 := fwSeqOfMoves(rm, o, d)
			sum := 100
			for _, m := range regexp.MustCompile(`\d+`).FindAllString(seq, -1) {
				v, _ := strconv.Atoi(m)
				sum += v
			}
			if sum != total || end != end2 {
				t.Fatalf("timeline %d/%d vs firmware %d/%d", total, end, sum, end2)
			}
		}
	}
	cmds := moveCommands(strings.Repeat("U", 130), NORMAL, 210)
	if len(cmds) != 3 || !strings.HasPrefix(cmds[0].Cmd, "MOVE 210 0 ") || !strings.HasPrefix(cmds[1].Cmd, "MOVE 210 1 ") {
		t.Fatalf("MOVE split: %v", cmds)
	}
}

func TestOriginalSolver(t *testing.T) {
	tb := NewTables()
	r := rand.New(rand.NewSource(11))
	for n := 0; n < 10; n++ {
		colors := applyMoves(solvedColors('R', 'W'), randomScramble(25, r))
		fac, _ := colorsToFacelets(colors)
		sols, err := SolveMany(tb, fac, 21, time.Second, 0, 1)
		if err != nil || len(sols) != 1 || len(sols[0]) > 21 {
			t.Fatalf("solver: %v %v", err, sols)
		}
		if applyMoves(colors, sols[0]) != solvedColors('R', 'W') {
			t.Fatal("does not solve")
		}
	}
}

func TestSoftenHolds(t *testing.T) {
	in := "RLLL250FRBR250fRbL210FLBL250RRLR250fCfCbCbC210RLLL250lR210LR250lClC210LL"
	want := "RCLC250FRBR250fRbL210FCBC250RRLR250fCfCbCbC210RCLC250lR210LR250lClC210LC"
	if got := softenHolds(in); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := softenHolds(closeSeq); got != "FCBC500RCLC" {
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

func TestFirmwareProbe(t *testing.T) {
	l := &Link{onLine: func(string) {}, onStatus: func(bool, string) {}}
	q := NewRobotQueue(l, func(map[string]any) {})
	l.sim = &Simulator{out: func(s string) {}} // swallows INFO: answer like the stock firmware
	go func() { time.Sleep(50 * time.Millisecond); q.OnLine("ERR cmd: INFO") }()
	if q.ProbeFirmware() || q.CRSFirmware() {
		t.Fatal("stock firmware reported as Cube Robot Studio firmware")
	}
	go func() { time.Sleep(50 * time.Millisecond); q.OnLine("OK CRS 2.8") }()
	if !q.ProbeFirmware() || q.FirmwareVersion() != "2.8" {
		t.Fatalf("firmware not recognised (%q)", q.FirmwareVersion())
	}
}

func TestHardGrip(t *testing.T) {
	in := fillSeq(defaultPlan().Steps[1].Present, 210)
	if !strings.Contains(in, "FHBH") {
		t.Fatalf("scan roll does not clamp hard: %s", in)
	}
	if got := noHardGrip(in); strings.Contains(got, "H") || len(got) != len(in) {
		t.Fatalf("stock firmware conversion: %s", got)
	}
	if got := softenHolds("FHBH400RLLL"); got != "FCBC400RCLC" {
		t.Fatalf("low power: %s", got)
	}
}
