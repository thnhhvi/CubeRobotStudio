package main

import (
	crand "crypto/rand"
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
	LowPower        bool    `json:"low_power"`
	FlipGripUs      int     `json:"flip_grip_us"` // H clamp: this much harder than L while flipping
	SyncMs          int     `json:"sync_ms"`      // time of one synced step (paired servos)
	FollowPct       int     `json:"follow_pct"`   // following sliders back off this % of C->R at 45 deg
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
	Labels  []string    // per step: colour name once known, else position ("left-arm face")
	Colors  []byte      // per step: colour letter once known, else '?'
	Obs     []centerObs // measured centres so far
	Known   bool        // centre colours assigned (after the first two faces)
	Second  bool        // second look: other arms holding, grippers moved off the stickers
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
	confirmCh    chan string
	beginCh      chan bool
	heard        *Event
	reconnecting bool
	pairCode     string
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
			ScanDelayMin: 200, SolveTimeout: 3, ReturnHome: true, FlipGripUs: 40, SyncMs: 1000, FollowPct: 60},
		stateKnown: true, pose: "NORMAL", thumbs: map[string]string{}, phones: map[string]map[string]any{},
		solutionInfo: map[string]any{}, captureCh: make(chan map[string]any, 4), confirmCh: make(chan string, 4), beginCh: make(chan bool, 2), heard: NewEvent(false)}
	if b, err := os.ReadFile(filepath.Join(c.cfgDir, "settings.json")); err == nil {
		json.Unmarshal(b, &c.settings)
	}
	if _, err := centerColors(c.settings.Front[0], c.settings.Top[0]); err != nil {
		c.settings.Front, c.settings.Top = "R", "W"
	}
	c.plan = loadPlan(c.cfgDir)
	c.NewPairCode()
	c.colors = solvedColors(c.settings.Front[0], c.settings.Top[0])
	c.link = &Link{onLine: c.onLine, onStatus: c.onLinkStatus}
	c.link.onDrop = c.onDrop
	c.rq = NewRobotQueue(c.link, c.bus.Emit)
	c.rq.SetLowPower(c.settings.LowPower)
	setSyncTime(c.settings.SyncMs)
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

// cfg returns a copy of the settings (safe from any goroutine).
func (c *Controller) cfg() Settings { c.mu.Lock(); defer c.mu.Unlock(); return c.settings }

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
		scan = map[string]any{"index": s.Index, "total": s.Total, "color": string(s.Color), "phase": s.Phase, "second_look": s.Second,
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
		cp := map[string]any{}
		for k, v := range p {
			cp[k] = v
		}
		phones = append(phones, cp)
	}
	// copies: the snapshot is turned into JSON after the lock is released
	thumbs := map[string]string{}
	for k, v := range c.thumbs {
		thumbs[k] = v
	}
	info := map[string]any{}
	for k, v := range c.solutionInfo {
		info[k] = v
	}
	return map[string]any{
		"type": "state", "colors": c.colors, "state_known": c.stateKnown, "settings": c.settings,
		"speed_range": []int{speedMin, speedMax},
		"robot": map[string]any{"connected": c.link.Connected(), "port": c.link.Port(),
			"state": c.rq.GetState(), "orientation": c.rq.GetOrientation(),
			"calibration_fw": c.rq.CalibrationFirmware(), "glide": c.rq.Glide()},
		"pose": c.pose, "solver_ready": c.solverReady, "solution": sol, "solution_info": info,
		"busy_task": busy, "scan": scan, "scan_order": c.scanOrder(), "centers": cm, "thumbs": thumbs,
		"scanned": append([]string{}, c.scanned...), "scan_steps": c.scanSteps(), "phones": phones,
		"warnings": append([]string{}, c.warnings...), "pair_code": c.pairCode,
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
	c.rq.mu.Lock()
	c.rq.calibFW = false
	c.rq.mu.Unlock()
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
		c.rq.QuerySliders()
		c.checkGlide()
		return nil
	}
	// The Nano resets when the port opens: wait for its banner, then PING.
	c.logf("info", "Waiting for the Arduino to boot…")
	c.heard.Wait(3 * time.Second)
	for try := 0; try < 3; try++ {
		c.heard.Clear()
		c.link.Send("PING")
		if c.heard.Wait(2 * time.Second) {
			if c.rq.CalibrationFirmware() {
				c.PushState()
				return nil // message already logged; commands are blocked
			}
			c.logf("info", "Arduino answered on %s", port)
			c.rq.QuerySliders()
			c.logf("info", "Slider positions: %s", c.sliderText())
			c.checkGlide()
			return nil
		}
	}
	c.logf("error", "No reply from the Arduino on %s. Check: the right COM port; the HC-06/HC-05 "+
		"Bluetooth module unplugged from RX/TX while using USB; firmware flashed with CALIBRATE false; "+
		"Serial Monitor in the Arduino IDE closed.", port)
	return nil
}

