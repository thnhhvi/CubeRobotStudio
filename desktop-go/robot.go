package main

// Arduino link (stock firmware API: PING, STATUS, SEQ, MOVE) + simulator.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	NORMAL   = 0
	INVERT   = 1
	moveBuf  = 60
	seqBuf   = 250
	openSeq  = "RRLRFRBR600"    // all sliders R: released, cube can be taken out (glide together)
	closeSeq = "FLBL600RLLL600" // front + back clamp (L), then right + left (each pair glides)
)

func toRobotMoves(moves []string) string {
	var b strings.Builder
	for _, mv := range moves {
		f := mv[0]
		switch {
		case strings.HasSuffix(mv, "'"):
			b.WriteByte(byte(unicode.ToLower(rune(f))))
		case strings.HasSuffix(mv, "2"):
			b.WriteByte(f)
			b.WriteByte(f)
		default:
			b.WriteByte(f)
		}
	}
	return b.String()
}

func orientationAfter(ch byte, cur int) int {
	c := byte(unicode.ToUpper(rune(ch)))
	if c == 'U' || c == 'D' {
		return INVERT
	}
	if c == 'L' || c == 'R' {
		return NORMAL
	}
	return cur
}

// rotateCubeSeq flips the cube between NORMAL and INVERT (same steps as the
// firmware's rotateCube) but every slider that holds the cube ends at L (clamped).
func rotateCubeSeq(o, d int) string {
	pre := "fLbR"
	if o == INVERT {
		pre = "fRbL"
	}
	s, p := sliderWait(d), pairWait(d)
	// (R/L are clamped at L between moves.) Release F/B, pre-turn the free F/B grippers,
	// clamp F/B HARD (H), ease R/L to C (touching), roll the cube while R/L follow it as
	// a base (T: touching at 0/90 deg, backing off for the corners, grippers vertical),
	// clamp R/L, then ease F/B back to L. Each step waits for the previous one to finish;
	// only the following sliders (T) move together with the rolling spinners.
	return fmt.Sprintf("FRBR%d%s%dFHBH%dRCLC%dfCfCbCbCRTLT%dRLLL%dFLBL%d", s, pre, p, s, s, p, s, s)
}

// Minimum times (ms) so a step has really finished before the next one starts, even
// at the fastest speed setting: a 90 deg spinner turn, one slider stroke, and paired
// moves (two spinners or two sliders together: the GLIDE firmware moves them along one
// timed path over 70% of the step, the rest lets them settle).
func spinWait(d int) int    { return maxInt(d, 300) }
func slider1Wait(d int) int { return maxInt(d, 400) }
func sliderWait(d int) int  { return maxInt(d, syncTime()) }   // two or more sliders
func pairWait(d int) int    { return maxInt(2*d, syncTime()) } // two spinners

// syncMs is the time of one synced (paired) step, the "Sync move time" setting. The
// servos glide over 70% of it; it must be slow enough for both servos to follow the
// path under load, or the faster one runs ahead again.
var syncMs atomic.Int32

func syncTime() int {
	if v := int(syncMs.Load()); v >= 600 {
		return v
	}
	return 600
}

