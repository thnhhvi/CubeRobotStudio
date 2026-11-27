package main

import (
	"math/rand"
	"testing"
)

func TestThumbOcclusion(t *testing.T) {
	cols := [9][3]int{}
	for k := range cols {
		cols[k] = defaultRGB[COLORS[k%6]]
	}
	vis, ok := thumbTiles(fakeThumb(cols, 3))
	if !ok {
		t.Fatal("thumb not decoded")
	}
	for k, v := range vis {
		if (k == 3) != (v.Visible < minVisible) {
			t.Fatalf("tile %d visible %.2f", k, v.Visible)
		}
	}
	// dark but coloured stickers (shadowed blue / red) are not grippers
	if isGripperPixel(20, 50, 120) || isGripperPixel(110, 15, 20) || !isGripperPixel(30, 30, 34) {
		t.Fatal("gripper pixel test wrong")
	}
}

func TestCompleteHidden(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, hide := range []int{4, 8, 12} {
		ok, amb := 0, 0
		for n := 0; n < 200; n++ {
			truth := applyMoves(solvedColors('R', 'W'), randomScramble(25, r))
			b := []byte(truth)
			for h := 0; h < hide; {
				i := r.Intn(54)
				if i%9 != 4 && b[i] != '?' {
					b[i] = '?'
					h++
				}
			}
			got, st := completeHidden(string(b))
			switch st {
			case "ok":
				if got != truth {
					t.Fatalf("wrong fill\n got %s\nwant %s", got, truth)
				}
				ok++
			case "ambiguous":
				amb++
			default:
				t.Fatalf("valid cube with %d hidden reported impossible", hide)
			}
		}
		t.Logf("%d hidden stickers: %d/200 filled uniquely, %d ambiguous", hide, ok, amb)
		if hide == 4 && ok < 190 {
			t.Fatalf("4 hidden: only %d/200 unique", ok)
		}
	}
	// a piece that cannot exist is reported
	b := []byte(solvedColors('R', 'W'))
	b[1] = '?'
	b[2] = b[9+2] // corner with two stickers of the same colour
	if _, st := completeHidden(string(b)); st != "impossible" {
		t.Fatalf("bad cube: %s", st)
	}
}
