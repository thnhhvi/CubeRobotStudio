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
	"time"
	"unicode"
)

const (
	NORMAL   = 0
	INVERT   = 1
	moveBuf  = 60
	seqBuf   = 250
	openSeq  = "RRLRFRBR"
	closeSeq = "RCLCFCBC"
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

func rotateCubeSeq(o, d int) string {
	pre := "fLbR"
	if o == INVERT {
		pre = "fRbL"
	}
	return fmt.Sprintf("RLLLFRBR%d%s%dFLBL%dRRLR%dfCfCbCbC%dRCLC%dFCBC", d, pre, d, d, d, d, d)
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
			ev = append(ev, &TLEvent{At: t + 4*d, Dur: d, Kind: "pose", Pose: p})
			t += 6 * d
			orient = no
		}
		f := string(unicode.ToUpper(rune(ch)))
		mv := f
		if unicode.IsLower(rune(ch)) {
			mv = f + "'"
		}
		ev = append(ev, &TLEvent{At: t, Dur: d, Kind: "move", Move: mv})
		t += 4 * d
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
	still := l.rw == rw
	if still {
		l.rw = nil
		l.port = ""
	}
	l.mu.Unlock()
	if still {
		l.onStatus(false, port)
	}
}

// ---------------------------------------------------------------- simulator
type Simulator struct {
	mu     sync.Mutex
	out    func(string)
	busy   bool
	cancel chan struct{}
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
	case "STATUS":
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
}

func NewRobotQueue(l *Link, emit func(map[string]any)) *RobotQueue {
	return &RobotQueue{link: l, emit: emit, State: "IDLE", idle: NewEvent(true), busySeen: NewEvent(false),
		aborted: NewEvent(false)}
}

func (q *RobotQueue) OnLine(line string) {
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

func (q *RobotQueue) Start(cmd string) (time.Time, error) {
	if q.aborted.IsSet() {
		return time.Time{}, errors.New("aborted")
	}
	q.idle.Wait(120 * time.Second)
	q.busySeen.Clear()
	q.idle.Clear()
	q.emit(map[string]any{"type": "log", "level": "tx", "msg": cmd})
	if err := q.link.Send(cmd); err != nil {
		q.idle.Set()
		return time.Time{}, err
	}
	if !q.busySeen.Wait(5 * time.Second) {
		q.idle.Set()
		return time.Time{}, errors.New("Robot did not start: " + cmd)
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

func (q *RobotQueue) Abort() {
	q.aborted.Set()
	q.link.Send("SEQ C")
	q.idle.Set()
	q.busySeen.Set()
}
func (q *RobotQueue) ClearAbort() { q.aborted.Clear() }
