package main

// Choosing a solution for this robot. The two-phase solver is run on the cube seen
// along each of its three axes (its phase-2 axis then lies along that cube axis), and
// on the inverse cube; near-shortest solutions are kept too. Every candidate is planned
// for the robot (planner.go: face turns, rolls, the way back home) and the one with the
// least robot time wins - usually the one that needs the fewest rolls.

import (
	"errors"
	"sync"
	"time"
)

// invertFacelets returns the facelets of the inverse cube state.
func invertFacelets(f string) (string, error) {
	cc, err := faceletsToCubie(f)
	if err != nil {
		return "", err
	}
	var inv Cubie
	for i := 0; i < 8; i++ {
		inv.cp[cc.cp[i]] = i
		inv.co[cc.cp[i]] = (3 - cc.co[i]) % 3
	}
	for i := 0; i < 12; i++ {
		inv.ep[cc.ep[i]] = i
		inv.eo[cc.ep[i]] = cc.eo[i]
	}
	return cubieToFacelets(&inv), nil
}

func invertMoves(m []string) []string {
	out := make([]string, 0, len(m))
	for i := len(m) - 1; i >= 0; i-- {
		mv := m[i]
		switch {
		case len(mv) == 2 && mv[1] == '\'':
			out = append(out, mv[:1])
		case len(mv) == 2:
			out = append(out, mv)
		default:
			out = append(out, mv+"'")
		}
	}
	return out
}

type robotChoice struct {
	Moves    []string
	Steps    []RobotStep
	Ms       int
	Rolls    int
	Tried    int
	Shortest int
}

// solveForRobot returns the solution of colors that is fastest for the robot.
func solveForRobot(tb *Tables, colors string, timeout time.Duration, start int, home bool, d int) (robotChoice, error) {
	type job struct {
		rot string
		inv bool
	}
	jobs := []job{{"", false}, {"x", false}, {"z", false}, {"", true}, {"x", true}, {"z", true}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	all := [][]string{}
	var firstErr error
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			col := colors
			back := map[byte]byte{}
			for i := 0; i < 6; i++ {
				back[FACES[i]] = FACES[i]
			}
			if j.rot != "" {
				col = applyMove(colors, j.rot)
				lab := []byte{}
				for i := 0; i < 6; i++ {
					for k := 0; k < 9; k++ {
						lab = append(lab, FACES[i])
					}
				}
				rl := applyMove(string(lab), j.rot)
				for i := 0; i < 6; i++ {
					back[FACES[i]] = rl[9*i+4] // face at rotated position i was this face
				}
			}
			fac, err := colorsToFacelets(col)
			if err == nil && j.inv {
				fac, err = invertFacelets(fac)
			}
			var sols [][]string
			if err == nil {
				sols, err = SolveMany(tb, fac, 24, timeout, 3, 400)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, s := range sols {
				if j.inv {
					s = invertMoves(s)
				}
				m := make([]string, len(s))
				for k, mv := range s {
					m[k] = string(back[mv[0]]) + mv[1:]
				}
				all = append(all, m)
			}
		}(j)
	}
	wg.Wait()
	if len(all) == 0 {
		if firstErr == nil {
			firstErr = errors.New("no solution found")
		}
		return robotChoice{}, firstErr
	}
	best := robotChoice{Ms: -1, Tried: len(all), Shortest: 99}
	for _, m := range all {
		if len(m) < best.Shortest {
			best.Shortest = len(m)
		}
		steps, _, ms := planRobot(toRobotMoves(m), start, home, d)
		if best.Ms < 0 || ms < best.Ms || (ms == best.Ms && len(m) < len(best.Moves)) {
			best.Moves, best.Steps, best.Ms = m, steps, ms
		}
	}
	best.Rolls = countRolls(best.Steps)
	return best, nil
}