func setSyncTime(ms int) {
	if ms < 600 {
		ms = 600
	}
	if ms > 2500 {
		ms = 2500
	}
	syncMs.Store(int32(ms))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// faceSeq turns one face (robot char, e.g. 'U' or 'u') with the arm that holds it;
// the slider re-clamps at L afterwards (the firmware's MOVE would leave it at C).
func faceSeq(ch byte, orient, d int) string {
	up := byte(unicode.ToUpper(rune(ch)))
	cw := unicode.IsUpper(rune(ch))
	var arm byte
	switch up {
	case 'F':
		arm = 'f'
	case 'B':
		arm = 'b'
	case 'U', 'L':
		arm = 'l'
	case 'D', 'R':
		arm = 'r'
	}
	dir := "R"
	if !cw {
		dir = "L"
	}
	slider := string(unicode.ToUpper(rune(arm)))
	a := string(arm)
	sw, sp := slider1Wait(d), spinWait(d)
	// turn (all four arms clamped), release this arm only (the other pair still holds),
	// gripper back to vertical, re-clamp.
	return fmt.Sprintf("%s%s%d%sR%d%sC%sC%d%sL%d", a, dir, sp, slider, sw, a, a, sp, slider, sw)
}

// parseSeq returns the (servo, state) pairs of a SEQ string in order (delays skipped).
func parseSeq(seq string) [][2]byte {
	out := [][2]byte{}
	for i := 0; i < len(seq); {
		if seq[i] >= '0' && seq[i] <= '9' {
			i++
			continue
		}
		if i+1 < len(seq) {
			out = append(out, [2]byte{seq[i], seq[i+1]})
		}
		i += 2
	}
	return out
}

// SeqBatch is one SEQ command (fits the firmware buffer) and the moves it performs.
type SeqBatch struct {
	Seq         string
	Moves       string
	StartOrient int
	EndOrient   int
}

func buildSeqBatches(rm string, orient, d int) []SeqBatch {
	out := []SeqBatch{}
	cur := SeqBatch{StartOrient: orient}
	for i := 0; i < len(rm); i++ {
		ch := rm[i]
		part := ""
		no := orientationAfter(ch, orient)
		if no != orient {
			part += rotateCubeSeq(no, d)
		}
		part += faceSeq(ch, no, d)
		if len(cur.Seq)+len(part) > seqBuf && cur.Seq != "" {
			cur.EndOrient = orient
			out = append(out, cur)
			cur = SeqBatch{StartOrient: orient}
		}
		cur.Seq += part
		cur.Moves += string(ch)
		orient = no
	}
	if cur.Seq != "" {
		cur.EndOrient = orient
		out = append(out, cur)
	}
	return out
}

type TLEvent struct {
	At    int    `json:"at"`
	Dur   int    `json:"dur"`
	Kind  string `json:"kind"`
	Move  string `json:"move,omitempty"`
	Pose  string `json:"pose,omitempty"`
	Index int    `json:"index"`
	fired bool
	timer *time.Timer
}

func buildTimeline(rm string, orient, d int) ([]*TLEvent, int, int) {
	t := 100
	ev := []*TLEvent{}
	for i := 0; i < len(rm); i++ {
		ch := rm[i]
		no := orientationAfter(ch, orient)
		if no != orient {
			p := "NORMAL"
			if no == INVERT {
				p = "INVERT"
			}
			sw, pw := sliderWait(d), pairWait(d)
			ev = append(ev, &TLEvent{At: t + 3*sw + pw, Dur: pw, Kind: "pose", Pose: p})
			t += 5*sw + 2*pw
			orient = no
		}
		f := string(unicode.ToUpper(rune(ch)))
		mv := f
		if unicode.IsLower(rune(ch)) {
			mv = f + "'"
		}
		ev = append(ev, &TLEvent{At: t, Dur: spinWait(d), Kind: "move", Move: mv})
		t += 2*spinWait(d) + 2*slider1Wait(d)
	}
	return ev, orient, t
}

func chunkMoves(s string) []string {
	out := []string{}
	for i := 0; i < len(s); i += moveBuf {
		j := i + moveBuf
		if j > len(s) {
			j = len(s)
		}
		out = append(out, s[i:j])
	}
	return out
}

// ---------------------------------------------------------------- Event (like threading.Event)
type Event struct {
	mu  sync.Mutex
	ch  chan struct{}
	set bool
}

func NewEvent(set bool) *Event {
	e := &Event{ch: make(chan struct{})}
	if set {
		e.Set()
	}
	return e
}
func (e *Event) Set() {
	e.mu.Lock()
	if !e.set {
		e.set = true
		close(e.ch)
	}
	e.mu.Unlock()
}
func (e *Event) Clear() {
	e.mu.Lock()
	if e.set {
		e.set = false
		e.ch = make(chan struct{})
	}
	e.mu.Unlock()
}
func (e *Event) IsSet() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.set }
func (e *Event) Wait(d time.Duration) bool {
	e.mu.Lock()
	ch := e.ch
	e.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

// ---------------------------------------------------------------- link
type Link struct {
	mu       sync.Mutex
	port     string
	rw       io.ReadWriteCloser
	sim      *Simulator
	onLine   func(string)
	onStatus func(bool, string)
	onDrop   func(port, reason string) // the port vanished / failed while connected
}

func (l *Link) Connected() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.rw != nil || l.sim != nil }
func (l *Link) Port() string    { l.mu.Lock(); defer l.mu.Unlock(); return l.port }

