"""
Physical simulation of the 4-arm robot, used to check the servo commands the app sends.

The simulated cube is 54 stickers in the robot frame. Servo commands are played back
group by group (commands between two delays run at the same time):

  * a spinner turning while its slider clamps (L or C) and its partner holds still
    turns that face's LAYER (all four arms must be holding); 45 deg twists are tracked,
  * two opposite spinners turning mirrored while both clamp and the other pair is
    released (R) ROLL the whole cube (45 deg steps allowed, e.g. scan lifts),
  * a slider may only clamp square to its face/layer (gripper angle - layer twist = 0/+-90),
  * no layer may turn while a layer on another axis is twisted (the cube would jam),
  * a group moves sliders OR spinners, never both, and never clamps one slider while
    releasing another; the next group only starts when this one is done: >= 400 ms after
    sliders, >= 300 ms after one spinner and >= 400 ms after two spinners (a roll),
  * H = slider clamped harder than L (used while the cube is flipped),
  * T = the non-carrying pair follows the rolling cube (only together with that roll,
    starting at C, grippers vertical),
  * after every group at least one opposite pair (F+B or R+L) holds the cube (L or C),
  * scanning: while the cube rolls the released arms stay vertical (C); in the scan pose
    nothing is twisted, all grippers are vertical, F/B hold at C and R/L are released (R).

Usage:
  python tools/check_robot_commands.py solve <seqs.json>   # from desktop-go DUMP_SEQ test
  python tools/check_robot_commands.py scan  <plan.json>   # [{id, present, revert}, ...]
"""
import json
import os
import re
import sys

import numpy as np

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "desktop"))
from cuberobot import cube  # noqa: E402

ANG = {"L": -90, "l": -45, "C": 0, "r": 45, "R": 90}
PAIR = {"f": "b", "b": "f", "r": "l", "l": "r"}
FACE = {"f": "F", "b": "B", "r": "R", "l": "L"}
# cube rotation produced by turning this arm's spinner clockwise (seen from that arm)
ROLL = {"f": "z", "b": "z'", "r": "x", "l": "x'"}
AXIS = {"f": (0, 0, 1), "b": (0, 0, -1), "r": (1, 0, 0), "l": (-1, 0, 0)}


