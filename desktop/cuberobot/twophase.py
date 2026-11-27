"""
Kociemba two-phase solver (pure Python + numpy for table generation).

Facelet string order: U1..U9 R1..R9 F1..F9 D1..D9 L1..L9 B1..B9 (54 chars,
letters U R F D L B), the same format used by Kociemba's twophase.jar and the
`kociemba` pip package.

Tables are generated once (~20-60 s) and cached as a compressed .npz file.
"""
from __future__ import annotations

import os
import time
from math import comb

import numpy as np

# --------------------------------------------------------------------------
# Cubie level
# --------------------------------------------------------------------------
URF, UFL, ULB, UBR, DFR, DLF, DBL, DRB = range(8)
UR, UF, UL, UB, DR, DF, DL, DB, FR, FL, BL, BR = range(12)

U1, U2, U3, U4, U5, U6, U7, U8, U9 = range(0, 9)
R1, R2, R3, R4, R5, R6, R7, R8, R9 = range(9, 18)
F1, F2, F3, F4, F5, F6, F7, F8, F9 = range(18, 27)
D1, D2, D3, D4, D5, D6, D7, D8, D9 = range(27, 36)
L1, L2, L3, L4, L5, L6, L7, L8, L9 = range(36, 45)
B1, B2, B3, B4, B5, B6, B7, B8, B9 = range(45, 54)

FACES = "URFDLB"

CORNER_FACELET = [
    [U9, R1, F3], [U7, F1, L3], [U1, L1, B3], [U3, B1, R3],
    [D3, F9, R7], [D1, L9, F7], [D7, B9, L7], [D9, R9, B7],
]
EDGE_FACELET = [
    [U6, R2], [U8, F2], [U4, L2], [U2, B2], [D6, R8], [D2, F8],
    [D4, L8], [D8, B8], [F6, R4], [F4, L6], [B6, L4], [B4, R6],
]
CORNER_COLOR = [
    "URF", "UFL", "ULB", "UBR", "DFR", "DLF", "DBL", "DRB",
]
EDGE_COLOR = [
    "UR", "UF", "UL", "UB", "DR", "DF", "DL", "DB", "FR", "FL", "BL", "BR",
]


