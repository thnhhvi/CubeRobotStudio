package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"testing"
)

// Writes the exact SEQ commands the app sends for random move lists (used by
// tools/check_robot_commands.py, a physical simulation of the robot).
func TestDumpSolveSeqs(t *testing.T) {
	out := os.Getenv("DUMP_SEQ")
	if out == "" {
		t.Skip("set DUMP_SEQ=<file>")
	}
	type run struct {
		Moves   []string `json:"moves"`
		Batches []string `json:"batches"`
		Home    string   `json:"home"`
	}
	r := rand.New(rand.NewSource(1))
	runs := []run{}
	for i := 0; i < 300; i++ {
		n := 1 + r.Intn(30)
		mv := randomScramble(n, r)
		d := 120 + 10*r.Intn(39)
		o := NORMAL
		bs := []string{}
		for _, b := range buildSeqBatches(toRobotMoves(mv), o, d) {
			bs = append(bs, b.Seq)
			o = b.EndOrient
		}
		home := ""
		if o == INVERT {
			home = rotateCubeSeq(NORMAL, d)
		}
		runs = append(runs, run{mv, bs, home})
	}
	b, _ := json.Marshal(runs)
	os.WriteFile(out, b, 0o644)
}

// Writes the default scan plan for tools/check_robot_commands.py scan.
func TestDumpScanPlan(t *testing.T) {
	out := os.Getenv("DUMP_PLAN")
	if out == "" {
		t.Skip("set DUMP_PLAN=<file>")
	}
	type s struct {
		ID      string `json:"id"`
		Present string `json:"present"`
		Revert  string `json:"revert"`
		Off     string `json:"handoff"`
		Back    string `json:"handback"`
	}
	steps := []s{}
	for _, st := range defaultPlan().Steps {
		off, back := st.look2()
		steps = append(steps, s{st.ID, st.Present, st.Revert, off, back})
	}
	b, _ := json.Marshal(steps)
	os.WriteFile(out, b, 0o644)
}
