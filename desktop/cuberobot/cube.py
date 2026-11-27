"""
Geometric facelet model of the cube in the *robot frame*.

Robot frame axes (right-handed):
    +x : face held by the RIGHT arm  (cube face R)
    +y : top of the cube             (cube face U)
    +z : face held by the FRONT arm  (cube face F)
The "robot front" (logo side) is between the FRONT and RIGHT arms, i.e. the
direction (+x +z).

Facelet indices follow Kociemba: U1..U9, R1..R9, F1..F9, D1..D9, L1..L9, B1..B9.
"""
from __future__ import annotations

import itertools
import math
import random

FACES = "URFDLB"
COLORS = "WYROBG"  # white yellow red orange blue green
COLOR_NAMES = {"W": "white", "Y": "yellow", "R": "red", "O": "orange", "B": "blue", "G": "green"}
OPPOSITE = {"W": "Y", "Y": "W", "R": "O", "O": "R", "B": "G", "G": "B"}

# outward normal, "right" and "down" in-face vectors for the standard net view
FACE_FRAME = {
    "U": ((0, 1, 0), (1, 0, 0), (0, 0, 1)),
    "R": ((1, 0, 0), (0, 0, -1), (0, -1, 0)),
    "F": ((0, 0, 1), (1, 0, 0), (0, -1, 0)),
    "D": ((0, -1, 0), (1, 0, 0), (0, 0, -1)),
    "L": ((-1, 0, 0), (0, 0, 1), (0, -1, 0)),
    "B": ((0, 0, -1), (-1, 0, 0), (0, -1, 0)),
}
NORMAL_TO_FACE = {v[0]: k for k, v in FACE_FRAME.items()}


def _add(*vs):
    return tuple(sum(c) for c in zip(*vs))


def _mul(k, v):
    return tuple(k * c for c in v)


def facelet_geometry():
    """List of (position, normal) for the 54 facelets (integer coordinates)."""
    out = []
    for f in FACES:
        n, r, d = FACE_FRAME[f]
        for row in range(3):
            for col in range(3):
                p = _add(_mul(3, n), _mul(2 * (col - 1), r), _mul(2 * (row - 1), d))
                out.append((p, n))
    return out


GEOM = facelet_geometry()
INDEX = {g: i for i, g in enumerate(GEOM)}


# ---------------------------------------------------------------------------
# rotations (integer 3x3 matrices, row-major tuples)
# ---------------------------------------------------------------------------
def mat_mul(a, b):
    return tuple(tuple(sum(a[i][k] * b[k][j] for k in range(3)) for j in range(3)) for i in range(3))


def mat_vec(m, v):
    return tuple(sum(m[i][k] * v[k] for k in range(3)) for i in range(3))


def mat_T(m):
    return tuple(tuple(m[j][i] for j in range(3)) for i in range(3))


IDENT = ((1, 0, 0), (0, 1, 0), (0, 0, 1))


def rot_about(axis, quarter_turns_cw):
    """Rotation by quarter turns *clockwise when looking at the face the axis
    points to* (cube-notation convention)."""
    q = quarter_turns_cw % 4
    # clockwise looking from +axis == rotation by -90deg about +axis
    ang = -math.pi / 2 * q
    c, s = round(math.cos(ang)), round(math.sin(ang))
    x, y, z = axis
    # Rodrigues for unit integer axis
    m = [[0] * 3 for _ in range(3)]
    K = ((0, -z, y), (z, 0, -x), (-y, x, 0))
    for i in range(3):
        for j in range(3):
            kk = sum(K[i][t] * K[t][j] for t in range(3))
            m[i][j] = (1 if i == j else 0) + s * K[i][j] + (1 - c) * kk
    return tuple(tuple(r) for r in m)


AXIS = {f: FACE_FRAME[f][0] for f in FACES}
# whole-cube rotations in standard notation: x follows R, y follows U, z follows F
CUBE_ROT = {"x": rot_about(AXIS["R"], 1), "y": rot_about(AXIS["U"], 1), "z": rot_about(AXIS["F"], 1)}