class CubieCube:
    __slots__ = ("cp", "co", "ep", "eo")

    def __init__(self, cp=None, co=None, ep=None, eo=None):
        self.cp = list(cp) if cp is not None else list(range(8))
        self.co = list(co) if co is not None else [0] * 8
        self.ep = list(ep) if ep is not None else list(range(12))
        self.eo = list(eo) if eo is not None else [0] * 12

    def copy(self):
        return CubieCube(self.cp, self.co, self.ep, self.eo)

    def corner_multiply(self, b: "CubieCube"):
        cp = [self.cp[b.cp[i]] for i in range(8)]
        co = [(self.co[b.cp[i]] + b.co[i]) % 3 for i in range(8)]
        self.cp, self.co = cp, co

    def edge_multiply(self, b: "CubieCube"):
        ep = [self.ep[b.ep[i]] for i in range(12)]
        eo = [(self.eo[b.ep[i]] + b.eo[i]) % 2 for i in range(12)]
        self.ep, self.eo = ep, eo

    def multiply(self, b: "CubieCube"):
        self.corner_multiply(b)
        self.edge_multiply(b)

    # ---- coordinates -----------------------------------------------------
    def get_twist(self):
        r = 0
        for i in range(7):
            r = 3 * r + self.co[i]
        return r

    def set_twist(self, twist):
        s = 0
        for i in range(6, -1, -1):
            self.co[i] = twist % 3
            s += self.co[i]
            twist //= 3
        self.co[7] = (-s) % 3

    def get_flip(self):
        r = 0
        for i in range(11):
            r = 2 * r + self.eo[i]
        return r

    def set_flip(self, flip):
        s = 0
        for i in range(10, -1, -1):
            self.eo[i] = flip % 2
            s += self.eo[i]
            flip //= 2
        self.eo[11] = (-s) % 2

    def get_slice_sorted(self):
        a = x = 0
        edge4 = []
        for j in range(11, -1, -1):
            if FR <= self.ep[j] <= BR:
                a += comb(11 - j, x + 1)
                edge4.insert(0, self.ep[j])
                x += 1
        b = 0
        for j in range(3, 0, -1):
            k = 0
            while edge4[j] != j + 8:
                _rotate_left(edge4, 0, j)
                k += 1
            b = (j + 1) * b + k
        return 24 * a + b

    def set_slice_sorted(self, idx):
        slice_edge = [FR, FL, BL, BR]
        other_edge = [UR, UF, UL, UB, DR, DF, DL, DB]
        b = idx % 24
        a = idx // 24
        ep = [-1] * 12
        for j in range(1, 4):
            k = b % (j + 1)
            b //= j + 1
            while k > 0:
                _rotate_right(slice_edge, 0, j)
                k -= 1
        x = 4
        for j in range(12):
            if x > 0 and a - comb(11 - j, x) >= 0:
                ep[j] = slice_edge[4 - x]
                a -= comb(11 - j, x)
                x -= 1
        x = 0
        for j in range(12):
            if ep[j] == -1:
                ep[j] = other_edge[x]
                x += 1
        self.ep = ep

    def get_corners(self):
        perm = list(self.cp)
        b = 0
        for j in range(7, 0, -1):
            k = 0
            while perm[j] != j:
                _rotate_left(perm, 0, j)
                k += 1
            b = (j + 1) * b + k
        return b

    def set_corners(self, idx):
        self.cp = list(range(8))
        for j in range(1, 8):
            k = idx % (j + 1)
            idx //= j + 1
            while k > 0:
                _rotate_right(self.cp, 0, j)
                k -= 1

    def get_ud_edges(self):
        perm = self.ep[0:8]
        b = 0
        for j in range(7, 0, -1):
            k = 0
            while perm[j] != j:
                _rotate_left(perm, 0, j)
                k += 1
            b = (j + 1) * b + k
        return b

    def set_ud_edges(self, idx):
        for i in range(8):
            self.ep[i] = i
        perm = self.ep[0:8]
        for j in range(1, 8):
            k = idx % (j + 1)
            idx //= j + 1
            while k > 0:
                _rotate_right(perm, 0, j)
                k -= 1
        self.ep[0:8] = perm

    # ---- validity ---------------------------------------------------------
    def corner_parity(self):
        s = 0
        for i in range(7, 0, -1):
            for j in range(i - 1, -1, -1):
                if self.cp[j] > self.cp[i]:
                    s += 1
        return s % 2

    def edge_parity(self):
        s = 0
        for i in range(11, 0, -1):
            for j in range(i - 1, -1, -1):
                if self.ep[j] > self.ep[i]:
                    s += 1
        return s % 2

    def verify(self):
        if sorted(self.ep) != list(range(12)):
            return "Some edges are missing or duplicated"
        if sum(self.eo) % 2:
            return "An edge is flipped (flip parity error)"
        if sorted(self.cp) != list(range(8)):
            return "Some corners are missing or duplicated"
        if sum(self.co) % 3:
            return "A corner is twisted (twist parity error)"
        if self.edge_parity() != self.corner_parity():
            return "Two pieces are swapped (permutation parity error)"
        return None


def _rotate_left(arr, l, r):
    t = arr[l]
    for i in range(l, r):
        arr[i] = arr[i + 1]
    arr[r] = t


def _rotate_right(arr, l, r):
    t = arr[r]
    for i in range(r, l, -1):
        arr[i] = arr[i - 1]
    arr[l] = t


# Basic face turns (clockwise quarter turn), Kociemba's definitions.
_BASIC = {
    "U": CubieCube([UBR, URF, UFL, ULB, DFR, DLF, DBL, DRB], [0] * 8,
                   [UB, UR, UF, UL, DR, DF, DL, DB, FR, FL, BL, BR], [0] * 12),
    "R": CubieCube([DFR, UFL, ULB, URF, DRB, DLF, DBL, UBR], [2, 0, 0, 1, 1, 0, 0, 2],
                   [FR, UF, UL, UB, BR, DF, DL, DB, DR, FL, BL, UR], [0] * 12),
    "F": CubieCube([UFL, DLF, ULB, UBR, URF, DFR, DBL, DRB], [1, 2, 0, 0, 2, 1, 0, 0],
                   [UR, FL, UL, UB, DR, FR, DL, DB, UF, DF, BL, BR],
                   [0, 1, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0]),
    "D": CubieCube([URF, UFL, ULB, UBR, DLF, DBL, DRB, DFR], [0] * 8,
                   [UR, UF, UL, UB, DF, DL, DB, DR, FR, FL, BL, BR], [0] * 12),
    "L": CubieCube([URF, ULB, DBL, UBR, DFR, UFL, DLF, DRB], [0, 1, 2, 0, 0, 2, 1, 0],
                   [UR, UF, BL, UB, DR, DF, FL, DB, FR, UL, DL, BR], [0] * 12),
    "B": CubieCube([URF, UFL, UBR, DRB, DFR, DLF, ULB, DBL], [0, 0, 1, 2, 0, 0, 2, 1],
                   [UR, UF, UL, BR, DR, DF, DL, BL, FR, FL, UB, DB],
                   [0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1]),
}

