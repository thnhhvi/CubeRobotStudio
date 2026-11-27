package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const speedMin, speedMax = 120, 500

type Settings struct {
	Delay           int     `json:"delay"`
	Front           string  `json:"front"`
	Top             string  `json:"top"`
	AutoCapture     bool    `json:"auto_capture"`
	CaptureSettleMs int     `json:"capture_settle_ms"`
	ScanDelayMin    int     `json:"scan_delay_min"`
	SolveTimeout    float64 `json:"solve_timeout"`
	ReturnHome      bool    `json:"return_home"`
}

// ---------------------------------------------------------------- bus
type Bus struct {
	mu   sync.Mutex
	subs map[chan []byte]bool
}

func (b *Bus) Subscribe() chan []byte {
	c := make(chan []byte, 2000)
	b.mu.Lock()
	b.subs[c] = true
	b.mu.Unlock()
	return c
}
func (b *Bus) Unsubscribe(c chan []byte) { b.mu.Lock(); delete(b.subs, c); b.mu.Unlock() }
func (b *Bus) Emit(ev map[string]any) {
	if _, ok := ev["ts"]; !ok {
		ev["ts"] = float64(time.Now().UnixNano()) / 1e9
	}
	data, _ := json.Marshal(ev)
	b.mu.Lock()
	for c := range b.subs {
		select {
		case c <- data:
		default:
		}
	}
	b.mu.Unlock()
}

// ---------------------------------------------------------------- controller
type ScanState struct {
	Index   int
	Total   int
	Color   byte
	Phase   string
	StepID  string
	Done    bool
	View    []string
	Cells   [9]int
	Samples map[int][3]int
}

type Controller struct {
	mu           sync.Mutex
	bus          *Bus
	cfgDir       string
	settings     Settings
	plan         ScanPlan
	colors       string
	stateKnown   bool
	solution     []string
	solutionInfo map[string]any
	tables       *Tables
	solverReady  bool
	busyTask     string
	pose         string
	scan         *ScanState
	thumbs       map[string]string
	scanned      []string
	phones       map[string]map[string]any
	warnings     []string
	link         *Link
	rq           *RobotQueue
	captureCh    chan map[string]any
	heard        *Event
	timers       []*TLEvent
	fireMu       sync.Mutex
}

func configDir() string {
	h, _ := os.UserHomeDir()
	d := filepath.Join(h, ".cuberobot")
	os.MkdirAll(d, 0o755)
	return d
}

func NewController() *Controller {
	c := &Controller{bus: &Bus{subs: map[chan []byte]bool{}}, cfgDir: configDir(),
		settings: Settings{Delay: 210, Front: "R", Top: "W", AutoCapture: true, CaptureSettleMs: 500,
			ScanDelayMin: 200, SolveTimeout: 3, ReturnHome: true},
		stateKnown: true, pose: "NORMAL", thumbs: map[string]string{}, phones: map[string]map[string]any{},
		solutionInfo: map[string]any{}, captureCh: make(chan map[string]any, 4), heard: NewEvent(false)}
	if b, err := os.ReadFile(filepath.Join(c.cfgDir, "settings.json")); err == nil {
		json.Unmarshal(b, &c.settings)
	}
	if _, err := centerColors(c.settings.Front[0], c.settings.Top[0]); err != nil {
		c.settings.Front, c.settings.Top = "R", "W"
	}
	c.plan = loadPlan(c.cfgDir)
	c.colors = solvedColors(c.settings.Front[0], c.settings.Top[0])
	c.link = &Link{onLine: c.onLine, onStatus: c.onLinkStatus}
	c.rq = NewRobotQueue(c.link, c.bus.Emit)
	go func() {
		t0 := time.Now()
		c.logf("info", "Building solver tables…")
		tb := NewTables()
		c.mu.Lock()
		c.tables = tb
		c.solverReady = true
		c.mu.Unlock()
		c.logf("info", "Solver ready (%.1fs)", time.Since(t0).Seconds())
		c.PushState()
	}()
	return c
}

func (c *Controller) logf(level, f string, a ...any) {
	msg := fmt.Sprintf(f, a...)
	log.Println(msg)
	c.bus.Emit(map[string]any{"type": "log", "level": level, "msg": msg})
}