def _perm_from_rotation(m, layer_axis=None):
    """perm[dst] = src for rotating facelets (whole cube or one layer)."""
    perm = list(range(54))
    for src, (p, n) in enumerate(GEOM):
        if layer_axis is not None and sum(a * b for a, b in zip(p, layer_axis)) <= 0:
            continue
        dst = INDEX[(mat_vec(m, p), mat_vec(m, n))]
        perm[dst] = src
    return perm


MOVE_PERM = {}
for _f in FACES:
    for _q, _suf in ((1, ""), (2, "2"), (3, "'")):
        MOVE_PERM[_f + _suf] = _perm_from_rotation(rot_about(AXIS[_f], _q), AXIS[_f])
for _r, _m in CUBE_ROT.items():
    MOVE_PERM[_r] = _perm_from_rotation(_m)
    MOVE_PERM[_r + "2"] = _perm_from_rotation(mat_mul(_m, _m))
    MOVE_PERM[_r + "'"] = _perm_from_rotation(mat_mul(_m, mat_mul(_m, _m)))


def apply_move(state: str, mv: str) -> str:
    p = MOVE_PERM[mv]
    return "".join(state[p[i]] for i in range(54))


def apply_moves(state: str, moves) -> str:
    for mv in moves:
        state = apply_move(state, mv)
    return state


def invert_moves(moves):
    out = []
    for mv in reversed(moves):
        if mv.endswith("'"):
            out.append(mv[:-1])
        elif mv.endswith("2"):
            out.append(mv)
        else:
            out.append(mv + "'")
    return out


# ---------------------------------------------------------------------------
# colour scheme / orientation choice
# ---------------------------------------------------------------------------
# Western scheme: white top, green front, red right
STANDARD = {(0, 1, 0): "W", (0, -1, 0): "Y", (0, 0, 1): "G", (0, 0, -1): "B",
            (1, 0, 0): "R", (-1, 0, 0): "O"}


def all_rotations():
    seen, out = set(), []
    gens = [CUBE_ROT["x"], CUBE_ROT["y"], CUBE_ROT["z"]]
    frontier = [IDENT]
    while frontier:
        m = frontier.pop()
        if m in seen:
            continue
        seen.add(m)
        out.append(m)
        frontier.extend(mat_mul(g, m) for g in gens)
    return out


ROTATIONS = all_rotations()  # 24


def center_colors(front: str, top: str) -> dict:
    """Map robot face letter -> colour letter for a chosen front/top colour."""
    if front == top or OPPOSITE[front] == top:
        raise ValueError("Front and top colours must be adjacent")
    for m in ROTATIONS:
        col = {mat_vec(m, n): c for n, c in STANDARD.items()}
        if col[(0, 1, 0)] == top and col[(0, 0, 1)] == front:
            return {f: col[AXIS[f]] for f in FACES}
    raise ValueError("No orientation found")


def solved_colors(front: str, top: str) -> str:
    cc = center_colors(front, top)
    return "".join(cc[f] * 9 for f in FACES)


def colors_to_facelets(colors: str) -> str:
    """Colour string (54 colour letters) -> Kociemba face-letter string."""
    cmap = {colors[9 * i + 4]: f for i, f in enumerate(FACES)}
    if len(cmap) != 6:
        raise ValueError("Centre colours are not 6 distinct colours")
    return "".join(cmap.get(c, "?") for c in colors)


# ---------------------------------------------------------------------------
# Colour classification
# ---------------------------------------------------------------------------
def _srgb_to_lin(c):
    c = c / 255.0
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def rgb_to_lab(rgb):
    r, g, b = (_srgb_to_lin(v) for v in rgb)
    x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047
    y = (0.2126 * r + 0.7152 * g + 0.0722 * b)
    z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883

    def f(t):
        return t ** (1 / 3) if t > 0.008856 else 7.787 * t + 16 / 116
    fx, fy, fz = f(x), f(y), f(z)
    return (116 * fy - 16, 500 * (fx - fy), 200 * (fy - fz))


