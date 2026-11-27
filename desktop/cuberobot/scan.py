"""
Scan plan: which servo sequences present each face to the camera at the robot
front, and how each captured 3x3 image maps onto the cube.

Default order (example: RED held by the FRONT arm, WHITE on top):

  Phase A - roll around the FRONT/BACK axis (front+back spinners turn, left/right
            arms release, then left/right re-grip while the spinners re-centre):
     1. green  (left)   rolled up and tilted 45 deg towards the robot front -> scan
                        -> the same arms turn it back 45 deg (green now on top)
     2. yellow  same move -> scan -> back
     3. blue    same move -> scan -> back   (white is now held by the LEFT arm)
     4. white   same move -> scan -> back   (white is top again)
  Phase B - roll around the LEFT/RIGHT axis (left/right arms hold green+blue):
     5. red     tilted 45 deg towards the robot front -> scan -> back
     6. orange  flipped over (135 deg) -> scan -> back
     7. cube is home again: red front, white top.

All sequences use the firmware SEQ syntax and the placeholder {d} for the
speed delay. They live in scan_plan.json and can be edited / jogged from the
desktop app if your calibration needs tweaks.
"""
from __future__ import annotations

import copy
import json
import os

from . import cube

# robot-frame whole-cube rotation that each step leaves the cube in while it
# is being photographed (the 45 deg tilt itself does not change the mapping)
DEFAULT_PLAN = {
    "camera_face": "U",
    "steps": [
        {
            "id": "A1", "axis": "front/back",
            "present": "RLLLFRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}",
            "revert": "fCfCbCbC{d}RCLC{d}FCBC",
            "view": ["z"], "keep": ["z"], "image_rot": 0,
        },
        {
            "id": "A2", "axis": "front/back",
            "present": "RLLLFRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}",
            "revert": "fCfCbCbC{d}RCLC{d}FCBC",
            "view": ["z"], "keep": ["z"], "image_rot": 0,
        },
        {
            "id": "A3", "axis": "front/back",
            "present": "RLLLFRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}",
            "revert": "fCfCbCbC{d}RCLC{d}FCBC",
            "view": ["z"], "keep": ["z"], "image_rot": 0,
        },
        {
            "id": "A4", "axis": "front/back",
            "present": "RLLLFRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}",
            "revert": "fCfCbCbC{d}RCLC{d}FCBC",
            "view": ["z"], "keep": ["z"], "image_rot": 0,
        },
        {
            "id": "B1", "axis": "left/right",
            "present": "RLLLFRBR{d}rrll{d}",
            "revert": "rCrClClC{d}FCBC{d}RCLC",
            "view": ["x"], "keep": [], "image_rot": 0,
        },
        {
            "id": "B2", "axis": "left/right",
            "present": "FLBLRRLR{d}rRlL{d}RLLL{d}FRBR{d}rllr{d}",
            "revert": "rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RCLC{d}FCBC",
            "view": ["x'"], "keep": [], "image_rot": 0,
        },
    ],
}


def plan_path(cfg_dir):
    return os.path.join(cfg_dir, "scan_plan.json")


def load_plan(cfg_dir):
    p = plan_path(cfg_dir)
    if os.path.exists(p):
        try:
            with open(p, "r", encoding="utf-8") as f:
                plan = json.load(f)
            if plan.get("steps"):
                return plan
        except Exception:
            pass
    return copy.deepcopy(DEFAULT_PLAN)


def save_plan(cfg_dir, plan):
    with open(plan_path(cfg_dir), "w", encoding="utf-8") as f:
        json.dump(plan, f, indent=2)


def fill(seq: str, d: int) -> str:
    return seq.replace("{d}", str(int(d)))


def _index_state_after(rotations):
    """Robot-frame facelet j currently shows home facelet perm[j]."""
    idx = list(range(54))
    for mv in rotations:
        p = cube.MOVE_PERM[mv]
        idx = [idx[p[i]] for i in range(54)]
    return idx


def walk(plan, front, top):
    """Yield per step: (step, orientation_before, view_rotations, home facelet
    indices seen by the camera in image order, colour letter of that face)."""
    centers = cube.center_colors(front, top)
    home = cube.solved_colors(front, top)
    orient = []
    cam_face = plan.get("camera_face", "U")
    fi = cube.FACES.index(cam_face)
    for step in plan["steps"]:
        view = orient + list(step["view"])
        perm = _index_state_after(view)
        robot_face = [perm[9 * fi + k] for k in range(9)]
        # image orientation correction (quarter turns clockwise)
        img = list(range(9))
        for _ in range(int(step.get("image_rot", 0)) % 4):
            img = [img[cube._QROT[k]] for k in range(9)]
        cells = [robot_face[img[k]] for k in range(9)]
        color = home[cells[4]]
        yield step, list(orient), view, cells, color
        orient = orient + list(step.get("keep", []))
    _ = centers


def scan_order(plan, front, top):
    return [c for *_, c in walk(plan, front, top)]