func (c *Controller) saveSettings() {
	b, _ := json.MarshalIndent(c.settings, "", "  ")
	os.WriteFile(filepath.Join(c.cfgDir, "settings.json"), b, 0o644)
}

func (c *Controller) scanOrder() []string {
	out := []string{}
	for _, w := range walkPlan(c.plan, c.settings.Front[0], c.settings.Top[0]) {
		out = append(out, string(w.Color))
	}
	return out
}

func (c *Controller) Snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	centers, _ := centerColors(c.settings.Front[0], c.settings.Top[0])
	cm := map[string]string{}
	for k, v := range centers {
		cm[string(k)] = string(v)
	}
	var scan any
	if c.scan != nil {
		s := c.scan
		scan = map[string]any{"index": s.Index, "total": s.Total, "color": string(s.Color), "phase": s.Phase,
			"step_id": s.StepID, "done": s.Done, "view": s.View, "cells": s.Cells}
	}
	var busy any
	if c.busyTask != "" {
		busy = c.busyTask
	}
	var sol any
	if c.solution != nil {
		sol = c.solution
	}
	phones := []any{}
	for _, p := range c.phones {
		phones = append(phones, p)
	}
	return map[string]any{
		"type": "state", "colors": c.colors, "state_known": c.stateKnown, "settings": c.settings,
		"speed_range": []int{speedMin, speedMax},
		"robot": map[string]any{"connected": c.link.Connected(), "port": c.link.Port(),
			"state": c.rq.GetState(), "orientation": c.rq.Orientation},
		"pose": c.pose, "solver_ready": c.solverReady, "solution": sol, "solution_info": c.solutionInfo,
		"busy_task": busy, "scan": scan, "scan_order": c.scanOrder(), "centers": cm, "thumbs": c.thumbs,
		"scanned": append([]string{}, c.scanned...), "phones": phones,
		"warnings": append([]string{}, c.warnings...),
	}
}

func (c *Controller) PushState() { c.bus.Emit(c.Snapshot()) }

func (c *Controller) onLine(line string) {
	c.heard.Set()
	c.rq.OnLine(line)
	if line == "IDLE" || line == "BUSY" {
		c.PushState()
	}
}

func (c *Controller) onLinkStatus(up bool, port string) {
	if up {
		c.logf("info", "Robot connected on %s", port)
	} else {
		c.logf("info", "Robot disconnected from %s", port)
	}
	c.PushState()
}

func (c *Controller) Connect(port string) error {
	c.heard.Clear()
	if err := c.link.Connect(port); err != nil {
		return err
	}
	c.rq.mu.Lock()
	c.rq.State = "IDLE"
	c.rq.mu.Unlock()
	c.rq.idle.Set()
	c.PushState()
	if port == "SIMULATOR" {
		c.link.Send("PING")
		return nil
	}
	// The Nano resets when the port opens: wait for its banner, then PING.
	c.logf("info", "Waiting for the Arduino to boot…")
	c.heard.Wait(3 * time.Second)
	for try := 0; try < 3; try++ {
		c.heard.Clear()
		c.link.Send("PING")
		if c.heard.Wait(2 * time.Second) {
			c.logf("info", "Arduino answered on %s", port)
			return nil
		}
	}
	c.logf("error", "No reply from the Arduino on %s. Check: the right COM port; the HC-06/HC-05 "+
		"Bluetooth module unplugged from RX/TX while using USB; firmware flashed with CALIBRATE false; "+
		"Serial Monitor in the Arduino IDE closed.", port)
	return nil
}

func (c *Controller) Disconnect() { c.link.Disconnect(); c.PushState() }

func (c *Controller) requireRobot() error {
	if !c.link.Connected() {
		return errors.New("Robot is not connected")
	}
	return nil
}

