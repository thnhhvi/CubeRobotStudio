package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"strings"
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
		seq, end := fwSeqOfMoves(toRobotMoves(mv), NORMAL, d)
		home := ""
		if end == INVERT {
			home = fwRotateSeq(NORMAL, d)
		}
		runs = append(runs, run{mv, []string{seq}, home})
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
	b, _ := json.Marshal(map[string]any{"steps": steps, "finish": defaultPlan().Finish})
	os.WriteFile(out, b, 0o644)
}

// Writes the exact SEQ strings the app sends (scan plan incl. second look and finish,
// cube flips, sample solves, open/close) for the firmware replay test
// (tools/firmware_host_test/replay_test.cpp), at the fastest speed.
func TestDumpFirmwareSeqs(t *testing.T) {
	out := os.Getenv("DUMP_FW")
	if out == "" {
		t.Skip("set DUMP_FW=<file>")
	}
	lines := []string{openSeq, closeSeq}
	for _, d := range []int{120, 300, 500} {
		p := defaultPlan()
		for _, st := range p.Steps {
			off, back := st.look2()
			for _, s := range []string{st.Present, off, back, st.Revert} {
				lines = append(lines, fillSeq(s, d))
			}
		}
		lines = append(lines, fillSeq(p.Finish, d), fwRotateSeq(NORMAL, d), fwRotateSeq(INVERT, d))
		r := rand.New(rand.NewSource(int64(d)))
		for _, m := range moveCommands(toRobotMoves(randomScramble(21, r)), NORMAL, d) {
			lines = append(lines, m.Cmd)
		}
	}
	os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
