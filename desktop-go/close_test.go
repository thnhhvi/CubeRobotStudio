package main

import "testing"

func TestCloseSequence(t *testing.T) {
	cases := []struct {
		name  string
		state map[byte]byte
	}{
		{"all open (R)", map[byte]byte{'F': 'R', 'B': 'R', 'R': 'R', 'L': 'R'}},
		{"unknown", map[byte]byte{}},
		{"all at C", map[byte]byte{'F': 'C', 'B': 'C', 'R': 'C', 'L': 'C'}},
		{"F/B clamped, R/L open", map[byte]byte{'F': 'L', 'B': 'L', 'R': 'R', 'L': 'R'}},
		{"already clamped", map[byte]byte{'F': 'L', 'B': 'L', 'R': 'L', 'L': 'L'}},
	}
	for _, c := range cases {
		s := closeSequence(c.state, 210, 80, 500)
		t.Logf("%-22s SEQ %s", c.name, s)
		// final states must all be L and nothing may move back towards open
		fin := map[byte]byte{}
		for k, v := range c.state {
			fin[k] = v
		}
		for _, p := range parseSeq(s) {
			old := fin[p[0]]
			if old != 0 && indexOf(p[1]) < indexOf(old) {
				t.Fatalf("%s: %c moves back from %c to %c", c.name, p[0], old, p[1])
			}
			fin[p[0]] = p[1]
		}
		for _, k := range []byte("FBRL") {
			if fin[k] != 'L' {
				t.Fatalf("%s: %c ends at %c", c.name, k, fin[k])
			}
		}
		if len(s) > 0 && s[len(s)-1] >= '0' && s[len(s)-1] <= '9' {
			t.Fatalf("%s: ends with a delay", c.name)
		}
	}
}

func indexOf(b byte) int {
	for i := 0; i < len(sliderPath); i++ {
		if sliderPath[i] == b {
			return i
		}
	}
	return -1
}
