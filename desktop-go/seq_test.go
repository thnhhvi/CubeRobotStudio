package main

import (
	"regexp"
	"strconv"
	"testing"
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
	in := "RLLLFRBR210fRbL210FLBL210RRLR210fCfCbCbC210RLLL210lR210LR210lClC210LL"
	want := "RCLCFRBR210fRbL210FCBC210RRLR210fCfCbCbC210RCLC210lR210LR210lClC210LC"
	if got := softenHolds(in); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if got := softenHolds("FLBL500RLLL"); got != "FCBC500RCLC" {
		t.Fatalf("close: %s", got)
	}
}
