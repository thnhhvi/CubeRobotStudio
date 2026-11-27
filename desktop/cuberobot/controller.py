"""Application logic shared by the HTTP API: robot, scanning, solving, animation."""
from __future__ import annotations

import json
import os
import queue
import random
import threading
import time
import traceback

from . import cube, robot, scan, twophase

SPEED_MIN_MS, SPEED_MAX_MS = 120, 500


def config_dir():
    d = os.path.join(os.path.expanduser("~"), ".cuberobot")
    os.makedirs(d, exist_ok=True)
    return d


class Bus:
    """Fan-out of events to SSE subscribers (laptop UI + phone)."""

    def __init__(self):
        self.subs: list[queue.Queue] = []
        self.lock = threading.Lock()

    def subscribe(self):
        q = queue.Queue(maxsize=2000)
        with self.lock:
            self.subs.append(q)
        return q

    def unsubscribe(self, q):
        with self.lock:
            if q in self.subs:
                self.subs.remove(q)

    def emit(self, ev: dict):
        ev.setdefault("ts", time.time())
        with self.lock:
            for q in list(self.subs):
                try:
                    q.put_nowait(ev)
                except queue.Full:
                    pass


class Controller:
    def __init__(self):
        self.bus = Bus()
        self.cfg_dir = config_dir()
        self.settings = {"delay": 210, "front": "R", "top": "W", "auto_capture": True,
                         "capture_settle_ms": 500, "scan_delay_min": 200,
                         "solve_timeout": 3.0, "return_home": True}
        self._load_settings()
        self.plan = scan.load_plan(self.cfg_dir)
        self.colors = cube.solved_colors(self.settings["front"], self.settings["top"])
        self.state_known = True
        self.solution: list[str] | None = None
        self.solution_info: dict = {}
        self.solver_ready = False
        self.solver = None
        self.busy_task: str | None = None
        self.pose = "NORMAL"
        self.scan = None  # dict while scanning
        self.thumbs: dict = {}
        self.scanned: list = []
        self.phones: dict = {}
        self.warnings: list[str] = []
        self.link = robot.SerialLink(self._on_line, self._on_link)
        self.rq = robot.RobotQueue(self.link, self.bus.emit)
        self._capture_evt = threading.Event()
        self._captured = None
        self._timers: list[threading.Timer] = []
        self._fire_lock = threading.Lock()
        threading.Thread(target=self._init_solver, daemon=True).start()

    # ------------------------------------------------------------------ setup
    def _load_settings(self):
        p = os.path.join(self.cfg_dir, "settings.json")
        if os.path.exists(p):
            try:
                with open(p, "r", encoding="utf-8") as f:
                    self.settings.update(json.load(f))
            except Exception:
                pass

    def _save_settings(self):
        with open(os.path.join(self.cfg_dir, "settings.json"), "w", encoding="utf-8") as f:
            json.dump(self.settings, f, indent=2)

    def _init_solver(self):
        try:
            tables = twophase.Tables(cache_dir=self.cfg_dir,
                                     log=lambda m: self.log(m))
            self.solver = twophase.Solver(tables)
            self.solver_ready = True
            self.log("Solver ready")
            self.push_state()
        except Exception as e:  # pragma: no cover
            self.log(f"Solver init failed: {e}", "error")

    def log(self, msg, level="info"):
        self.bus.emit({"type": "log", "level": level, "msg": msg})

    # ------------------------------------------------------------------ state
    def snapshot(self):
        order = scan.scan_order(self.plan, self.settings["front"], self.settings["top"])
        return {
            "type": "state",
            "colors": self.colors,
            "state_known": self.state_known,
            "settings": self.settings,
            "speed_range": [SPEED_MIN_MS, SPEED_MAX_MS],
            "robot": {"connected": self.link.connected, "port": self.link.port,
                      "state": self.rq.state, "orientation": self.rq.orientation},
            "pose": self.pose,
            "solver_ready": self.solver_ready,
            "solution": self.solution,
            "solution_info": self.solution_info,
            "busy_task": self.busy_task,
            "scan": self._scan_public(),
            "scan_order": order,
            "centers": cube.center_colors(self.settings["front"], self.settings["top"]),
            "thumbs": self.thumbs,
            "scanned": self.scanned,
            "phones": list(self.phones.values()),
            "warnings": self.warnings,
        }

    def _scan_public(self):
        if not self.scan:
            return None
        s = self.scan
        return {k: s.get(k) for k in ("index", "total", "color", "phase", "step_id", "done",
                                      "view", "cells")}

    def push_state(self):
        self.bus.emit(self.snapshot())

    # ------------------------------------------------------------------ robot link
    def _on_line(self, line):
        self.rq.on_line(line)
        if line in ("IDLE", "BUSY"):
            self.push_state()

    def _on_link(self, up, port):
        self.log(f"Robot {'connected on' if up else 'disconnected from'} {port}")
        self.push_state()

    def connect(self, port):
        self.link.connect(port)
        if port != "SIMULATOR":
            time.sleep(2.0)  # Nano resets when the port opens
        self.rq.state = "IDLE"
        self.rq._idle.set()
        try:
            self.link.send("PING")
        except Exception:
            pass
        self.push_state()

    def disconnect(self):
        self.link.disconnect()
        self.push_state()

    def _require_robot(self):
        if not self.link.connected:
            raise RuntimeError("Robot is not connected")

    def _start_task(self, name, fn, *a):
        if self.busy_task:
            raise RuntimeError(f"Busy: {self.busy_task}")
        self.busy_task = name
        self.rq.clear_abort()
        self.push_state()

        def run():
            try:
                fn(*a)
            except Exception as e:
                if str(e) != "aborted":
                    self.log(f"{name} failed: {e}", "error")
                    traceback.print_exc()
            finally:
                self.busy_task = None
                self.push_state()
        threading.Thread(target=run, daemon=True).start()

    def open_arms(self):
        self._require_robot()
        self._start_task("open", self.rq.run, "SEQ " + robot.OPEN_SEQ)

    def close_arms(self):
        self._require_robot()
        self._start_task("close", self.rq.run, "SEQ " + robot.CLOSE_SEQ)

    def abort(self):
        for t in self._timers:
            t.cancel()
        self._timers.clear()
        if self.scan:
            self.scan["done"] = True
            self._capture_evt.set()
        if self.link.connected:
            self.rq.abort()
        if self.busy_task in ("solve", "scramble"):
            self.state_known = False
            self.warnings = ["Aborted mid-sequence: cube state on screen may not match the real cube."]
        self.log("ABORT sent (SEQ C)", "error")
        self.push_state()

    def jog(self, seq):
        self._require_robot()
        seq = scan.fill(seq, max(self.settings["delay"], self.settings["scan_delay_min"]))
        self._start_task("jog", self.rq.run, "SEQ " + seq)

    # ------------------------------------------------------------------ settings
    def update_settings(self, data: dict):
        changed_orientation = False
        for k, v in data.items():
            if k == "delay":
                v = max(SPEED_MIN_MS, min(SPEED_MAX_MS, int(v)))
            if k in ("front", "top") and v != self.settings.get(k):
                changed_orientation = True
            self.settings[k] = v
        if changed_orientation:
            cube.center_colors(self.settings["front"], self.settings["top"])  # validates
            self.colors = cube.solved_colors(self.settings["front"], self.settings["top"])
            self.solution = None
            self.solution_info = {}
            self.thumbs = {}
            self.scanned = []
        self._save_settings()
        self.push_state()

    def set_colors(self, colors: str):
        if len(colors) != 54:
            raise ValueError("need 54 stickers")
        self.colors = colors
        self.state_known = True
        self.solution = None
        self.solution_info = {}
        self.warnings = []
        self.push_state()

    def reset(self):
        self.set_colors(cube.solved_colors(self.settings["front"], self.settings["top"]))

    # ------------------------------------------------------------------ solving
    def solve(self):
        if not self.solver_ready:
            raise RuntimeError("Solver is still initialising")
        facelets = cube.colors_to_facelets(self.colors)
        err = twophase.verify(facelets)
        if err:
            fixed = cube.auto_orient(self.colors, twophase.verify)
            if fixed and fixed[1] != (0,) * 6:
                self.colors = fixed[0]
                self.log("Auto-corrected rotated faces: " + ", ".join(
                    f"{cube.FACES[i]}x{q}" for i, q in enumerate(fixed[1]) if q))
                facelets = cube.colors_to_facelets(self.colors)
            else:
                self.solution = None
                self.solution_info = {"error": err}
                self.push_state()
                raise ValueError(err)
        sols = self.solver.solve(facelets, max_length=24,
                                 timeout=float(self.settings["solve_timeout"]))
        d = self.settings["delay"]
        best, best_ms = None, None
        for s in sols:
            _, _, ms = robot.build_timeline(robot.to_robot_moves(s), robot.NORMAL, d)
            if best_ms is None or ms < best_ms:
                best, best_ms = s, ms
        self.solution = best
        rm = robot.to_robot_moves(best)
        flips = sum(1 for e in robot.build_timeline(rm, robot.NORMAL, d)[0] if e["kind"] == "pose")
        self.solution_info = {"moves": len(best), "robot_moves": len(rm), "flips": flips,
                              "est_ms": best_ms, "candidates": len(sols),
                              "robot_string": rm, "shortest": min(len(s) for s in sols)}
        self.push_state()
        return best

    def execute_solution(self):
        self._require_robot()
        if not self.solution:
            self.solve()
        moves = list(self.solution or [])
        if not moves:
            raise RuntimeError("Cube is already solved")
        self._start_task("solve", self._run_moves, moves, True)

    def scramble(self, n=22):
        self._require_robot()
        moves = cube.random_scramble(n)
        self.log("Scramble: " + " ".join(moves))
        self._start_task("scramble", self._run_moves, moves, False)

    def _run_moves(self, moves, is_solve):
        d = int(self.settings["delay"])
        rm = robot.to_robot_moves(moves)
        self.bus.emit({"type": "run", "moves": moves, "solve": is_solve, "delay": d})
        idx = 0
        for chunk in robot.chunk_moves(rm):
            o = self.rq.orientation
            events, end_o, total = robot.build_timeline(chunk, o, d)
            t_busy = self.rq.run_async_start(f"MOVE {d} {o} {chunk}")
            self._schedule(events, t_busy, idx)
            idx += sum(1 for e in events if e["kind"] == "move")
            if not self.rq.wait_idle(total / 1000 + 30):
                raise RuntimeError("robot timeout")
            if self.rq.aborted.is_set():
                raise RuntimeError("aborted")
            self._flush_timers()
            self.rq.orientation = end_o
        if self.settings.get("return_home", True) and self.rq.orientation == robot.INVERT:
            # put the cube back the way it was loaded: chosen front + top
            seq = robot.rotate_cube_seq(robot.NORMAL, d)
            t_busy = self.rq.run_async_start("SEQ " + seq)
            self._schedule([{"at": 100 + 4 * d, "dur": d, "kind": "pose", "pose": "NORMAL"}],
                           t_busy, idx)
            self.rq.wait_idle(30)
            self._flush_timers()
            self.rq.orientation = robot.NORMAL
        if is_solve:
            self.solution = None
            self.solution_info = {"done": True}
        self.bus.emit({"type": "run_done", "solve": is_solve})

    def _schedule(self, events, t_busy, base_idx):
        k = base_idx
        for ev in events:
            ev = dict(ev)
            if ev["kind"] == "move":
                ev["index"] = k
                k += 1
            delay = max(0.0, t_busy + ev["at"] / 1000.0 - time.time())
            t = threading.Timer(delay, self._fire, args=(ev,))
            t.daemon = True
            ev["_timer"] = t
            self._timers.append(t)
            t.start()

    def _fire(self, ev):
        with self._fire_lock:
            if ev.get("_fired"):
                return
            ev["_fired"] = True
        ev = {k: v for k, v in ev.items() if not k.startswith("_")}
        if ev["kind"] == "move":
            self.colors = cube.apply_move(self.colors, ev["move"])
            self.bus.emit({"type": "move", "move": ev["move"], "dur": ev["dur"],
                           "index": ev["index"], "colors": self.colors})
        else:
            self.pose = ev["pose"]
            self.bus.emit({"type": "pose", "pose": ev["pose"], "dur": ev["dur"]})

    def _flush_timers(self):
        # robot reported IDLE: fire anything still pending right now
        for t in list(self._timers):
            t.cancel()
            self._fire(t.args[0])
        self._timers.clear()

    # ------------------------------------------------------------------ scanning
    def start_scan(self):
        if self.busy_task:
            raise RuntimeError(f"Busy: {self.busy_task}")
        front, top = self.settings["front"], self.settings["top"]
        steps = list(scan.walk(self.plan, front, top))
        self.scan = {"index": 0, "total": len(steps), "color": steps[0][4], "phase": "moving",
                     "step_id": steps[0][0]["id"], "done": False, "samples": {}, "view": [], "cells": [],
                     "logo": {}}
        self.thumbs = {}
        self.scanned = []
        self.warnings = []
        self.colors = "".join("?" if i % 9 != 4 else c for i, c in
                              enumerate(cube.solved_colors(front, top)))
        self.solution = None
        self.solution_info = {}
        self._start_task("scan", self._scan_run, steps)

    def _scan_run(self, steps):
        d = max(int(self.settings["delay"]), int(self.settings["scan_delay_min"]))
        use_robot = self.link.connected
        if not use_robot:
            self.log("Robot not connected: scanning in manual mode (turn the cube by hand)")
        centers = cube.center_colors(self.settings["front"], self.settings["top"])
        for i, (step, orient, view, cells, color) in enumerate(steps):
            s = self.scan
            s.update(index=i, color=color, phase="moving", step_id=step["id"], view=view,
                     cells=cells)
            self.push_state()
            if use_robot:
                self.rq.run("SEQ " + scan.fill(step["present"], d))
            s["phase"] = "capture"
            self._capture_evt.clear()
            self._captured = None
            self.bus.emit({"type": "scan_prompt", "index": i, "total": len(steps),
                           "color": color, "color_name": cube.COLOR_NAMES[color],
                           "auto": bool(self.settings["auto_capture"]),
                           "settle_ms": int(self.settings["capture_settle_ms"]),
                           "manual_hint": None if use_robot else self._manual_hint(view)})
            self.push_state()
            while not self._capture_evt.wait(0.25):
                if s.get("done"):
                    raise RuntimeError("aborted")
            if s.get("done"):
                raise RuntimeError("aborted")
            data = self._captured
            tiles = data["tiles"]
            for k in range(9):
                s["samples"][cells[k]] = tuple(int(v) for v in tiles[k])
            self.scanned.append(color)
            if data.get("thumb"):
                self.thumbs[color] = data["thumb"]
            logo = data.get("logo") or {}
            s["logo"][color] = logo
            if logo.get("detected") and color != "W":
                self.warnings.append(
                    f"A GAN logo was seen on the {cube.COLOR_NAMES[color]} centre - check the "
                    "front/top choice.")
            # provisional colours for live display
            prov = list(self.colors)
            for k in range(9):
                idx = cells[k]
                prov[idx] = color if idx % 9 == 4 else _nearest(tiles[k])
            self.colors = "".join(prov)
            s["phase"] = "reverting"
            self.push_state()
            if use_robot:
                self.rq.run("SEQ " + scan.fill(step["revert"], d))
        # classify all 54 together (balanced 9 per colour, centres from orientation)
        cols = cube.classify(self.scan["samples"], centers)
        fixed = cube.auto_orient(cols, twophase.verify)
        if fixed is None:
            self.colors = cols
            self.warnings.append("Scan is not a valid cube - tap stickers to fix them, then Solve.")
        else:
            if fixed[1] != (0,) * 6:
                self.log("Auto-corrected face rotation(s): " + ", ".join(
                    f"{cube.FACES[i]} {q * 90}deg" for i, q in enumerate(fixed[1]) if q))
            self.colors = fixed[0]
        self.state_known = True
        self.scan["done"] = True
        self.scan["phase"] = "done"
        self.push_state()
        self.bus.emit({"type": "scan_done"})
        self.scan = None
        if fixed is not None and self.solver_ready:
            try:
                self.solve()
            except Exception as e:
                self.log(f"Solve failed: {e}", "error")

    def _manual_hint(self, view):
        return "Hold the cube so that this face points at the camera"

    def capture(self, data: dict):
        if not self.scan or self.scan.get("phase") != "capture":
            raise RuntimeError("Not waiting for a capture")
        if len(data.get("tiles", [])) != 9:
            raise ValueError("need 9 tiles")
        self._captured = data
        self._capture_evt.set()

    def cancel_scan(self):
        if self.scan:
            self.scan["done"] = True
            self._capture_evt.set()
        if self.link.connected and self.rq.state == "BUSY":
            self.rq.abort()

    # ------------------------------------------------------------------ plan
    def set_plan(self, plan):
        list(scan.walk(plan, self.settings["front"], self.settings["top"]))  # validate
        self.plan = plan
        scan.save_plan(self.cfg_dir, plan)
        self.push_state()

    def reset_plan(self):
        import copy
        self.set_plan(copy.deepcopy(scan.DEFAULT_PLAN))

    # ------------------------------------------------------------------ phones
    def phone_hello(self, info):
        pid = info.get("id") or "phone"
        info["seen"] = time.time()
        self.phones[pid] = info
        self.push_state()


def _nearest(rgb):
    lab = cube.rgb_to_lab(rgb)
    return min(cube.DEFAULT_RGB, key=lambda c: cube._dist(lab, cube.rgb_to_lab(cube.DEFAULT_RGB[c])))