func (c *Controller) startTask(name string, fn func() error) error {
	c.mu.Lock()
	if c.busyTask != "" {
		b := c.busyTask
		c.mu.Unlock()
		return errors.New("Busy: " + b)
	}
	c.busyTask = name
	c.mu.Unlock()
	c.rq.ClearAbort()
	c.PushState()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logf("error", "%s crashed: %v", name, r)
			}
			c.mu.Lock()
			c.busyTask = ""
			c.mu.Unlock()
			c.PushState()
		}()
		if err := fn(); err != nil && err.Error() != "aborted" {
			c.logf("error", "%s failed: %v", name, err)
		}
	}()
	return nil
}

func (c *Controller) OpenArms() error {
	if err := c.requireRobot(); err != nil {
		return err
	}
	return c.startTask("open", func() error { return c.rq.Run("SEQ " + openSeq) })
}
func (c *Controller) CloseArms() error {
	if err := c.requireRobot(); err != nil {
		return err
	}
	return c.startTask("close", func() error { return c.rq.Run("SEQ " + closeSeq) })
}

func (c *Controller) Abort() {
	c.fireMu.Lock()
	for _, t := range c.timers {
		if t.timer != nil {
			t.timer.Stop()
		}
	}
	c.timers = nil
	c.fireMu.Unlock()
	c.mu.Lock()
	if c.scan != nil {
		c.scan.Done = true
	}
	if c.busyTask == "solve" || c.busyTask == "scramble" {
		c.stateKnown = false
		c.warnings = []string{"Aborted mid-sequence: cube state on screen may not match the real cube."}
	}
	c.mu.Unlock()
	select {
	case c.captureCh <- nil:
	default:
	}
	if c.link.Connected() {
		c.rq.Abort()
	}
	c.logf("error", "ABORT sent (SEQ C)")
	c.PushState()
}

func (c *Controller) Jog(seq string) error {
	if err := c.requireRobot(); err != nil {
		return err
	}
	d := c.settings.Delay
	if c.settings.ScanDelayMin > d {
		d = c.settings.ScanDelayMin
	}
	s := fillSeq(seq, d)
	return c.startTask("jog", func() error { return c.rq.Run("SEQ " + s) })
}

func (c *Controller) UpdateSettings(data map[string]any) error {
	c.mu.Lock()
	s := c.settings
	changed := false
	for k, v := range data {
		switch k {
		case "delay":
			d := int(toFloat(v))
			if d < speedMin {
				d = speedMin
			}
			if d > speedMax {
				d = speedMax
			}
			s.Delay = d
		case "front", "top":
			str, _ := v.(string)
			if len(str) != 1 {
				c.mu.Unlock()
				return errors.New("bad colour")
			}
			if (k == "front" && str != s.Front) || (k == "top" && str != s.Top) {
				changed = true
			}
			if k == "front" {
				s.Front = str
			} else {
				s.Top = str
			}
		case "auto_capture":
			s.AutoCapture, _ = v.(bool)
		case "capture_settle_ms":
			s.CaptureSettleMs = int(toFloat(v))
		case "solve_timeout":
			s.SolveTimeout = toFloat(v)
		case "return_home":
			s.ReturnHome, _ = v.(bool)
		}
	}
	if changed {
		if _, err := centerColors(s.Front[0], s.Top[0]); err != nil {
			c.mu.Unlock()
			return err
		}
		c.colors = solvedColors(s.Front[0], s.Top[0])
		c.solution = nil
		c.solutionInfo = map[string]any{}
		c.thumbs = map[string]string{}
		c.scanned = nil
	}
	c.settings = s
	c.saveSettings()
	c.mu.Unlock()
	c.PushState()
	return nil
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}

func (c *Controller) SetColors(colors string) error {
	if len(colors) != 54 {
		return errors.New("need 54 stickers")
	}
	c.mu.Lock()
	c.colors = colors
	c.stateKnown = true
	c.solution = nil
	c.solutionInfo = map[string]any{}
	c.warnings = nil
	c.mu.Unlock()
	c.PushState()
	return nil
}

func (c *Controller) Reset() error {
	return c.SetColors(solvedColors(c.settings.Front[0], c.settings.Top[0]))
}