# 18 moves: U1 U2 U3 R1 R2 R3 F1 ... B3   (index = 3*face + power-1)
MOVE_NAMES = [f + p for f in FACES for p in ("", "2", "'")]
MOVE_CUBES: list[CubieCube] = []
for _f in FACES:
    _c = CubieCube()
    for _p in range(3):
        _c.multiply(_BASIC[_f])
        MOVE_CUBES.append(_c.copy())

PHASE2_MOVES = [m for m in range(18)
                if MOVE_NAMES[m][0] in "UD" or MOVE_NAMES[m].endswith("2")]


def facelets_to_cubie(s: str) -> CubieCube:
    if len(s) != 54:
        raise ValueError("Cube string must have 54 facelets")
    for f in FACES:
        if s.count(f) != 9:
            raise ValueError(f"Each color must appear 9 times ({f}: {s.count(f)})")
    cc = CubieCube()
    cc.cp = [-1] * 8
    cc.ep = [-1] * 12
    for i in range(8):
        fac = CORNER_FACELET[i]
        ori = 0
        for ori in range(3):
            if s[fac[ori]] in "UD":
                break
        else:
            raise ValueError("Invalid corner (no U/D sticker)")
        col1 = s[fac[(ori + 1) % 3]]
        col2 = s[fac[(ori + 2) % 3]]
        for j in range(8):
            if col1 == CORNER_COLOR[j][1] and col2 == CORNER_COLOR[j][2]:
                cc.cp[i] = j
                cc.co[i] = ori
                break
    for i in range(12):
        a, b = s[EDGE_FACELET[i][0]], s[EDGE_FACELET[i][1]]
        for j in range(12):
            if a == EDGE_COLOR[j][0] and b == EDGE_COLOR[j][1]:
                cc.ep[i] = j
                cc.eo[i] = 0
                break
            if a == EDGE_COLOR[j][1] and b == EDGE_COLOR[j][0]:
                cc.ep[i] = j
                cc.eo[i] = 1
                break
    return cc


def cubie_to_facelets(cc: CubieCube) -> str:
    s = [""] * 54
    for i, f in enumerate(FACES):
        s[9 * i + 4] = f
    for i in range(8):
        j = cc.cp[i]
        ori = cc.co[i]
        for n in range(3):
            s[CORNER_FACELET[i][(n + ori) % 3]] = CORNER_COLOR[j][n]
    for i in range(12):
        j = cc.ep[i]
        ori = cc.eo[i]
        for n in range(2):
            s[EDGE_FACELET[i][(n + ori) % 2]] = EDGE_COLOR[j][n]
    return "".join(s)


def verify(s: str) -> str | None:
    """Return None if the facelet string is a solvable cube, else an error."""
    try:
        cc = facelets_to_cubie(s)
    except ValueError as e:
        return str(e)
    if -1 in cc.cp or -1 in cc.ep:
        return "Impossible piece (sticker colors do not form a real piece)"
    return cc.verify()


# --------------------------------------------------------------------------
# Tables
# --------------------------------------------------------------------------
N_TWIST, N_FLIP, N_SLICE, N_SLICE_SORTED = 2187, 2048, 495, 11880
N_PERM8, N_SLICE24 = 40320, 24

_TABLE_VERSION = 3


