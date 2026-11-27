"""
Physical simulation of the 4-arm robot, used to check the servo commands the app sends.

The simulated cube is 54 stickers in the robot frame. Servo commands are played back
group by group (commands between two delays run at the same time):

  * a spinner turning while its slider clamps (L or C) and its partner holds still
    turns that face's LAYER (all four arms must be holding),
  * two opposite spinners turning mirrored while both clamp and the other pair is
    released (R) ROLL the whole cube (45 deg steps allowed, e.g. scan lifts),
  * a slider may only clamp square to its face (gripper at 0/+-90 relative to it),
  * while the cube rolls, the released pair's grippers must be horizontal (+-90) during
    scanning (the corners of a tilted cube sweep into vertical grippers).

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
        self.err = []
        self.scan_rules = scan_rules

    def clamped(self, arm):
        return self.sl[arm.upper()] in "LC"

    def run(self, seq, label):
        for grp in re.split(r"\d+", seq):
            spin, slide = {}, {}
            for s, st in re.findall(r"([FBRLfbrl])([LlCrR])", grp):
                (slide if s.isupper() else spin)[s] = st
            # sliders first (the firmware writes them in the same instant; clamping onto a
            # gripper that also moves in this group is checked with the new angle)
            moved = {k: ANG[v] - self.sp[k] for k, v in spin.items() if ANG[v] != self.sp[k]}
            for s, st in slide.items():
                if st == "L" and self.sl[s] != "L":
                    arm = s.lower()
                    ang = ANG[spin[arm]] if arm in spin else self.sp[arm]
                    if not self.square(arm, ang):
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
                    if self.scan_rules and any(self.sp[o] % 180 == 0 for o in others):
                        self.err.append(f"{label}: cube rolled while released grippers are vertical")
                    self.roll(k, d)
                    done |= {k, p}
                else:                               # layer turn
                    if not all(self.clamped(o) for o in "fbrl"):
                        self.err.append(f"{label}: {k} turns a layer while the cube is not held")
                    if any(self.tilt.values()):
                        self.err.append(f"{label}: layer turn on a tilted cube")
                    if d % 90:
                        self.err.append(f"{label}: {k} turns its layer by {d} deg")
                    q = (d // 90) % 4
                    for _ in range(q):
                        self.c = cube.apply_move(self.c, FACE[k])
                    done.add(k)
            for k, v in spin.items():
                self.sp[k] = ANG[v]

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


def check_scan(path, front="R", top="W"):
    plan = json.load(open(path))
    home = cube.solved_colors(front, top)
    rob = Robot(home, scan_rules=True)
    seen = []
    for st in plan:
        rob.run(st["present"].replace("{d}", "200"), st["id"] + " present")
        seen.append(rob.camera_face_color())
        rob.run(st["revert"].replace("{d}", "200"), st["id"] + " revert")
    ok_home = rob.c == home and not any(rob.tilt.values())
    print("scan: camera sees", " -> ".join(cube.COLOR_NAMES[c] for c in seen),
          "| back home:", ok_home, "| final grippers", rob.sp, "sliders", rob.sl)
    print("\n".join(rob.err) if rob.err else "scan: no rule violations")
    return not rob.err and ok_home, seen


if __name__ == "__main__":
    mode, path = sys.argv[1], sys.argv[2]
    ok = check_solve(path) if mode == "solve" else check_scan(path)[0]
    sys.exit(0 if ok else 1)