func (l *Link) Connect(port string) error {
	l.Disconnect()
	if port == "SIMULATOR" {
		l.mu.Lock()
		l.sim = &Simulator{out: l.onLine}
		l.port = port
		l.mu.Unlock()
		l.onStatus(true, port)
		l.sim.boot()
		return nil
	}
	rw, err := openSerial(port, 9600)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.rw = rw
	l.port = port
	l.mu.Unlock()
	go l.reader(rw, port)
	l.onStatus(true, port)
	return nil
}

func (l *Link) Disconnect() {
	l.mu.Lock()
	rw, port := l.rw, l.port
	l.rw, l.sim, l.port = nil, nil, ""
	l.mu.Unlock()
	if rw != nil {
		rw.Close()
	}
	if port != "" {
		l.onStatus(false, port)
	}
}

func (l *Link) Send(line string) error {
	l.mu.Lock()
	sim, rw := l.sim, l.rw
	l.mu.Unlock()
	if sim != nil {
		sim.receive(line)
		return nil
	}
	if rw == nil {
		return errors.New("Robot not connected")
	}
	_, err := rw.Write([]byte(line + "\n"))
	return err
}

func (l *Link) reader(rw io.ReadWriteCloser, port string) {
	sc := bufio.NewScanner(rw)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s != "" {
			l.onLine(s)
		}
	}
	l.mu.Lock()
	still := l.rw == rw // false when the user disconnected on purpose
	if still {
		l.rw = nil
		l.port = ""
	}
	l.mu.Unlock()
	if still {
		rw.Close()
		reason := port + " closed"
		if err := sc.Err(); err != nil {
			reason = friendlySerialError(port, err)
		}
		l.onStatus(false, port)
		if l.onDrop != nil {
			l.onDrop(port, reason)
		}
	}
}

// ---------------------------------------------------------------- simulator
type Simulator struct {
	mu     sync.Mutex
	out    func(string)
	busy   bool
	cancel chan struct{}
	state  map[byte]byte
	glide  bool
}

func (s *Simulator) later(msg string, d time.Duration) {
	time.AfterFunc(d, func() { s.out(msg) })
}
func (s *Simulator) boot() {
	s.later("Listening for API commands, type 'HELP' for list of commands.", 200*time.Millisecond)
}
func (s *Simulator) receive(line string) {
	parts := strings.Split(strings.TrimSpace(line), " ")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch parts[0] {
	case "PING":
		s.later("PONG", 10*time.Millisecond)
	case "FOLLOW":
		s.later("OK FOLLOW 60", 10*time.Millisecond)
	case "GRIP":
		g := "40"
		if len(parts) == 2 {
			g = parts[1]
		}
		s.later("OK GRIP "+g, 10*time.Millisecond)
	case "GLIDE":
		if len(parts) == 2 {
			s.glide = parts[1] != "0"
		}
		g := "0"
		if s.glide {
			g = "1"
		}
		s.later("OK GLIDE "+g, 10*time.Millisecond)
	case "STATUS":
		if len(parts) == 2 && len(parts[1]) == 1 {
			st := byte('C')
			if v, ok := s.state[parts[1][0]]; ok {
				st = v
			}
			s.later(string(st), 10*time.Millisecond)
			return
		}
		if s.busy {
			s.later("BUSY", 10*time.Millisecond)
		} else {
			s.later("IDLE", 10*time.Millisecond)
		}
	case "SEQ":
		if len(parts) != 2 {
			s.later("ERR args", 10*time.Millisecond)
			return
		}
		if parts[1] == "C" {
			if s.cancel != nil {
				close(s.cancel)
				s.cancel = nil
			}
			s.busy = false
			s.later("OK", 10*time.Millisecond)
			s.later("IDLE", 12*time.Millisecond)
			return
		}
		if s.busy {
			s.later("ERR busy", 10*time.Millisecond)
			return
		}
		if s.state == nil {
			s.state = map[byte]byte{}
		}
		for _, p := range parseSeq(parts[1]) {
			s.state[p[0]] = p[1]
		}
		total := 100
		num := ""
		for _, c := range parts[1] + "x" {
			if c >= '0' && c <= '9' {
				num += string(c)
			} else if num != "" {
				n, _ := strconv.Atoi(num)
				total += n
				num = ""
			}
		}
		s.run(total)
	case "MOVE":
		if len(parts) != 4 {
			s.later("ERR args", 10*time.Millisecond)
			return
		}
		if s.busy {
			s.later("ERR busy", 10*time.Millisecond)
			return
		}
		d, _ := strconv.Atoi(parts[1])
		o, _ := strconv.Atoi(parts[2])
		mv := parts[3]
		if len(mv) > 63 {
			mv = mv[:63]
		}
		_, _, total := buildTimeline(mv, o, d)
		s.run(total)
	default:
		s.later("ERR cmd: "+parts[0], 10*time.Millisecond)
	}
}
func (s *Simulator) run(totalMs int) {
	s.busy = true
	c := make(chan struct{})
	s.cancel = c
	s.later("BUSY", 5*time.Millisecond)
	s.later("OK", 6*time.Millisecond)
	go func() {
		select {
		case <-c:
		case <-time.After(time.Duration(totalMs) * time.Millisecond):
			s.mu.Lock()
			s.busy = false
			s.cancel = nil
			s.mu.Unlock()
			s.out("IDLE")
		}
	}()
}