class Robot:
    def __init__(self, colors, scan_rules=False):
        self.c = colors                      # physical stickers, robot frame
        self.sl = {k: "L" for k in "FBRL"}
        self.sp = {k: 0 for k in "fbrl"}
        self.tilt = {"z": 0, "x": 0}         # partial roll (deg, cw seen from f / r)
        self.twist = {}                      # partial layer twist by centre colour
        self.err = []
        self.scan_rules = scan_rules

    def clamped(self, arm):
        return self.sl[arm.upper()] in "LCH"     # T (following) does not hold the cube

    def run(self, seq, label):
        parts = re.split(r"(\d+)", seq)
        for gi in range(0, len(parts), 2):
            grp = parts[gi]
            wait = int(parts[gi + 1]) if gi + 1 < len(parts) else None
            spin, slide = {}, {}
            for s, st in re.findall(r"([FBRLfbrl])([LlCrRHT])", grp):
                (slide if s.isupper() else spin)[s] = st
            follow = [k for k, v in slide.items() if v == "T"]
            if follow:
                self.err.append(f"{label}: T (following sliders) is not used any more: '{grp}'")
            if spin and any(v != "T" for v in slide.values()):
                self.err.append(f"{label}: group '{grp}' moves sliders and spinners together")
            if follow:
                rolling = [k for k in spin if ANG[spin[k]] != self.sp[k]]
                if len(rolling) != 2 or any(self.sl[k] != "C" for k in follow) \
                        or any(PAIR[k.lower()].upper() not in follow for k in follow):
                    self.err.append(f"{label}: following sliders {follow} need to start at C "
                                    f"(touching), as a pair, while the other pair rolls the cube")
                if any(r.upper() in follow for r in rolling):
                    self.err.append(f"{label}: a following slider belongs to a rolling arm")
                if any(self.sp[k.lower()] != 0 for k in follow):
                    self.err.append(f"{label}: following arm gripper not vertical (no flat base)")
            depth = {"R": 0, "r": 0, "C": 1, "l": 1, "L": 2, "H": 3, "T": 1}
            clamps = [k for k, v in slide.items() if v != "T" and depth[v] > depth[self.sl[k]]]
            frees = [k for k, v in slide.items() if v != "T" and depth[v] < depth[self.sl[k]]]
            if clamps and frees:
                self.err.append(f"{label}: group '{grp}' clamps {clamps} and releases {frees} at once")
            need_sl = 400
            if slide and not spin and wait is not None and wait < need_sl:
                self.err.append(f"{label}: only {wait} ms after slider group '{grp}' (needs {need_sl})")
            # sliders first (the firmware writes them in the same instant; clamping onto a
            # gripper that also moves in this group is checked with the new angle)
            moved = {k: ANG[v] - self.sp[k] for k, v in spin.items() if ANG[v] != self.sp[k]}
            need = 400 if len(moved) > 1 else 300 if moved else 0
            if need and wait is not None and wait < need:
                self.err.append(f"{label}: only {wait} ms after spinner group '{grp}' (needs {need})")
            for s, st in slide.items():
                if st in "LH" and self.sl[s] not in "LH":
                    arm = s.lower()
                    ang = ANG[spin[arm]] if arm in spin else self.sp[arm]
                    if not self.square(arm, ang - self.twist.get(self.c[self.center(arm)], 0)):
                        self.err.append(f"{label}: {s} clamps a non-square face (gripper {ang})")
                self.sl[s] = st
            done = set()
            for k, d in moved.items():
                if k in done or not self.clamped(k):
                    continue
                p = PAIR[k]
                others = [o for o in "fbrl" if o not in (k, p)]
                if p in moved:                      # whole-cube roll
                    if moved[p] != -d or not self.clamped(p):
                        self.err.append(f"{label}: {k}/{p} do not mirror")
                    if any(self.clamped(o) for o in others):
                        self.err.append(f"{label}: cube rolled by {k} while the other pair clamps")
                    if self.scan_rules and any(self.sp[o] != 0 for o in others):
                        self.err.append(f"{label}: cube rolled while released grippers are not vertical")
                    self.roll(k, d)
                    done |= {k, p}
                else:                               # layer turn
                    if not all(self.clamped(o) for o in "fbrl"):
                        self.err.append(f"{label}: {k} turns a layer while the cube is not held")
                    if any(self.tilt.values()):
                        self.err.append(f"{label}: layer turn on a tilted cube")
                    col = self.c[self.center(k)]
                    axis_k = "z" if k in "fb" else "x"
                    for other, a in self.twist.items():
                        if a and other != col and self.axis_of(other) not in (axis_k, None) \
                                and self.axis_of(other) != axis_k:
                            self.err.append(f"{label}: {k} turns a layer while another axis is twisted")
                    t = self.twist.get(col, 0) + d
                    while t >= 90:
                        self.c = cube.apply_move(self.c, FACE[k]); t -= 90
                    while t <= -90:
                        self.c = cube.apply_move(self.c, FACE[k] + "'"); t += 90
                    self.twist[col] = t
                    if t % 45:
                        self.err.append(f"{label}: {k} leaves its layer at {t} deg")
                    done.add(k)
            for k, v in spin.items():
                self.sp[k] = ANG[v]
            for k in follow:            # a following slider ends touching
                self.sl[k] = "C"
            if grp and not ((self.clamped("f") and self.clamped("b")) or
                            (self.clamped("r") and self.clamped("l"))):
                self.err.append(f"{label}: after '{grp}' no opposite pair holds the cube {self.sl}")

    def center(self, arm):
        return 9 * cube.FACES.index(FACE[arm]) + 4

    def axis_of(self, col):
        """world axis ('z' f/b, 'x' r/l, 'y' u/d) of the layer whose centre has this colour"""
        i = [self.c[9 * j + 4] for j in range(6)].index(col)
        return {"F": "z", "B": "z", "R": "x", "L": "x", "U": "y", "D": "y"}[cube.FACES[i]]

    def square(self, arm, ang):
        ax = "z" if arm in "fb" else "x"
        other = "x" if ax == "z" else "z"
        if self.tilt[other]:
            return False                      # this face is tilted out of plane
        rel = ang - (self.tilt[ax] if arm in "fr" else -self.tilt[ax])
        return rel % 90 == 0

    def roll(self, arm, d):
        base = "f" if arm in "fb" else "r"
        dd = d if arm == base else -d
        ax = "z" if base == "f" else "x"
        self.tilt[ax] += dd
        while abs(self.tilt[ax]) >= 90:
            step = 90 if self.tilt[ax] > 0 else -90
            self.c = cube.apply_move(self.c, ROLL[base] if step > 0 else ROLL[base] + "'")
            self.tilt[ax] -= step

    def scan_pose(self, label):
        """face rolled straight up: nothing twisted or tilted, every gripper vertical (the
        clamped pair is a platform); one opposite pair clamped at L, the other released at R"""
        top = self.c[4]
        if any(self.tilt.values()) or any(a % 90 for a in self.twist.values()):
            self.err.append(f"{label}: cube or a layer is not square")
        pairs = [("F", "B"), ("R", "L")]
        ok = False
        for hold, free in (pairs, pairs[::-1]):
            if all(self.sl[k] == "L" for k in hold) and all(self.sl[k] == "R" for k in free) \
                    and all(self.sp[k.lower()] == 0 for k in "FBRL"):
                ok = True
        if not ok:
            self.err.append(f"{label}: scan pose sliders {self.sl} grippers {self.sp} "
                            "(want one pair at L, the other at R, all grippers vertical)")
        return top

    def camera_face_color(self):
        """Centre colour of the face the camera at the robot front (+x+z, raised) sees best."""
        cam = np.array([1, 1, 1]) / np.sqrt(3)
        best, col = -9, None
        for i, f in enumerate(cube.FACES):
            n = np.array(cube.FACE_FRAME[f][0], float)
            for ax, a in (("z", self.tilt["z"]), ("x", self.tilt["x"])):
                if a:
                    axis = np.array((0, 0, 1) if ax == "z" else (1, 0, 0), float)
                    t = np.radians(-a)                     # clockwise = negative right-hand
                    n = (n * np.cos(t) + np.cross(axis, n) * np.sin(t) + axis * axis.dot(n) * (1 - np.cos(t)))
            if n.dot(cam) > best:
                best, col = n.dot(cam), self.c[9 * i + 4]
        return col