class Tables:
    def __init__(self, cache_dir: str | None = None, log=print):
        self.log = log
        cache_dir = cache_dir or os.path.join(os.path.expanduser("~"), ".cuberobot")
        os.makedirs(cache_dir, exist_ok=True)
        path = os.path.join(cache_dir, f"twophase_v{_TABLE_VERSION}.npz")
        bundled = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                               f"twophase_v{_TABLE_VERSION}.npz")
        for p in (path, bundled):
            if os.path.exists(p):
                try:
                    d = np.load(p)
                    self._assign(d)
                    return
                except Exception:  # corrupt cache -> rebuild
                    pass
        t = time.time()
        self.log("Generating Kociemba tables (one-time, ~30 s)...")
        d = self._build()
        np.savez_compressed(path, **d)
        self._assign(d)
        self.log(f"Tables ready in {time.time() - t:.1f}s")

    def _assign(self, d):
        for k in ("twist_move", "flip_move", "slice_sorted_move", "corners_move",
                  "ud_edges_move", "slice_twist_prun", "slice_flip_prun",
                  "corner_slice_prun", "edge_slice_prun"):
            setattr(self, k, d[k])
        # python lists are faster than numpy scalars inside the search
        self.twist_l = self.twist_move.tolist()
        self.flip_l = self.flip_move.tolist()
        self.ss_l = self.slice_sorted_move.tolist()
        self.corn_l = self.corners_move.tolist()
        self.ude_l = self.ud_edges_move.tolist()
        self.stp = bytes(self.slice_twist_prun.tobytes())
        self.sfp = bytes(self.slice_flip_prun.tobytes())
        self.csp = bytes(self.corner_slice_prun.tobytes())
        self.esp = bytes(self.edge_slice_prun.tobytes())

    @staticmethod
    def _move_table(n, setter, getter, part, moves=range(18)):
        tbl = np.full((n, 18), -1, dtype=np.int32)
        a = CubieCube()
        for i in range(n):
            setter(a, i)
            for m in moves:
                b = a.copy()
                if part == "c":
                    b.corner_multiply(MOVE_CUBES[m])
                else:
                    b.edge_multiply(MOVE_CUBES[m])
                tbl[i, m] = getter(b)
        return tbl

    def _build(self):
        L = self.log
        L("  move tables: twist")
        twist_move = self._move_table(N_TWIST, CubieCube.set_twist, CubieCube.get_twist, "c")
        L("  move tables: flip")
        flip_move = self._move_table(N_FLIP, CubieCube.set_flip, CubieCube.get_flip, "e")
        L("  move tables: slice")
        slice_sorted_move = self._move_table(N_SLICE_SORTED, CubieCube.set_slice_sorted,
                                             CubieCube.get_slice_sorted, "e")
        L("  move tables: corners")
        corners_move = self._move_table(N_PERM8, CubieCube.set_corners,
                                        CubieCube.get_corners, "c")
        L("  move tables: ud edges")
        ud_edges_move = self._move_table(N_PERM8, CubieCube.set_ud_edges,
                                         CubieCube.get_ud_edges, "e", PHASE2_MOVES)

        slice_move = slice_sorted_move[np.arange(N_SLICE) * 24] // 24  # (495,18)
        L("  pruning: slice x twist")
        slice_twist_prun = _bfs2(slice_move, twist_move, N_SLICE, N_TWIST, range(18))
        L("  pruning: slice x flip")
        slice_flip_prun = _bfs2(slice_move, flip_move, N_SLICE, N_FLIP, range(18))
        s24 = slice_sorted_move[:24]  # phase-2: slice edges stay in slice
        L("  pruning: corners x slice")
        corner_slice_prun = _bfs2(corners_move, s24, N_PERM8, N_SLICE24, PHASE2_MOVES)
        L("  pruning: edges x slice")
        edge_slice_prun = _bfs2(ud_edges_move, s24, N_PERM8, N_SLICE24, PHASE2_MOVES)
        return dict(twist_move=twist_move, flip_move=flip_move,
                    slice_sorted_move=slice_sorted_move, corners_move=corners_move,
                    ud_edges_move=ud_edges_move, slice_twist_prun=slice_twist_prun,
                    slice_flip_prun=slice_flip_prun, corner_slice_prun=corner_slice_prun,
                    edge_slice_prun=edge_slice_prun)


def _bfs2(move_a, move_b, na, nb, moves):
    """Breadth-first distance table over index a*nb + b (both start at 0)."""
    n = na * nb
    dist = np.full(n, 255, dtype=np.uint8)
    dist[0] = 0
    frontier = np.array([0], dtype=np.int64)
    depth = 0
    while frontier.size:
        a = frontier // nb
        b = frontier % nb
        nxt = []
        for m in moves:
            na_ = move_a[a, m].astype(np.int64)
            nb_ = move_b[b, m].astype(np.int64)
            nxt.append(na_ * nb + nb_)
        cand = np.unique(np.concatenate(nxt))
        cand = cand[dist[cand] == 255]
        depth += 1
        dist[cand] = depth
        frontier = cand
    return dist


# --------------------------------------------------------------------------
# Search
# --------------------------------------------------------------------------
_P2_SET = set(PHASE2_MOVES)