const glideHelp = "This is the stock firmware: when two spinners turn together (cube rolls while " +
	"scanning, flips while solving) one can run ahead and turn a single face instead of the whole cube. " +
	"Upload the firmware from the arduino/Arduino folder of Cube Robot Studio (CubeRobotFirmware.zip on the " +
	"release page): it moves paired spinners on one timed path so they stay in sync. Your calibration is kept."

// checkGlide turns on synced spinner moves when the firmware has them, else warns.
func (c *Controller) checkGlide() {
	if c.rq.EnableGlide() {
		c.rq.SetGrip(c.cfg().FlipGripUs, c.cfg().FollowPct)
		c.logf("info", "Firmware glide on: spinners that turn together move in sync; flips clamp %d µs harder", c.cfg().FlipGripUs)
	} else if !c.rq.CalibrationFirmware() {
		c.logf("error", glideHelp)
	}
	c.PushState()
}

func (c *Controller) Disconnect() {
	c.mu.Lock()
	c.reconnecting = false // a manual disconnect stops auto-reconnect
	c.mu.Unlock()
	c.link.Disconnect()
	c.PushState()
}

// onDrop: the Arduino vanished while connected (USB reset / brown-out / cable).
// Any running job is stopped and the same port is reopened for up to 30 s.
func (c *Controller) onDrop(port, reason string) {
	c.logf("error", "%s", reason)
	c.rq.Abort() // unblock whatever waited for the robot; state is unknown now
	c.mu.Lock()
	if c.busyTask == "solve" || c.busyTask == "scramble" {
		c.stateKnown = false
		c.warnings = append(c.warnings, "The robot dropped off USB during a move: the cube on screen may not match.")
	}
	if c.reconnecting {
		c.mu.Unlock()
		return
	}
	c.reconnecting = true
	c.mu.Unlock()
	go func() {
		defer func() { c.mu.Lock(); c.reconnecting = false; c.mu.Unlock() }()
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			c.mu.Lock()
			want := c.reconnecting
			c.mu.Unlock()
			if !want || c.link.Connected() {
				return
			}
			if err := c.Connect(port); err == nil {
				c.logf("info", "Reconnected to %s", port)
				return
			}
		}
		c.logf("error", "Could not reconnect to %s - check the USB cable and the servo power supply, then Connect again.", port)
	}()
}

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

// CloseArms clamps the cube: front and back sliders to L, 500 ms, then right and left.
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
	select {
	case c.confirmCh <- "":
	default:
	}
	select {
	case c.beginCh <- false:
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
	d := c.cfg().Delay
	if c.cfg().ScanDelayMin > d {
		d = c.cfg().ScanDelayMin
	}
	s := fillSeq(seq, d)
	return c.startTask("jog", func() error { return c.rq.Run("SEQ " + s) })
}