def check_solve(path):
    runs = json.load(open(path))
    home = cube.solved_colors("R", "W")
    bad = 0
    for i, r in enumerate(runs):
        rob = Robot(home)
        for j, s in enumerate(r["batches"] + ([r["home"]] if r["home"] else [])):
            rob.run(s, f"run {i} cmd {j}")
        want = cube.apply_moves(home, r["moves"])
        if rob.c != want:
            rob.err.append(f"run {i}: physical cube != app model for {' '.join(r['moves'])}")
        if any(v != "L" for v in rob.sl.values()) or any(rob.sp.values()) or any(rob.tilt.values()):
            rob.err.append(f"run {i}: not in clamped home pose at the end {rob.sl} {rob.sp}")
        if rob.err:
            bad += 1
            print("\n".join(rob.err[:5]))
    print(f"solve: {len(runs)} runs, {sum(len(r['batches']) for r in runs)} SEQ commands, "
          f"{bad} with problems")
    return bad == 0


def fill(seq):
    return seq.replace("{d}", "300").replace("{s}", "400").replace("{p}", "400")


def check_scan(path, front="R", top="W"):
    data = json.load(open(path))
    plan, finish = (data["steps"], data.get("finish", "")) if isinstance(data, dict) else (data, "")
    home = cube.solved_colors(front, top)
    rob = Robot(home, scan_rules=True)
    seen = []
    for st in plan:
        rob.run(fill(st["present"]), st["id"] + " present")
        seen.append(rob.scan_pose(st["id"] + " scan pose"))
        if st.get("handoff"):   # second look: side arms hold at C, front/back released
            before = dict(rob.sl)
            rob.run(fill(st["handoff"]), st["id"] + " hand-off")
            if {k: ("R" if v == "L" else "L") for k, v in before.items()} != rob.sl:
                rob.err.append(f"{st['id']} second look did not swap the holding pair {rob.sl}")
            if rob.c[4] != seen[-1]:
                rob.err.append(f"{st['id']} hand-off moved the cube")
            rob.run(fill(st["handback"]), st["id"] + " hand-back")
            rob.scan_pose(st["id"] + " after hand-back")
        rob.run(fill(st["revert"]), st["id"] + " revert")
    if finish:
        rob.run(fill(finish), "finish")
    ok_home = rob.c == home and not any(rob.tilt.values()) and not any(rob.twist.values()) \
        and all(v == "L" for v in rob.sl.values()) and not any(rob.sp.values())
    print("scan: camera sees", " -> ".join(cube.COLOR_NAMES[c] for c in seen),
          "| back home:", ok_home, "| final grippers", rob.sp, "sliders", rob.sl)
    print("\n".join(rob.err) if rob.err else "scan: no rule violations")
    return not rob.err and ok_home, seen


if __name__ == "__main__":
    mode, path = sys.argv[1], sys.argv[2]
    ok = check_solve(path) if mode == "solve" else check_scan(path)[0]
    sys.exit(0 if ok else 1)
