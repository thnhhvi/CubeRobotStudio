import os
import random
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from cuberobot import cube, robot, scan, twophase  # noqa: E402
from cuberobot.controller import Controller  # noqa: E402

SOLVED = "".join(f * 9 for f in "URFDLB")


def test_geometry_matches_solver():
    rng = random.Random(1)
    for _ in range(100):
        mv = [rng.choice(twophase.MOVE_NAMES) for _ in range(10)]
        assert cube.apply_moves(SOLVED, mv) == twophase.apply_moves_facelets(SOLVED, mv)


def test_scan_order_and_coverage():
    order = scan.scan_order(scan.DEFAULT_PLAN, "R", "W")
    assert order == ["G", "Y", "B", "W", "R", "O"]
    cells = [c for *_, cs, _ in scan.walk(scan.DEFAULT_PLAN, "R", "W") for c in cs]
    assert sorted(cells) == list(range(54))


def test_seq_lengths_fit_firmware():
    for st in scan.DEFAULT_PLAN["steps"]:
        for k in ("present", "revert"):
            assert len("SEQ " + scan.fill(st[k], 500)) < robot.SEQ_BUF


def test_solver_and_robot_string():
    t = twophase.Tables()
    s = twophase.Solver(t)
    rng = random.Random(5)
    for _ in range(3):
        c = twophase.random_cube_string(rng)
        sol = s.solve(c, timeout=1.5)[-1]
        assert twophase.apply_moves_facelets(c, sol) == SOLVED
        rm = robot.to_robot_moves(sol)
        assert set(rm) <= set("URFDLBurfdlb")


def test_simulated_end_to_end():
    c = Controller()
    while not c.solver_ready:
        time.sleep(0.2)
    c.update_settings({"delay": 120, "front": "R", "top": "W"})
    c.connect("SIMULATOR")
    time.sleep(0.4)
    true = cube.apply_moves(cube.solved_colors("R", "W"), cube.random_scramble(20, random.Random(2)))
    c.start_scan()
    for i, (_, _, _, cells, col) in enumerate(scan.walk(c.plan, "R", "W")):
        while not (c.scan and c.scan["phase"] == "capture" and c.scan["index"] == i):
            time.sleep(0.02)
        c.capture({"tiles": [list(cube.DEFAULT_RGB[true[cells[k]]]) for k in range(9)],
                   "logo": {"detected": col == "W"}})
    while c.busy_task:
        time.sleep(0.05)
    assert c.colors == true
    c.execute_solution()
    while c.busy_task:
        time.sleep(0.05)
    assert c.colors == cube.solved_colors("R", "W")
    assert c.rq.orientation == robot.NORMAL