// ---------------------------------------------------------------- queue
type RobotQueue struct {
	link        *Link
	emit        func(map[string]any)
	mu          sync.Mutex
	State       string
	idle        *Event
	busySeen    *Event
	Orientation int
	aborted     *Event
	sliders     map[byte]byte // last known slider states (F B R L)
	lowPower    bool          // servos on USB power: hold at C instead of L
	calibFW     bool          // Arduino runs the calibration firmware (CALIBRATE true)
	statusCh    chan byte
	glideCh     chan bool
	glide       bool // firmware supports GLIDE (paired spinners move in sync)
}

func NewRobotQueue(l *Link, emit func(map[string]any)) *RobotQueue {
	return &RobotQueue{link: l, emit: emit, State: "IDLE", idle: NewEvent(true), busySeen: NewEvent(false),
		aborted: NewEvent(false), sliders: map[byte]byte{}, statusCh: make(chan byte, 4),
		glideCh: make(chan bool, 2)}
}

// calibration firmware prints e.g. "Selected Servo: 0 (RIGHT_SPINNER) | Mode: CENTER ..."
func isCalibrationOutput(line string) bool {
	return strings.HasPrefix(line, "Selected Servo:") || strings.HasPrefix(line, "Servo calibration tool") ||
		strings.HasPrefix(line, "Writing all calibrations") || strings.HasPrefix(line, "Writing calibration for")
}

const calibFWMsg = "The Arduino is running the CALIBRATION firmware (CALIBRATE true in Config.h): it ignores " +
	"the app and would move servos at random from its commands. Set #define CALIBRATE false, upload again, " +
	"then Connect."

func (q *RobotQueue) CalibrationFirmware() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.calibFW }