class Solver:
    def __init__(self, tables: Tables):
        self.t = tables

    def solve(self, cubestring: str, max_length: int = 22, timeout: float = 3.0,
              want_many: bool = True):
        """Return a list of solutions (each a list of move names), shortest last.

        Search keeps going until `timeout` seconds, tightening the length
        bound each time a shorter solution is found.
        """
        err = verify(cubestring)
        if err:
            raise ValueError(err)
        cc = facelets_to_cubie(cubestring)
        if cubestring == "".join(f * 9 for f in FACES):
            return [[]]
        self.cc = cc
        self.deadline = time.time() + timeout
        self.best = max_length + 1
        self.solutions: list[list[str]] = []
        self.want_many = want_many
        self.path: list[int] = []
        t = self.t
        twist, flip, ss = cc.get_twist(), cc.get_flip(), cc.get_slice_sorted()
        corners = cc.get_corners()
        d0 = self._h1(twist, flip, ss // 24)
        self.stop = False
        for togo in range(d0, max_length + 1):
            if togo >= self.best or self.stop:
                break
            self._phase1(twist, flip, ss, corners, togo)
        if not self.solutions:
            raise TimeoutError("No solution found within the time / length limit")
        return self.solutions

    def _h1(self, twist, flip, sl):
        a = self.t.stp[sl * N_TWIST + twist]
        b = self.t.sfp[sl * N_FLIP + flip]
        return a if a > b else b

    def _phase1(self, twist, flip, ss, corners, togo):
        if self.stop:
            return
        t = self.t
        if togo == 0:
            # entering phase 2: last move must not be a phase-2 move
            if self.path and self.path[-1] in _P2_SET:
                return
            self._start_phase2(ss, corners)
            return
        if time.time() > self.deadline and self.solutions:
            self.stop = True
            return
        last = self.path[-1] if self.path else -1
        lf = last // 3
        for m in range(18):
            f = m // 3
            if last >= 0 and (f == lf or (f == lf - 3)):
                # same face, or opposite face in canonical order (U before D etc.)
                continue
            tw = t.twist_l[twist][m]
            fl = t.flip_l[flip][m]
            s2 = t.ss_l[ss][m]
            h = self._h1(tw, fl, s2 // 24)
            if h >= togo:
                continue
            self.path.append(m)
            self._phase1(tw, fl, s2, t.corn_l[corners][m], togo - 1)
            self.path.pop()
            if self.stop:
                return

    def _start_phase2(self, ss, corners):
        # need ud-edge permutation after phase-1 moves (apply on cubie level)
        c = CubieCube(ep=self.cc.ep)
        for m in self.path:
            c.edge_multiply(MOVE_CUBES[m])
        ude = c.get_ud_edges()
        t = self.t
        n1 = len(self.path)
        limit = self.best - 1 - n1
        if limit < 0:
            return
        h2 = max(t.csp[corners * 24 + ss], t.esp[ude * 24 + ss])
        if h2 > limit:
            return
        for d2 in range(h2, limit + 1):
            self.p2path = []
            if self._phase2(corners, ude, ss, d2):
                sol = [MOVE_NAMES[m] for m in self.path + self.p2path]
                self.solutions.append(sol)
                self.best = len(sol)
                if not self.want_many:
                    self.stop = True
                return

    def _phase2(self, corners, ude, ss, togo):
        if togo == 0:
            return corners == 0 and ude == 0 and ss == 0
        t = self.t
        prev = self.p2path[-1] if self.p2path else (self.path[-1] if self.path else -1)
        lf = prev // 3 if prev >= 0 else -9
        for m in PHASE2_MOVES:
            f = m // 3
            if f == lf or f == lf - 3:
                continue
            c2 = t.corn_l[corners][m]
            e2 = t.ude_l[ude][m]
            s2 = t.ss_l[ss][m]
            a = t.csp[c2 * 24 + s2]
            b = t.esp[e2 * 24 + s2]
            if (a if a > b else b) >= togo:
                continue
            self.p2path.append(m)
            if self._phase2(c2, e2, s2, togo - 1):
                return True
            self.p2path.pop()
        return False


# --------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------
def apply_moves_facelets(s: str, moves: list[str]) -> str:
    cc = facelets_to_cubie(s)
    for mv in moves:
        cc.multiply(MOVE_CUBES[MOVE_NAMES.index(mv)])
    return cubie_to_facelets(cc)


def random_cube_string(rng=None) -> str:
    import random
    rng = rng or random.Random()
    cc = CubieCube()
    cc.set_twist(rng.randrange(N_TWIST))
    cc.set_flip(rng.randrange(N_FLIP))
    while True:
        cc.set_corners(rng.randrange(N_PERM8))
        perm = list(range(12))
        rng.shuffle(perm)
        cc.ep = perm
        if cc.edge_parity() == cc.corner_parity():
            break
    return cubie_to_facelets(cc)