// ---------------------------------------------------------------- solving
func (c *Controller) Solve() error {
	c.mu.Lock()
	if !c.solverReady {
		c.mu.Unlock()
		return errors.New("Solver is still initialising")
	}
	colors := c.colors
	tb := c.tables
	timeout := time.Duration(c.settings.SolveTimeout * float64(time.Second))
	d := c.settings.Delay
	c.mu.Unlock()

	fac, err := colorsToFacelets(colors)
	if err == nil {
		err = verifyFacelets(fac)
	}
	if err != nil {
		fixed, rots, ok := autoOrient(colors)
		if ok && rots != [6]int{} {
			colors = fixed
			c.logf("info", "Auto-corrected rotated faces: %v", rots)
			fac, _ = colorsToFacelets(colors)
		} else {
			c.mu.Lock()
			c.solution = nil
			c.solutionInfo = map[string]any{"error": err.Error()}
			c.mu.Unlock()
			c.PushState()
			return err
		}
	}
	sols, err := Solve(tb, fac, 24, timeout)
	if err != nil {
		return err
	}
	var best []string
	bestMs := -1
	shortest := 99
	for _, s := range sols {
		_, _, ms := buildTimeline(toRobotMoves(s), NORMAL, d)
		if bestMs < 0 || ms < bestMs {
			best, bestMs = s, ms
		}
		if len(s) < shortest {
			shortest = len(s)
		}
	}
	rm := toRobotMoves(best)
	ev, _, _ := buildTimeline(rm, NORMAL, d)
	flips := 0
	for _, e := range ev {
		if e.Kind == "pose" {
			flips++
		}
	}
	c.mu.Lock()
	c.colors = colors
	c.solution = best
	c.solutionInfo = map[string]any{"moves": len(best), "robot_moves": len(rm), "flips": flips,
		"est_ms": bestMs, "candidates": len(sols), "robot_string": rm, "shortest": shortest}
	c.mu.Unlock()
	c.PushState()
	return nil
}