func (c *Controller) UpdateSettings(data map[string]any) error {
	c.mu.Lock()
	s := c.settings
	changed, grip := false, false
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
		case "low_power":
			s.LowPower, _ = v.(bool)
		case "sync_ms":
			s.SyncMs = int(toFloat(v))
			if s.SyncMs < 600 {
				s.SyncMs = 600
			}
			if s.SyncMs > 2500 {
				s.SyncMs = 2500
			}
		case "follow_pct":
			s.FollowPct = int(toFloat(v))
			if s.FollowPct < 0 {
				s.FollowPct = 0
			}
			if s.FollowPct > 100 {
				s.FollowPct = 100
			}
			grip = true
		case "flip_grip_us":
			s.FlipGripUs = int(toFloat(v))
			if s.FlipGripUs < 0 {
				s.FlipGripUs = 0
			}
			if s.FlipGripUs > 150 {
				s.FlipGripUs = 150
			}
			grip = true
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
	c.rq.SetLowPower(s.LowPower)
	setSyncTime(s.SyncMs)
	if grip {
		c.rq.SetGrip(s.FlipGripUs, s.FollowPct)
	}
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
	return c.SetColors(solvedColors(c.cfg().Front[0], c.cfg().Top[0]))
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
	d := c.cfg().Delay
	rm := toRobotMoves(moves)
	c.bus.Emit(map[string]any{"type": "run", "moves": moves, "solve": isSolve, "delay": d})
	idx := 0
	for _, b := range buildSeqBatches(rm, c.rq.GetOrientation(), d) {
		events, _, total := buildTimeline(b.Moves, b.StartOrient, d)
		tBusy, err := c.rq.Start("SEQ " + b.Seq)
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
		c.rq.SetOrientation(b.EndOrient)
	}
	if c.cfg().ReturnHome && c.rq.GetOrientation() == INVERT {
		tBusy, err := c.rq.Start("SEQ " + rotateCubeSeq(NORMAL, d))
		if err != nil {
			return err
		}
		c.schedule([]*TLEvent{{At: 100 + 4*d, Dur: d, Kind: "pose", Pose: "NORMAL"}}, tBusy, idx)
		c.rq.WaitIdle(30 * time.Second)
		c.flushTimers()
		c.rq.SetOrientation(NORMAL)
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
	// the geometry (which facelets each picture shows) does not depend on the colours;
	// the centre colours are measured during the scan
	steps := walkPlan(c.plan, c.settings.Front[0], c.settings.Top[0])
	labels, cols := []string{}, []byte{}
	for _, w := range steps {
		labels = append(labels, faceLabel(FACES[w.Cells[4]/9]))
		cols = append(cols, '?')
	}
	c.scan = &ScanState{Total: len(steps), Color: '?', Phase: "ready", StepID: steps[0].Step.ID,
		Samples: map[int][3]int{}, Labels: labels, Colors: cols}
	c.thumbs = map[string]string{}
	c.scanned = nil
	c.warnings = nil
	c.colors = strings.Repeat("?", 54)
	c.solution = nil
	c.solutionInfo = map[string]any{}
	c.mu.Unlock()
	for len(c.captureCh) > 0 {
		<-c.captureCh
	}
	for len(c.confirmCh) > 0 {
		<-c.confirmCh
	}
	for len(c.beginCh) > 0 {
		<-c.beginCh
	}
	return c.startTask("scan", func() error { return c.scanRun(steps) })
}

// assignCenters fixes the orientation from the measured centres (called with c.mu held).
func (c *Controller) assignCenters(s *ScanState, steps []WalkStep, final bool) {
	rank := rankOrientations(s.Obs)
	best := rank[0]
	prevF, prevT := c.settings.Front[0], c.settings.Top[0]
	c.settings.Front, c.settings.Top = string(best.Front), string(best.Top)
	centers, _ := centerColors(best.Front, best.Top)
	col := []byte(c.colors)
	for i := 0; i < 6; i++ {
		col[9*i+4] = centers[FACES[i]]
	}
	c.colors = string(col)
	for i, w := range walkPlan(c.plan, best.Front, best.Top) {
		s.Colors[i] = w.Color
		s.Labels[i] = colorNames[w.Color]
		_ = steps
	}
	s.Color = s.Colors[s.Index]
	if len(rank) > 1 && rank[1].Cost-best.Cost < 15 {
		c.warnings = append(c.warnings, "Centre colours were hard to tell apart - check the net before solving.")
	}
	if final && s.Known && (best.Front != prevF || best.Top != prevT) {
		c.warnings = append(c.warnings, "Centre colours were corrected after seeing all six faces.")
	}
	s.Known = true
	c.saveSettings()
	go c.logf("info", "Centre colours: %s in the front arm, %s on top", colorNames[best.Front], colorNames[best.Top])
}

func (c *Controller) scanRun(steps []WalkStep) error {
	d := c.cfg().Delay
	if c.cfg().ScanDelayMin > d {
		d = c.cfg().ScanDelayMin
	}
	useRobot := c.link.Connected()
	if !useRobot {
		c.logf("info", "Robot not connected: scanning in manual mode (turn the cube by hand)")
	} else if c.rq.Glide() {
		c.logf("info", "Synced moves on (firmware glide), %d ms per paired step", syncTime())
	} else {
		c.logf("error", "Synced moves are OFF: "+glideHelp)
	}
	s := c.scan
	// scan mode is armed: wait until the user presses "Start scanning"
	c.bus.Emit(map[string]any{"type": "scan_ready", "total": len(steps), "front": "", "top": ""})
	c.PushState()
	if ok := <-c.beginCh; !ok || c.scanCancelled(s) {
		return c.scanFail(errors.New("aborted"))
	}
	for i, w := range steps {
		c.mu.Lock()
		s.Index, s.Color, s.Phase, s.StepID, s.View, s.Cells = i, s.Colors[i], "moving", w.Step.ID, w.View, w.Cells
		label := s.Labels[i]
		c.mu.Unlock()
		c.PushState()
		if useRobot {
			if err := c.rq.Run("SEQ " + fillSeq(w.Step.Present, d)); err != nil {
				return c.scanFail(err)
			}
		}
		hint := any(nil)
		if !useRobot {
			hint = "Hold the cube so that this face points at the camera"
		}
		// capture -> show the result -> wait for the user to Confirm (or Retake)
		for {
			c.mu.Lock()
			s.Phase = "capture"
			c.mu.Unlock()
			for len(c.confirmCh) > 0 {
				<-c.confirmCh
			}
			c.bus.Emit(map[string]any{"type": "scan_prompt", "index": i, "total": len(steps),
				"color": string(s.Colors[i]), "color_name": label, "auto": false,
				"settle_ms": c.cfg().CaptureSettleMs, "manual_hint": hint})
			c.PushState()
			stopHold := c.holdPowered(useRobot)
			data := <-c.captureCh
			if data == nil || c.scanCancelled(s) {
				stopHold()
				return c.scanFail(errors.New("aborted"))
			}
			fr := readFace(data)
			if fr.hiddenEdges() > 0 && useRobot {
				// second look: the other pair takes the cube over and the rolling pair
				// lets go, so the stickers under its grippers come into view
				stopHold()
				fr2, err := c.secondLook(s, w.Step, i, len(steps), label, d)
				if err != nil {
					return c.scanFail(err)
				}
				if fr2 != nil {
					fr.merge(fr2)
				}
				stopHold = c.holdPowered(useRobot)
			}
			th, logo, hidden := fr.thumb, fr.logo, fr.hidden
			c.mu.Lock()
			before := c.colors
			prov := []byte(c.colors)
			preview := make([]string, 9)
			nHidden := 0
			if logo && w.Cells[4]%9 == 4 {
				hidden[4] = false
			}
			for k := 0; k < 9; k++ {
				rgb := fr.rgb[k]
				cell := w.Cells[k]
				switch {
				case cell%9 == 4 && logo:
					prov[cell] = 'W'
					s.Samples[cell] = rgb
				case cell%9 == 4 && s.Known:
					prov[cell] = s.Colors[i]
					s.Samples[cell] = rgb
				case hidden[k]:
					delete(s.Samples, cell)
					prov[cell] = '?'
				default:
					s.Samples[cell] = rgb
					prov[cell] = nearestDefault(rgb)
				}
				if hidden[k] {
					nHidden++
				}
				preview[k] = string(prov[cell])
			}
			c.colors = string(prov)
			if th != "" {
				c.thumbs[fmt.Sprint(i)] = th
			}
			s.Phase = "confirm"
			c.mu.Unlock()
			c.bus.Emit(map[string]any{"type": "scan_review", "index": i, "total": len(steps),
				"color": string(s.Colors[i]), "color_name": label, "preview": preview,
				"thumb": th, "logo": logo, "hidden": hidden, "hidden_count": nHidden})
			c.PushState()
			act := <-c.confirmCh
			stopHold()
			if act == "" || c.scanCancelled(s) {
				return c.scanFail(errors.New("aborted"))
			}
			if act == "retake" {
				c.mu.Lock()
				c.colors = before
				delete(c.thumbs, fmt.Sprint(i))
				c.mu.Unlock()
				continue
			}
			c.mu.Lock()
			c.scanned = append(c.scanned, fmt.Sprint(i))
			if !hidden[4] { // a centre hidden by a gripper says nothing about the colours
				s.Obs = append(s.Obs, centerObs{Face: FACES[w.Cells[4]/9], RGB: fr.rgb[4], Logo: logo})
			}
			if !s.Known && len(s.Obs) >= 2 {
				c.assignCenters(s, steps, false) // after the first two faces
			}
			c.mu.Unlock()
			break
		}
		c.mu.Lock()
		s.Phase = "reverting"
		c.mu.Unlock()
		c.PushState()
		if useRobot {
			if err := c.rq.Run("SEQ " + fillSeq(w.Step.Revert, d)); err != nil {
				return c.scanFail(err)
			}
		}
	}
	c.mu.Lock()
	finish := c.plan.Finish
	c.mu.Unlock()
	if useRobot && finish != "" { // back to where the cube started
		c.mu.Lock()
		s.Phase = "reverting"
		c.mu.Unlock()
		c.PushState()
		if err := c.rq.Run("SEQ " + fillSeq(finish, d)); err != nil {
			return c.scanFail(err)
		}
	}
	c.mu.Lock()
	c.assignCenters(s, steps, true) // re-check with all six centres
	centers, _ := centerColors(c.settings.Front[0], c.settings.Top[0])
	logos := 0
	for _, o := range s.Obs {
		if o.Logo {
			logos++
			if centers[o.Face] != 'W' {
				c.warnings = append(c.warnings, "A GAN logo was seen on a centre that is not white - check it.")
			}
		}
	}
	c.mu.Unlock()
	cols := classify(s.Samples, centers)
	nHidden := strings.Count(cols, "?")
	fixed, rots, status := fixScan(cols)
	ok := status == "ok"
	c.mu.Lock()
	if status == "ambiguous" {
		c.colors = cols
		c.warnings = append(c.warnings, fmt.Sprintf("%d sticker(s) were hidden by the grippers and could not be worked out - tap the ? stickers to set them (or rescan), then Calculate.", nHidden))
	} else if !ok {
		c.colors = cols
		c.warnings = append(c.warnings, "Scan is not a valid cube - tap stickers to fix them, then Calculate.")
	} else {
		if nHidden > 0 {
			c.logf("info", "Filled in %d sticker(s) hidden by the grippers", nHidden)
		}
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

// faceRead is one capture: sticker colours (gripper pixels left out), which stickers
// are hidden by a gripper, the thumbnail and the GAN logo check.
type faceRead struct {
	rgb    [9][3]int
	hidden []bool
	thumb  string
	logo   bool
}

func readFace(data map[string]any) *faceRead {
	fr := &faceRead{hidden: make([]bool, 9)}
	tiles := data["tiles"].([]any)
	fr.thumb, _ = data["thumb"].(string)
	// the phone ignores black gripper pixels and marks mostly hidden stickers; the
	// laptop re-checks on the thumbnail (also for older phone apps)
	phoneOcc, hasPhoneOcc := data["occluded"].([]any)
	for k := 0; k < 9 && hasPhoneOcc && k < len(phoneOcc); k++ {
		fr.hidden[k], _ = phoneOcc[k].(bool)
	}
	vis, hasVis := thumbTiles(fr.thumb)
	for k := 0; k < 9; k++ {
		t := tiles[k].([]any)
		fr.rgb[k] = [3]int{int(toFloat(t[0])), int(toFloat(t[1])), int(toFloat(t[2]))}
		if hasVis {
			if vis[k].Visible < minVisible {
				fr.hidden[k] = true
			} else if !hasPhoneOcc && vis[k].Visible < 0.9 && vis[k].OK {
				fr.rgb[k] = vis[k].RGB // partly covered: use only the visible part
			}
		}
	}
	if lg, ok := data["logo"].(map[string]any); ok {
		fr.logo, _ = lg["detected"].(bool)
	}
	if !fr.logo && fr.thumb != "" { // laptop double-check, any logo direction
		fr.logo, _ = logoInThumb(fr.thumb)
	}
	return fr
}

func (fr *faceRead) hiddenEdges() int {
	n := 0
	for k, h := range fr.hidden {
		if h && k != 4 {
			n++
		}
	}
	return n
}

// merge fills stickers hidden in fr with the ones the second look saw.
func (fr *faceRead) merge(o *faceRead) {
	for k := 0; k < 9; k++ {
		if fr.hidden[k] && !o.hidden[k] {
			fr.rgb[k], fr.hidden[k] = o.rgb[k], false
		}
	}
	fr.logo = fr.logo || o.logo
}

// secondLook hands the cube from the rolling pair to the other pair, takes
// another picture of the same face and hands it back. nil result = no usable picture.
func (c *Controller) secondLook(s *ScanState, st ScanStep, i, total int, label string, d int) (*faceRead, error) {
	off, back := st.look2()
	c.logf("info", "Some stickers are hidden by the grippers - taking a second look with the other arms holding")
	c.mu.Lock()
	s.Phase, s.Second = "moving", true
	c.mu.Unlock()
	c.PushState()
	defer func() { c.mu.Lock(); s.Second = false; c.mu.Unlock() }()
	if err := c.rq.Run("SEQ " + fillSeq(off, d)); err != nil {
		return nil, err
	}
	c.mu.Lock()
	s.Phase = "capture"
	c.mu.Unlock()
	c.bus.Emit(map[string]any{"type": "scan_prompt", "index": i, "total": total,
		"color": string(s.Colors[i]), "color_name": label, "auto": true, "second_look": true,
		"settle_ms": c.cfg().CaptureSettleMs, "manual_hint": nil})
	c.PushState()
	go func() { // the phone captures by itself; "Scan face" works as well
		time.Sleep(time.Duration(c.cfg().CaptureSettleMs+300) * time.Millisecond)
		c.mu.Lock()
		waiting := c.scan == s && s.Phase == "capture"
		c.mu.Unlock()
		if waiting {
			c.bus.Emit(map[string]any{"type": "capture_now"})
		}
	}()
	stopHold := c.holdPowered(true)
	data := <-c.captureCh
	stopHold()
	if data == nil || c.scanCancelled(s) {
		return nil, errors.New("aborted")
	}
	fr := readFace(data)
	if err := c.rq.Run("SEQ " + fillSeq(back, d)); err != nil {
		return nil, err
	}
	return fr, nil
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
	select {
	case c.confirmCh <- "":
	default:
	}
	select {
	case c.beginCh <- false:
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

// ---------------------------------------------------------------- phone pairing
// A 6-digit code shown on the laptop (as digits and inside the QR code). The phone
// app finds the laptop that shows this code (UDP beacon, then a scan of its own subnet).
func (c *Controller) NewPairCode() string {
	var b [4]byte
	crand.Read(b[:])
	n := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 1000000
	code := fmt.Sprintf("%06d", n)
	c.mu.Lock()
	c.pairCode = code
	c.mu.Unlock()
	return code
}

func (c *Controller) PairCode() string { c.mu.Lock(); defer c.mu.Unlock(); return c.pairCode }

func (c *Controller) Pair(code string) error {
	if strings.TrimSpace(code) != c.PairCode() {
		return errors.New("wrong code")
	}
	c.logf("info", "Phone paired with code %s", code)
	return nil
}

// holdPowered keeps the servos powered while the cube is lifted for the camera: the
// firmware detaches every servo 3 s after a command (no holding torque), so the
// current slider positions are re-sent every 2 s until stop is closed.
func (c *Controller) holdPowered(useRobot bool) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if !useRobot || c.cfg().LowPower {
			return // low-power mode: no keep-alive (holding current would brown out USB)
		}
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				st := c.rq.SliderStates()
				seq := ""
				for _, s := range []byte("FBRL") {
					if v, ok := st[s]; ok {
						seq += string(s) + string(v)
					}
				}
				if seq != "" && c.rq.GetState() == "IDLE" {
					c.rq.RunQuiet("SEQ " + seq)
				}
			}
		}
	}()
	return func() { close(done); <-finished }
}

// ConfirmScan answers the review step of the current face: "confirm" or "retake".
func (c *Controller) ConfirmScan(action string) error {
	c.mu.Lock()
	ok := c.scan != nil && c.scan.Phase == "confirm"
	if ok {
		c.scan.Phase = "confirmed"
	}
	c.mu.Unlock()
	if !ok {
		return errors.New("Nothing to confirm right now")
	}
	if action != "retake" {
		action = "confirm"
	}
	c.confirmCh <- action
	return nil
}

// BeginScan: the user pressed "Start scanning" (cube loaded with the chosen front/top).
func (c *Controller) BeginScan() error {
	c.mu.Lock()
	ok := c.scan != nil && c.scan.Phase == "ready"
	if ok {
		c.scan.Phase = "moving"
	}
	c.mu.Unlock()
	if !ok {
		return errors.New("Scan mode is not waiting to start")
	}
	c.beginCh <- true
	return nil
}

// TriggerCapture: "Scan face" pressed on the laptop -> the phone captures now.
func (c *Controller) TriggerCapture() error {
	c.mu.Lock()
	ok := c.scan != nil && c.scan.Phase == "capture"
	c.mu.Unlock()
	if !ok {
		return errors.New("Not waiting for a face to be scanned")
	}
	recent := false
	now := float64(time.Now().UnixNano()) / 1e9
	c.mu.Lock()
	for _, p := range c.phones {
		if seen, _ := p["seen"].(float64); now-seen < 15 {
			recent = true
		}
	}
	c.mu.Unlock()
	if !recent {
		return errors.New("No phone connected - pair the phone first")
	}
	c.bus.Emit(map[string]any{"type": "capture_now"})
	return nil
}

func (c *Controller) sliderText() string {
	m := c.rq.SliderStates()
	out := ""
	for _, s := range []byte("FBRL") {
		v := m[s]
		if v == 0 {
			v = '?'
		}
		out += string(s) + "=" + string(v) + " "
	}
	return strings.TrimSpace(out)
}

// scanSteps lists the scan steps for the UI (called with c.mu held).
func (c *Controller) scanSteps() []map[string]string {
	out := []map[string]string{}
	if c.scan != nil {
		for i := range c.scan.Labels {
			out = append(out, map[string]string{"label": c.scan.Labels[i], "color": string(c.scan.Colors[i])})
		}
		return out
	}
	for _, w := range walkPlan(c.plan, c.settings.Front[0], c.settings.Top[0]) {
		out = append(out, map[string]string{"label": colorNames[w.Color], "color": string(w.Color)})
	}
	return out
}

func (c *Controller) scanCancelled(s *ScanState) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return s.Done
}
