package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func scanPhase(c *Controller) (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scan == nil {
		return "", -1
	}
	return c.scan.Phase, c.scan.Index
}

func idle(c *Controller) bool { c.mu.Lock(); defer c.mu.Unlock(); return c.busyTask == "" }

// Full app flow against the built-in simulator (run with -race).
func TestAppFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	c := NewController()
	bus := c.bus.Subscribe()
	go func() {
		for range bus {
		}
	}()
	waitFor(t, "solver", func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.solverReady })
	if err := c.UpdateSettings(map[string]any{"delay": 120.0}); err != nil {
		t.Fatal(err)
	}
	if err := c.Connect("SIMULATOR"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "slider query", func() bool { return len(c.rq.SliderStates()) == 4 })

	for _, f := range []func() error{c.CloseArms, c.OpenArms, c.CloseArms} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "arms", func() bool { return idle(c) })
	}
	for _, s := range []byte("FBRL") {
		if c.rq.SliderStates()[s] != 'L' {
			t.Fatalf("after Close %c = %c", s, c.rq.SliderStates()[s])
		}
	}

	// ---- scan: ready -> begin -> capture -> (retake once) -> confirm x6
	r := rand.New(rand.NewSource(3))
	truth := applyMoves(solvedColors('R', 'W'), randomScramble(25, r))
	// the app must not rely on the front/top setting: set a wrong one
	if err := c.UpdateSettings(map[string]any{"front": "G", "top": "Y"}); err != nil {
		t.Fatal(err)
	}
	if err := c.StartScan(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ready", func() bool { p, _ := scanPhase(c); return p == "ready" })
	if err := c.Capture(map[string]any{"tiles": make([]any, 9)}); err == nil {
		t.Fatal("capture accepted before Start scanning")
	}
	if err := c.BeginScan(); err != nil {
		t.Fatal(err)
	}
	steps := walkPlan(c.plan, 'R', 'W')
	for i, w := range steps {
		waitFor(t, "capture", func() bool { p, k := scanPhase(c); return p == "capture" && k == i })
		if i == 1 {
			time.Sleep(2500 * time.Millisecond) // keep-alive must fire while waiting
		}
		tiles := func(bad bool) map[string]any {
			ts := []any{}
			logo := false
			for k := 0; k < 9; k++ {
				rgb := defaultRGB[truth[w.Cells[k]]]
				if k == 4 && truth[w.Cells[k]] == 'W' {
					rgb = [3]int{70, 90, 190} // GAN logo: the centre reads blue-ish
					logo = true
				}
				if bad {
					rgb = [3]int{128, 128, 128}
				}
				ts = append(ts, []any{float64(rgb[0]), float64(rgb[1]), float64(rgb[2])})
			}
			return map[string]any{"tiles": ts, "logo": map[string]any{"detected": logo}}
		}
		if i == 1 { // after the first face only the position is known
			c.mu.Lock()
			lbl, known := c.scan.Labels[1], c.scan.Known
			c.mu.Unlock()
			if known || lbl != "bottom face" {
				t.Fatalf("before 2 faces: known=%v label=%q", known, lbl)
			}
		}
		if i == 2 {
			c.mu.Lock()
			known, lbl := c.scan.Known, c.scan.Labels[2]
			c.mu.Unlock()
			if !known || lbl != "blue" {
				t.Fatalf("after 2 faces: known=%v label=%q (want blue)", known, lbl)
			}
			c.Capture(tiles(true))
			waitFor(t, "confirm", func() bool { p, _ := scanPhase(c); return p == "confirm" })
			c.ConfirmScan("retake")
			waitFor(t, "recapture", func() bool { p, k := scanPhase(c); return p == "capture" && k == i })
		}
		if err := c.Capture(tiles(false)); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "confirm", func() bool { p, k := scanPhase(c); return p == "confirm" && k == i })
		if err := c.ConfirmScan("confirm"); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "scan end", func() bool { return idle(c) })
	c.mu.Lock()
	got, sol := c.colors, append([]string{}, c.solution...)
	c.mu.Unlock()
	if got != truth {
		t.Fatalf("scan wrong\n got %s\nwant %s", got, truth)
	}
	if st := c.cfg(); st.Front != "R" || st.Top != "W" {
		t.Fatalf("detected orientation %s front %s top, want R/W", st.Front, st.Top)
	}
	if len(sol) == 0 {
		t.Fatal("no solution after scan")
	}

	// ---- solve on the (simulated) robot
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	go func() { // user moves the speed slider during the solve
		for i := 0; i < 20; i++ {
			c.UpdateSettings(map[string]any{"delay": float64(120 + 10*(i%3))})
			time.Sleep(50 * time.Millisecond)
		}
	}()
	waitFor(t, "solve", func() bool { return idle(c) })
	c.mu.Lock()
	solved := c.colors == solvedColors('R', 'W')
	c.mu.Unlock()
	if !solved || c.rq.GetOrientation() != NORMAL {
		t.Fatalf("not solved / not home: %v %d", solved, c.rq.GetOrientation())
	}

	// ---- scramble, abort halfway, close again
	if err := c.Scramble(20); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	c.Abort()
	waitFor(t, "abort", func() bool { return idle(c) })
	c.mu.Lock()
	known := c.stateKnown
	c.mu.Unlock()
	if known {
		t.Fatal("state should be marked unknown after abort")
	}
	if err := c.CloseArms(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "close after abort", func() bool { return idle(c) })
	if s := c.sliderText(); !strings.Contains(s, "F=L") {
		t.Fatalf("sliders after close: %s", s)
	}
}