func (c *Controller) Execute() error {
	if err := c.requireRobot(); err != nil {
		return err
	}
	c.mu.Lock()
	has := c.solution != nil
	c.mu.Unlock()
	if !has {
		if err := c.Solve(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	moves := append([]string{}, c.solution...)
	c.mu.Unlock()
	if len(moves) == 0 {
		return errors.New("Cube is already solved")
	}
	return c.startTask("solve", func() error { return c.runMoves(moves, true) })
}

func (c *Controller) Scramble(n int) error {
	if err := c.requireRobot(); err != nil {
		return err
	}
	moves := randomScramble(n, rand.New(rand.NewSource(time.Now().UnixNano())))
	c.logf("info", "Scramble: %s", strings.Join(moves, " "))
	return c.startTask("scramble", func() error { return c.runMoves(moves, false) })
}

func (c *Controller) runMoves(moves []string, isSolve bool) error {
	d := c.settings.Delay
	rm := toRobotMoves(moves)
	c.bus.Emit(map[string]any{"type": "run", "moves": moves, "solve": isSolve, "delay": d})
	idx := 0
	for _, chunk := range chunkMoves(rm) {
		o := c.rq.Orientation
		events, endO, total := buildTimeline(chunk, o, d)
		tBusy, err := c.rq.Start(fmt.Sprintf("MOVE %d %d %s", d, o, chunk))
		if err != nil {
			return err
		}
		idx = c.schedule(events, tBusy, idx)
		if !c.rq.WaitIdle(time.Duration(total)*time.Millisecond + 30*time.Second) {
			return errors.New("robot timeout")
		}
		if c.rq.aborted.IsSet() {
			return errors.New("aborted")
		}
		c.flushTimers()
		c.rq.Orientation = endO
	}
	if c.settings.ReturnHome && c.rq.Orientation == INVERT {
		tBusy, err := c.rq.Start("SEQ " + rotateCubeSeq(NORMAL, d))
		if err != nil {
			return err
		}
		c.schedule([]*TLEvent{{At: 100 + 4*d, Dur: d, Kind: "pose", Pose: "NORMAL"}}, tBusy, idx)
		c.rq.WaitIdle(30 * time.Second)
		c.flushTimers()
		c.rq.Orientation = NORMAL
	}
	if isSolve {
		c.mu.Lock()
		c.solution = nil
		c.solutionInfo = map[string]any{"done": true}
		c.mu.Unlock()
	}
	c.bus.Emit(map[string]any{"type": "run_done", "solve": isSolve})
	return nil
}

func (c *Controller) schedule(events []*TLEvent, tBusy time.Time, k int) int {
	c.fireMu.Lock()
	defer c.fireMu.Unlock()
	for _, ev := range events {
		if ev.Kind == "move" {
			ev.Index = k
			k++
		}
		e := ev
		delay := time.Until(tBusy.Add(time.Duration(e.At) * time.Millisecond))
		if delay < 0 {
			delay = 0
		}
		e.timer = time.AfterFunc(delay, func() { c.fire(e) })
		c.timers = append(c.timers, e)
	}
	return k
}

func (c *Controller) fire(e *TLEvent) {
	c.fireMu.Lock()
	if e.fired {
		c.fireMu.Unlock()
		return
	}
	e.fired = true
	c.fireMu.Unlock()
	if e.Kind == "move" {
		c.mu.Lock()
		c.colors = applyMove(c.colors, e.Move)
		col := c.colors
		c.mu.Unlock()
		c.bus.Emit(map[string]any{"type": "move", "move": e.Move, "dur": e.Dur, "index": e.Index, "colors": col})
	} else {
		c.mu.Lock()
		c.pose = e.Pose
		c.mu.Unlock()
		c.bus.Emit(map[string]any{"type": "pose", "pose": e.Pose, "dur": e.Dur})
	}
}

func (c *Controller) flushTimers() {
	c.fireMu.Lock()
	ts := c.timers
	c.timers = nil
	c.fireMu.Unlock()
	for _, t := range ts {
		t.timer.Stop()
		c.fire(t)
	}
}

// ---------------------------------------------------------------- scanning
func (c *Controller) StartScan() error {
	c.mu.Lock()
	if c.busyTask != "" {
		b := c.busyTask
		c.mu.Unlock()
		return errors.New("Busy: " + b)
	}
	front, top := c.settings.Front[0], c.settings.Top[0]
	steps := walkPlan(c.plan, front, top)
	c.scan = &ScanState{Total: len(steps), Color: steps[0].Color, Phase: "moving", StepID: steps[0].Step.ID,
		Samples: map[int][3]int{}}
	c.thumbs = map[string]string{}
	c.scanned = nil
	c.warnings = nil
	home := []byte(solvedColors(front, top))
	for i := range home {
		if i%9 != 4 {
			home[i] = '?'
		}
	}
	c.colors = string(home)
	c.solution = nil
	c.solutionInfo = map[string]any{}
	c.mu.Unlock()
	for len(c.captureCh) > 0 {
		<-c.captureCh
	}
	return c.startTask("scan", func() error { return c.scanRun(steps) })
}

func (c *Controller) scanRun(steps []WalkStep) error {
	d := c.settings.Delay
	if c.settings.ScanDelayMin > d {
		d = c.settings.ScanDelayMin
	}
	useRobot := c.link.Connected()
	if !useRobot {
		c.logf("info", "Robot not connected: scanning in manual mode (turn the cube by hand)")
	}
	centers, _ := centerColors(c.settings.Front[0], c.settings.Top[0])
	s := c.scan
	for i, w := range steps {
		c.mu.Lock()
		s.Index, s.Color, s.Phase, s.StepID, s.View, s.Cells = i, w.Color, "moving", w.Step.ID, w.View, w.Cells
		c.mu.Unlock()
		c.PushState()
		if useRobot {
			if err := c.rq.Run("SEQ " + fillSeq(w.Step.Present, d)); err != nil {
				return c.scanFail(err)
			}
		}
		c.mu.Lock()
		s.Phase = "capture"
		c.mu.Unlock()
		hint := any(nil)
		if !useRobot {
			hint = "Hold the cube so that this face points at the camera"
		}
		c.bus.Emit(map[string]any{"type": "scan_prompt", "index": i, "total": len(steps),
			"color": string(w.Color), "color_name": colorNames[w.Color], "auto": c.settings.AutoCapture,
			"settle_ms": c.settings.CaptureSettleMs, "manual_hint": hint})
		c.PushState()
		data := <-c.captureCh
		if data == nil || s.Done {
			return c.scanFail(errors.New("aborted"))
		}
		tiles := data["tiles"].([]any)
		c.mu.Lock()
		prov := []byte(c.colors)
		for k := 0; k < 9; k++ {
			t := tiles[k].([]any)
			rgb := [3]int{int(toFloat(t[0])), int(toFloat(t[1])), int(toFloat(t[2]))}
			s.Samples[w.Cells[k]] = rgb
			if w.Cells[k]%9 == 4 {
				prov[w.Cells[k]] = w.Color
			} else {
				prov[w.Cells[k]] = nearestDefault(rgb)
			}
		}
		c.colors = string(prov)
		c.scanned = append(c.scanned, string(w.Color))
		if th, ok := data["thumb"].(string); ok && th != "" {
			c.thumbs[string(w.Color)] = th
		}
		if lg, ok := data["logo"].(map[string]any); ok {
			if det, _ := lg["detected"].(bool); det && w.Color != 'W' {
				c.warnings = append(c.warnings, fmt.Sprintf(
					"A GAN logo was seen on the %s centre - check the front/top choice.", colorNames[w.Color]))
			}
		}
		s.Phase = "reverting"
		c.mu.Unlock()
		c.PushState()
		if useRobot {
			if err := c.rq.Run("SEQ " + fillSeq(w.Step.Revert, d)); err != nil {
				return c.scanFail(err)
			}
		}
	}
	cols := classify(s.Samples, centers)
	fixed, rots, ok := autoOrient(cols)
	c.mu.Lock()
	if !ok {
		c.colors = cols
		c.warnings = append(c.warnings, "Scan is not a valid cube - tap stickers to fix them, then Calculate.")
	} else {
		if rots != [6]int{} {
			c.logf("info", "Auto-corrected face rotation(s): %v", rots)
		}
		c.colors = fixed
	}
	c.stateKnown = true
	s.Done = true
	s.Phase = "done"
	c.mu.Unlock()
	c.PushState()
	c.bus.Emit(map[string]any{"type": "scan_done"})
	c.mu.Lock()
	c.scan = nil
	ready := c.solverReady
	c.mu.Unlock()
	if ok && ready {
		if err := c.Solve(); err != nil {
			c.logf("error", "Solve failed: %v", err)
		}
	}
	return nil
}

func (c *Controller) scanFail(err error) error {
	c.mu.Lock()
	c.scan = nil
	c.mu.Unlock()
	return err
}

func (c *Controller) Capture(data map[string]any) error {
	c.mu.Lock()
	ok := c.scan != nil && c.scan.Phase == "capture"
	c.mu.Unlock()
	if !ok {
		return errors.New("Not waiting for a capture")
	}
	t, _ := data["tiles"].([]any)
	if len(t) != 9 {
		return errors.New("need 9 tiles")
	}
	for _, x := range t {
		if a, ok := x.([]any); !ok || len(a) < 3 {
			return errors.New("bad tile")
		}
	}
	c.mu.Lock()
	c.scan.Phase = "captured"
	c.mu.Unlock()
	c.captureCh <- data
	return nil
}

func (c *Controller) CancelScan() {
	c.mu.Lock()
	if c.scan != nil {
		c.scan.Done = true
	}
	c.mu.Unlock()
	select {
	case c.captureCh <- nil:
	default:
	}
	if c.link.Connected() && c.rq.GetState() == "BUSY" {
		c.rq.Abort()
	}
}

func (c *Controller) SetPlan(p ScanPlan) error {
	if p.CameraFace == "" {
		p.CameraFace = "U"
	}
	if !validPlan(p) {
		return errors.New("invalid plan")
	}
	c.mu.Lock()
	c.plan = p
	c.mu.Unlock()
	savePlan(c.cfgDir, p)
	c.PushState()
	return nil
}

func (c *Controller) PhoneHello(info map[string]any) {
	id, _ := info["id"].(string)
	if id == "" {
		id = "phone"
	}
	info["seen"] = float64(time.Now().UnixNano()) / 1e9
	c.mu.Lock()
	c.phones[id] = info
	c.mu.Unlock()
	c.PushState()
}
