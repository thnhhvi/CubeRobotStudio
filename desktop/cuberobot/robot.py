"""
Robot link (Arduino Nano over USB serial) and command builder.

Speaks the firmware API from Yonni123/RubiksCubeRobot (Arduino/API.cpp):
    PING -> PONG
    STATUS -> IDLE | BUSY
    SEQ <servo-state string with delays> | SEQ C (abort)
    MOVE <delay_ms> <orientation 0|1> <moves e.g. FBfUU>
The firmware prints BUSY when a SEQ/MOVE starts and IDLE when it ends.
"""
from __future__ import annotations

import queue
import threading
import time

try:
    import serial  # pyserial
    import serial.tools.list_ports
except Exception:  # pragma: no cover - optional at import time
    serial = None

BAUD = 9600
MOVE_BUF = 60        # firmware MOVE_BUFFER_SIZE is 64 (incl. terminator)
SEQ_BUF = 250        # firmware SEQUENCE_BUFFER_SIZE is 256 (incl. "SEQ ")

NORMAL, INVERT = 0, 1

OPEN_SEQ = "RRLRFRBR"
CLOSE_SEQ = "RCLCFCBC"


# ---------------------------------------------------------------------------
# notation helpers
# ---------------------------------------------------------------------------
def to_robot_moves(moves: list[str]) -> str:
    """['F', "U'", 'R2'] -> 'FuRR' (firmware single-char notation)."""
    out = []
    for mv in moves:
        f = mv[0]
        if mv.endswith("'"):
            out.append(f.lower())
        elif mv.endswith("2"):
            out.append(f + f)
        else:
            out.append(f)
    return "".join(out)


def orientation_after(ch: str, cur: int) -> int:
    c = ch.upper()
    if c in "UD":
        return INVERT
    if c in "LR":
        return NORMAL
    return cur


def rotate_cube_seq(new_orient: int, d: int) -> str:
    """Same servo sequence the firmware uses in SequenceManager::rotateCube."""
    pre = "fRbL" if new_orient == INVERT else "fLbR"
    return (f"RLLLFRBR{d}{pre}{d}FLBL{d}RRLR{d}fCfCbCbC{d}RCLC{d}FCBC")


# Timing model of the firmware (see SequenceManager.cpp)
FACE_DELAYS = 4          # fR d FR d fCfC d FC d
FLIP_DELAYS = 6          # rotateCube
START_MS = 100           # nextMoveAt = millis()+100


def build_timeline(robot_moves: str, orient: int, d: int):
    """Return (events, end_orientation, total_ms). Event times relative to BUSY."""
    t = START_MS
    ev = []
    for ch in robot_moves:
        new_o = orientation_after(ch, orient)
        if new_o != orient:
            # the cube physically rolls during the 5th delay of rotateCube
            ev.append({"at": t + 4 * d, "dur": d, "kind": "pose",
                       "pose": "INVERT" if new_o == INVERT else "NORMAL"})
            t += FLIP_DELAYS * d
            orient = new_o
        f = ch.upper()
        mv = f if ch.isupper() else f + "'"
        ev.append({"at": t, "dur": d, "kind": "move", "move": mv})
        t += FACE_DELAYS * d
    return ev, orient, t


def chunk_moves(robot_moves: str, size: int = MOVE_BUF):
    return [robot_moves[i:i + size] for i in range(0, len(robot_moves), size)]


# ---------------------------------------------------------------------------
# Serial link
# ---------------------------------------------------------------------------
class SerialLink:
    """Thread-based line reader/writer. Calls on_line(str) for each line."""

    def __init__(self, on_line, on_status):
        self.on_line = on_line
        self.on_status = on_status
        self.ser = None
        self.port = None
        self._stop = threading.Event()
        self._lock = threading.Lock()
        self.sim = None

    @staticmethod
    def list_ports():
        ports = []
        if serial is not None:
            for p in serial.tools.list_ports.comports():
                desc = p.description or ""
                ports.append({"device": p.device, "description": desc,
                              "arduino": any(k in (desc + str(p.hwid)).lower()
                                             for k in ("arduino", "ch340", "ch341", "ftdi",
                                                       "usb-serial", "usb serial", "2341:", "1a86:"))})
        ports.append({"device": "SIMULATOR", "description": "Built-in robot simulator",
                      "arduino": False})
        return ports

    @property
    def connected(self):
        return self.ser is not None or self.sim is not None

    def connect(self, port: str):
        self.disconnect()
        self._stop.clear()
        if port == "SIMULATOR":
            self.sim = SimulatedArduino(self._feed)
            self.port = port
            self.on_status(True, port)
            self.sim.boot()
            return
        if serial is None:
            raise RuntimeError("pyserial is not installed")
        self.ser = serial.Serial(port, BAUD, timeout=0.1)
        self.port = port
        threading.Thread(target=self._reader, daemon=True).start()
        self.on_status(True, port)

    def disconnect(self):
        self._stop.set()
        if self.ser is not None:
            try:
                self.ser.close()
            except Exception:
                pass
        self.ser = None
        self.sim = None
        if self.port:
            self.on_status(False, self.port)
        self.port = None

    def send(self, line: str):
        with self._lock:
            if self.sim is not None:
                self.sim.receive(line)
                return
            if self.ser is None:
                raise RuntimeError("Robot not connected")
            self.ser.write((line + "\n").encode("ascii"))
            self.ser.flush()

    def _feed(self, line):
        self.on_line(line)

    def _reader(self):
        buf = b""
        while not self._stop.is_set():
            try:
                data = self.ser.read(256)
            except Exception:
                self.on_status(False, self.port)
                self.ser = None
                return
            if not data:
                continue
            buf += data
            while b"\n" in buf:
                line, buf = buf.split(b"\n", 1)
                s = line.decode("ascii", "replace").strip()
                if s:
                    self.on_line(s)