def _dist(a, b):
    # weight lightness less: lighting varies more than chroma
    return math.sqrt(0.5 * (a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2 + (a[2] - b[2]) ** 2)


DEFAULT_RGB = {"W": (235, 235, 235), "Y": (240, 220, 40), "R": (200, 20, 30),
               "O": (250, 120, 20), "B": (20, 70, 200), "G": (20, 170, 70)}


def classify(samples: dict, expected_centers: dict):
    """
    samples: {home_facelet_index: (r,g,b)} for all 54 facelets.
    expected_centers: {face_letter: colour_letter} (from the front/top choice).
    Returns 54-char colour string. Centres are taken from the expected
    orientation (this is what bypasses the GAN logo on the white centre).
    Each colour gets exactly 9 stickers (balanced assignment).
    """
    refs = {}
    for i, f in enumerate(FACES):
        c = expected_centers[f]
        rgb = samples.get(9 * i + 4)
        refs[c] = rgb_to_lab(rgb) if rgb and c != "W" else rgb_to_lab(DEFAULT_RGB[c])
    # white reference: use the brightest-low-chroma sticker average if centre has a logo
    labs = {i: rgb_to_lab(v) for i, v in samples.items()}
    whites = sorted((v for i, v in labs.items() if i % 9 != 4),
                    key=lambda L: -(L[0] - 2 * math.hypot(L[1], L[2])))[:6]
    if whites:
        refs["W"] = tuple(sum(w[k] for w in whites) / len(whites) for k in range(3))

    out = [None] * 54
    for i, f in enumerate(FACES):
        out[9 * i + 4] = expected_centers[f]
    idx = [i for i in range(54) if i % 9 != 4 and i in labs]
    cost = {(i, c): _dist(labs[i], refs[c]) for i in idx for c in COLORS}
    # greedy balanced assignment
    count = {c: 1 for c in COLORS}
    for (i, c) in sorted(cost, key=cost.get):
        if out[i] is None and count[c] < 9:
            out[i] = c
            count[c] += 1
    # pairwise-swap improvement
    improved = True
    while improved:
        improved = False
        for a, b in itertools.combinations(idx, 2):
            ca, cb = out[a], out[b]
            if ca == cb:
                continue
            if cost[(a, cb)] + cost[(b, ca)] + 1e-9 < cost[(a, ca)] + cost[(b, cb)]:
                out[a], out[b] = cb, ca
                improved = True
    return "".join(c or "?" for c in out)


# ---------------------------------------------------------------------------
# Auto orientation repair
# ---------------------------------------------------------------------------
# rotate a face's 9 stickers clockwise by one quarter: new[r][c] = old[2-c][r]
_QROT = [6, 3, 0, 7, 4, 1, 8, 5, 2]


def rotate_face(colors: str, face_idx: int, q: int) -> str:
    s = list(colors)
    blk = s[9 * face_idx: 9 * face_idx + 9]
    for _ in range(q % 4):
        blk = [blk[_QROT[k]] for k in range(9)]
    s[9 * face_idx: 9 * face_idx + 9] = blk
    return "".join(s)


def auto_orient(colors: str, verify):
    """Try all 4^6 face rotations; return (fixed_colors, rotations) or None."""
    if verify(colors_to_facelets(colors)) is None:
        return colors, (0,) * 6
    hits = []
    for rots in itertools.product(range(4), repeat=6):
        s = colors
        for fi, q in enumerate(rots):
            if q:
                s = rotate_face(s, fi, q)
        if verify(colors_to_facelets(s)) is None:
            hits.append((sum(1 for q in rots if q), s, rots))
    if not hits:
        return None
    hits.sort(key=lambda h: h[0])
    return hits[0][1], hits[0][2]


def random_scramble(n=22, rng=None):
    rng = rng or random.Random()
    out, last = [], None
    for _ in range(n):
        while True:
            f = rng.choice(FACES)
            if f != last and not (last and OPPOSITE_FACE[f] == last and rng.random() < 0.5):
                break
        last = f
        out.append(f + rng.choice(["", "2", "'"]))
    return out


OPPOSITE_FACE = {"U": "D", "D": "U", "R": "L", "L": "R", "F": "B", "B": "F"}