func (q *RobotQueue) OnLine(line string) {
	if isCalibrationOutput(line) {
		q.mu.Lock()
		first := !q.calibFW
		q.calibFW = true
		q.mu.Unlock()
		if first {
			q.emit(map[string]any{"type": "log", "level": "error", "msg": calibFWMsg})
		}
		q.busySeen.Set() // wake anything waiting for BUSY
		q.idle.Set()
		return
	}
	if strings.HasPrefix(line, "Listening for API commands") {
		q.mu.Lock()
		q.calibFW = false
		q.mu.Unlock()
	}
	if strings.HasPrefix(line, "OK GRIP") || line == "ERR cmd: GRIP" ||
		strings.HasPrefix(line, "OK FOLLOW") || line == "ERR cmd: FOLLOW" {
		return // answers to "GRIP <us>" / "FOLLOW <pct>"
	}
	if strings.HasPrefix(line, "OK GLIDE") || line == "ERR cmd: GLIDE" {
		select { // reply to "GLIDE 1" (stock firmware: unknown command)
		case q.glideCh <- strings.HasPrefix(line, "OK GLIDE 1"):
		default:
		}
		return
	}
	if len(line) == 1 && strings.Contains("RCLrlH", line) {
		select { // reply to "STATUS <servo>"
		case q.statusCh <- line[0]:
		default:
		}
		return
	}
	switch {
	case line == "IDLE" || line == "BUSY":
		q.mu.Lock()
		q.State = line
		q.mu.Unlock()
		if line == "IDLE" {
			q.idle.Set()
		} else {
			q.idle.Clear()
			q.busySeen.Set()
		}
		q.emit(map[string]any{"type": "robot_state", "state": line})
	case strings.HasPrefix(line, "ERR"):
		q.emit(map[string]any{"type": "log", "level": "error", "msg": "Arduino: " + line})
		q.busySeen.Set()
		q.idle.Set()
	default:
		q.emit(map[string]any{"type": "log", "level": "info", "msg": "Arduino: " + line})
	}
}

func (q *RobotQueue) GetState() string { q.mu.Lock(); defer q.mu.Unlock(); return q.State }

func (q *RobotQueue) GetOrientation() int  { q.mu.Lock(); defer q.mu.Unlock(); return q.Orientation }
func (q *RobotQueue) SetOrientation(o int) { q.mu.Lock(); q.Orientation = o; q.mu.Unlock() }

func (q *RobotQueue) Start(cmd string) (time.Time, error) { return q.start(cmd, true) }

// RunQuiet sends a command without logging it (keep-alive).
func (q *RobotQueue) RunQuiet(cmd string) error {
	if _, err := q.start(cmd, false); err != nil {
		return err
	}
	if !q.idle.Wait(10 * time.Second) {
		return errors.New("Robot timed out")
	}
	return nil
}

func (q *RobotQueue) start(cmd string, logIt bool) (time.Time, error) {
	if q.aborted.IsSet() {
		return time.Time{}, errors.New("aborted")
	}
	if q.CalibrationFirmware() {
		return time.Time{}, errors.New(calibFWMsg)
	}
	q.idle.Wait(120 * time.Second)
	q.busySeen.Clear()
	q.idle.Clear()
	q.mu.Lock()
	low := q.lowPower
	glide := q.glide
	q.mu.Unlock()
	if low && strings.HasPrefix(cmd, "SEQ ") && cmd != "SEQ C" {
		cmd = "SEQ " + softenHolds(cmd[4:])
	} else if !glide && strings.HasPrefix(cmd, "SEQ ") {
		cmd = "SEQ " + noHardGrip(cmd[4:]) // stock firmware has no H state
	}
	if logIt {
		q.emit(map[string]any{"type": "log", "level": "tx", "msg": cmd})
	}
	if err := q.link.Send(cmd); err != nil {
		q.idle.Set()
		return time.Time{}, err
	}
	if strings.HasPrefix(cmd, "SEQ ") && cmd != "SEQ C" {
		q.mu.Lock()
		for _, p := range parseSeq(cmd[4:]) {
			if strings.IndexByte("FBRL", p[0]) >= 0 {
				st := p[1]
				if st == 'T' {
					st = 'C' // a following slider ends touching
				}
				q.sliders[p[0]] = st
			}
		}
		q.mu.Unlock()
	}
	if !q.busySeen.Wait(5 * time.Second) {
		q.idle.Set()
		return time.Time{}, errors.New("Robot did not answer " + cmd + " (no BUSY within 5 s). " +
			"Check that the normal firmware is running (CALIBRATE false in Config.h), the right COM port " +
			"is selected, and the Arduino IDE Serial Monitor is closed.")
	}
	if q.CalibrationFirmware() {
		q.idle.Set()
		return time.Time{}, errors.New(calibFWMsg)
	}
	return time.Now(), nil
}