class SimulatedArduino:
    """Mimics the firmware's API/timing so the apps can be tried without hardware."""

    def __init__(self, out):
        self.out = out
        self.busy = False
        self.cancel = threading.Event()

    def boot(self):
        threading.Timer(0.2, lambda: self.out(
            "Listening for API commands, type 'HELP' for list of commands.")).start()

    def receive(self, line):
        parts = line.strip().split(" ")
        cmd = parts[0]
        if cmd == "PING":
            self._later("PONG")
        elif cmd == "STATUS":
            self._later("BUSY" if self.busy else "IDLE")
        elif cmd == "SEQ":
            if len(parts) != 2:
                return self._later("ERR args")
            if parts[1] == "C":
                self.cancel.set()
                self.busy = False
                self._later("OK")
                self._later("IDLE")
                return
            if self.busy:
                return self._later("ERR busy")
            total = 100 + sum(int(x) for x in _numbers(parts[1]))
            self._run(total)
        elif cmd == "MOVE":
            if len(parts) != 4:
                return self._later("ERR args")
            if self.busy:
                return self._later("ERR busy")
            d, o, mv = int(parts[1]), int(parts[2]), parts[3][:63]
            _, _, total = build_timeline(mv, o, d)
            self._run(total)
        else:
            self._later(f"ERR cmd: {cmd}")

    def _later(self, s, delay=0.01):
        threading.Timer(delay, lambda: self.out(s)).start()

    def _run(self, total_ms):
        self.busy = True
        self.cancel.clear()
        self._later("BUSY", 0.005)
        self._later("OK", 0.006)

        def fin():
            if not self.cancel.wait(total_ms / 1000.0):
                self.busy = False
                self.out("IDLE")
        threading.Thread(target=fin, daemon=True).start()


def _numbers(s):
    num = ""
    for ch in s:
        if ch.isdigit():
            num += ch
        elif num:
            yield num
            num = ""
    if num:
        yield num


# ---------------------------------------------------------------------------
# Command queue that waits for the firmware to go IDLE between jobs
# ---------------------------------------------------------------------------
class RobotQueue:
    """Serialises SEQ/MOVE jobs; each job waits for BUSY->IDLE."""

    def __init__(self, link: SerialLink, emit):
        self.link = link
        self.emit = emit
        self.state = "IDLE"
        self._idle = threading.Event()
        self._idle.set()
        self._busy_seen = threading.Event()
        self.orientation = NORMAL
        self.aborted = threading.Event()

    def on_line(self, line: str):
        if line in ("IDLE", "BUSY"):
            self.state = line
            if line == "IDLE":
                self._idle.set()
            else:
                self._idle.clear()
                self._busy_seen.set()
            self.emit({"type": "robot_state", "state": line})
        elif line.startswith("ERR"):
            self.emit({"type": "log", "level": "error", "msg": f"Arduino: {line}"})
            self._busy_seen.set()  # unblock waiters; job failed
            self._idle.set()
        else:
            self.emit({"type": "log", "level": "info", "msg": f"Arduino: {line}"})

    def run(self, cmd: str, timeout: float = 120.0) -> float:
        """Send a command and block until the robot is IDLE again.
        Returns the time BUSY was observed (time.time())."""
        if self.aborted.is_set():
            raise RuntimeError("aborted")
        self._idle.wait(timeout)
        self._busy_seen.clear()
        self._idle.clear()
        self.emit({"type": "log", "level": "tx", "msg": cmd})
        self.link.send(cmd)
        if not self._busy_seen.wait(5.0):
            self._idle.set()
            raise RuntimeError(f"Robot did not start: {cmd}")
        t_busy = time.time()
        if not self._idle.wait(timeout):
            raise RuntimeError("Robot timed out")
        if self.aborted.is_set():
            raise RuntimeError("aborted")
        return t_busy

    def run_async_start(self, cmd: str) -> float:
        """Send and return when BUSY is seen (caller waits for idle)."""
        self._idle.wait(120)
        self._busy_seen.clear()
        self._idle.clear()
        self.emit({"type": "log", "level": "tx", "msg": cmd})
        self.link.send(cmd)
        if not self._busy_seen.wait(5.0):
            self._idle.set()
            raise RuntimeError(f"Robot did not start: {cmd}")
        return time.time()

    def wait_idle(self, timeout=300):
        return self._idle.wait(timeout)

    def abort(self):
        self.aborted.set()
        try:
            self.link.send("SEQ C")
        finally:
            self._idle.set()
            self._busy_seen.set()

    def clear_abort(self):
        self.aborted.clear()
