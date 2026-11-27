package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSeqBatches(t *testing.T) {
	rm := toRobotMoves([]string{"U", "F'", "R2", "D", "B", "L'", "U2", "F", "D'", "R"})
	bs := buildSeqBatches(rm, NORMAL, 210)
	all := ""
	for _, b := range bs {
		if len("SEQ "+b.Seq) > 254 {
			t.Fatalf("too long: %d", len("SEQ "+b.Seq))
		}
		all += b.Moves
		// timing of the SEQ must match the animation timeline
		_, _, total := buildTimeline(b.Moves, b.StartOrient, 210)
		sum := 100
		for _, m := range regexp.MustCompile(`\d+`).FindAllString(b.Seq, -1) {
			n, _ := strconv.Atoi(m)
			sum += n
		}
		if sum != total {
			t.Fatalf("timing mismatch seq=%d timeline=%d", sum, total)
		}
	}
	if all != rm {
		t.Fatalf("moves lost: %s vs %s", all, rm)
	}
	t.Logf("%d batches; first: SEQ %s", len(bs), bs[0].Seq)
	t.Logf("face U at INVERT: %s", faceSeq('U', INVERT, 210))
	t.Logf("face f: %s", faceSeq('f', NORMAL, 210))
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
	go func() { time.Sleep(50 * time.Millisecond); q.OnLine("OK GLIDE 1") }()
	if !q.EnableGlide() {
		t.Fatal("glide firmware not recognised")
	}
}

func TestHardGrip(t *testing.T) {
	in := rotateCubeSeq(INVERT, 210)
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