func (q *RobotQueue) Run(cmd string) error {
	if _, err := q.Start(cmd); err != nil {
		return err
	}
	if !q.idle.Wait(120 * time.Second) {
		return errors.New("Robot timed out")
	}
	if q.aborted.IsSet() {
		return errors.New("aborted")
	}
	return nil
}

func (q *RobotQueue) WaitIdle(d time.Duration) bool { return q.idle.Wait(d) }

// SetLowPower switches low-power mode (holds at C, for servos powered from USB).
func (q *RobotQueue) SetLowPower(on bool) { q.mu.Lock(); q.lowPower = on; q.mu.Unlock() }

// softenHolds turns every slider clamp (L) into a light hold (C); delays and spinner
// states are kept. Used in low-power mode so the servos draw far less current.
// noHardGrip adapts a sequence for the stock firmware: no hard clamp (H -> L) and no
// following sliders (T -> R, released as in the original firmware).
func noHardGrip(seq string) string {
	b := []byte(seq)
	for i := 0; i+1 < len(b); {
		if b[i] >= '0' && b[i] <= '9' {
			i++
			continue
		}
		if strings.IndexByte("FBRL", b[i]) >= 0 && b[i+1] == 'H' {
			b[i+1] = 'L'
		}
		if strings.IndexByte("FBRL", b[i]) >= 0 && b[i+1] == 'T' {
			b[i+1] = 'R'
		}
		i += 2
	}
	return string(b)
}

func softenHolds(seq string) string {
	var b strings.Builder
	for i := 0; i < len(seq); {
		if seq[i] >= '0' && seq[i] <= '9' {
			b.WriteByte(seq[i])
			i++
			continue
		}
		if i+1 < len(seq) {
			s, st := seq[i], seq[i+1]
			if strings.IndexByte("FBRL", s) >= 0 && (st == 'L' || st == 'H') {
				st = 'C'
			}
			b.WriteByte(s)
			b.WriteByte(st)
		} else {
			b.WriteByte(seq[i])
		}
		i += 2
	}
	return b.String()
}

// SliderStates returns a copy of the last known slider states.
func (q *RobotQueue) SliderStates() map[byte]byte {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := map[byte]byte{}
	for k, v := range q.sliders {
		m[k] = v
	}
	return m
}

// QuerySliders asks the firmware ("STATUS F" ...) where each slider is.
func (q *RobotQueue) QuerySliders() {
	if q.CalibrationFirmware() {
		return
	}
	for _, s := range []byte("FBRL") {
		for len(q.statusCh) > 0 {
			<-q.statusCh
		}
		if q.link.Send("STATUS "+string(s)) != nil {
			return
		}
		select {
		case st := <-q.statusCh:
			q.mu.Lock()
			q.sliders[s] = st
			q.mu.Unlock()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

// EnableGlide turns on the firmware's synced spinner glide ("GLIDE 1"); false when the
// firmware is the stock one (no GLIDE command).
func (q *RobotQueue) EnableGlide() bool {
	if q.CalibrationFirmware() {
		return false
	}
	for len(q.glideCh) > 0 {
		<-q.glideCh
	}
	ok := false
	if q.link.Send("GLIDE 1") == nil {
		select {
		case ok = <-q.glideCh:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	q.mu.Lock()
	q.glide = ok
	q.mu.Unlock()
	return ok
}

// SetGrip sends how much harder than L the H clamp presses, and how far following
// sliders back off at 45 deg (GLIDE firmware only).
func (q *RobotQueue) SetGrip(us, followPct int) {
	if q.Glide() {
		q.link.Send(fmt.Sprintf("GRIP %d", us))
		time.Sleep(50 * time.Millisecond)
		q.link.Send(fmt.Sprintf("FOLLOW %d", followPct))
	}
}

func (q *RobotQueue) Glide() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.glide }

func (q *RobotQueue) Abort() {
	q.mu.Lock()
	q.sliders = map[byte]byte{} // unknown after an abort
	q.mu.Unlock()
	q.aborted.Set()
	q.link.Send("SEQ C")
	q.idle.Set()
	q.busySeen.Set()
}
func (q *RobotQueue) ClearAbort() { q.aborted.Clear() }
