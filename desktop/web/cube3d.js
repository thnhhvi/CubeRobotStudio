/* Dependency-free 3D Rubik's cube renderer (Canvas 2D) with layer-turn and
 * whole-cube animations. Shared by the laptop UI and the phone WebView.
 *
 * Robot frame: +x = RIGHT arm face, +y = top, +z = FRONT arm face.
 * Colour string: 54 colour letters (W Y R O B G ?) in Kociemba facelet order
 * U1..U9 R1..R9 F1..F9 D1..D9 L1..L9 B1..B9.
 */
(function (global) {
  "use strict";
  const FACES = "URFDLB";
  const FRAME = {
    U: [[0, 1, 0], [1, 0, 0], [0, 0, 1]],
    R: [[1, 0, 0], [0, 0, -1], [0, -1, 0]],
    F: [[0, 0, 1], [1, 0, 0], [0, -1, 0]],
    D: [[0, -1, 0], [1, 0, 0], [0, 0, -1]],
    L: [[-1, 0, 0], [0, 0, 1], [0, -1, 0]],
    B: [[0, 0, -1], [-1, 0, 0], [0, -1, 0]],
  };
  const PALETTE = {
    W: "#f4f4f0", Y: "#ffd60a", R: "#d7263d", O: "#ff7a1a", B: "#1e5bd8", G: "#1fa34a",
    "?": "#5a5a5a",
  };

  const add = (a, b) => [a[0] + b[0], a[1] + b[1], a[2] + b[2]];
  const mul = (k, a) => [k * a[0], k * a[1], k * a[2]];
  const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
  const key = (p, n) => p.join(",") + "|" + n.join(",");

  // facelet geometry
  const GEOM = [];
  for (const f of FACES) {
    const [n, r, d] = FRAME[f];
    for (let row = 0; row < 3; row++)
      for (let col = 0; col < 3; col++) {
        const p = add(add(mul(3, n), mul(2 * (col - 1), r)), mul(2 * (row - 1), d));
        GEOM.push({ p, n, r, d, face: f });
      }
  }
  const INDEX = {};
  GEOM.forEach((g, i) => (INDEX[key(g.p, g.n)] = i));

  // integer rotation matrix: quarter turns clockwise looking from +axis
  function rotQ(axis, q) {
    q = ((q % 4) + 4) % 4;
    const ang = (-Math.PI / 2) * q;
    return rotA(axis, ang, true);
  }
  function rotA(axis, ang, round) {
    const [x, y, z] = axis;
    const c = Math.cos(ang), s = Math.sin(ang), t = 1 - c;
    let m = [
      [t * x * x + c, t * x * y - s * z, t * x * z + s * y],
      [t * x * y + s * z, t * y * y + c, t * y * z - s * x],
      [t * x * z - s * y, t * y * z + s * x, t * z * z + c],
    ];
    if (round) m = m.map((r) => r.map((v) => Math.round(v)));
    return m;
  }
  const mv = (m, v) => [dot(m[0], v), dot(m[1], v), dot(m[2], v)];
  const mm = (a, b) => a.map((row) => [0, 1, 2].map((j) => row[0] * b[0][j] + row[1] * b[1][j] + row[2] * b[2][j]));
  const I3 = [[1, 0, 0], [0, 1, 0], [0, 0, 1]];

  const AXIS = {};
  for (const f of FACES) AXIS[f] = FRAME[f][0];
  const ROT_AXIS = { x: AXIS.R, y: AXIS.U, z: AXIS.F };

  function parseMove(m) {
    const base = m[0];
    const q = m.endsWith("2") ? 2 : m.endsWith("'") ? 3 : 1;
    if ("xyz".includes(base)) return { axis: ROT_AXIS[base], q, layer: false };
    return { axis: AXIS[base], q, layer: true };
  }

  const PERM_CACHE = {};
  function movePerm(m) {
    if (PERM_CACHE[m]) return PERM_CACHE[m];
    const { axis, q, layer } = parseMove(m);
    const R = rotQ(axis, q);
    const perm = [...Array(54).keys()];
    GEOM.forEach((g, src) => {
      if (layer && dot(g.p, axis) <= 0) return;
      perm[INDEX[key(mv(R, g.p), mv(R, g.n))]] = src;
    });
    return (PERM_CACHE[m] = perm);
  }
  function applyMove(state, m) {
    const p = movePerm(m);
    let out = "";
    for (let i = 0; i < 54; i++) out += state[p[i]];
    return out;
  }

  // ------------------------------------------------------------------ view
  class CubeView {
    constructor(canvas, opts = {}) {
      this.canvas = canvas;
      this.ctx = canvas.getContext("2d");
      this.colors = opts.colors || "WWWWWWWWWBBBBBBBBBRRRRRRRRRYYYYYYYYYGGGGGGGGGOOOOOOOOO";
      // camera: yaw 45deg puts the robot front (between FRONT and RIGHT arm) to the viewer
      this.yaw = opts.yaw ?? -Math.PI / 4;
      this.pitch = opts.pitch ?? 0.55;
      this.zoom = 1;
      this.pose = I3;            // whole-cube orientation (robot flips / scan rolls)
      this.anim = null;          // current animation
      this.queue = [];
      this.highlight = null;     // facelet indices to outline
      this.labels = opts.labels ?? true;
      this.onStateChange = null;
      this._bindMouse();
      this._loop = this._loop.bind(this);
      requestAnimationFrame(this._loop);
    }

    setColors(c) {
      if (!this.anim && !this.queue.length) this.colors = c;
      else this._pendingColors = c;
    }

    // animate a face turn or whole-cube rotation; dur in ms
    turn(move, dur = 250) {
      this.queue.push({ kind: "move", move, dur });
    }
    setPose(rotations, dur = 400) {
      let M = I3;
      for (const r of rotations) M = mm(rotQ(ROT_AXIS[r[0]], parseMove(r).q), M);
      this.queue.push({ kind: "pose", target: M, dur });
    }
    flush() {
      while (this.queue.length || this.anim) this._finish();
    }
    _finish() {
      if (!this.anim) {
        if (!this.queue.length) return;
        this._start(this.queue.shift());
      }
      const a = this.anim;
      if (a.kind === "move") this.colors = applyMove(this.colors, a.move);
      else this.pose = a.target;
      this.anim = null;
      if (!this.queue.length && this._pendingColors) {
        this.colors = this._pendingColors;
        this._pendingColors = null;
      }
      this.onStateChange && this.onStateChange(this.colors);
    }
    _start(item) {
      item.t0 = performance.now();
      if (item.kind === "move") {
        const { axis, q, layer } = parseMove(item.move);
        item.axis = axis;
        item.angle = (q === 3 ? 1 : -1) * (q === 2 ? Math.PI : Math.PI / 2);
        item.layer = layer;
      } else item.from = this.pose;
      this.anim = item;
    }

    _bindMouse() {
      let drag = null;
      const c = this.canvas;
      const down = (x, y) => (drag = { x, y, yaw: this.yaw, pitch: this.pitch });
      const move = (x, y) => {
        if (!drag) return;
        this.yaw = drag.yaw - (x - drag.x) * 0.01;
        this.pitch = Math.max(-1.4, Math.min(1.4, drag.pitch + (y - drag.y) * 0.01));
      };
      c.addEventListener("mousedown", (e) => down(e.clientX, e.clientY));
      window.addEventListener("mousemove", (e) => move(e.clientX, e.clientY));
      window.addEventListener("mouseup", () => (drag = null));
      c.addEventListener("touchstart", (e) => down(e.touches[0].clientX, e.touches[0].clientY), { passive: true });
      c.addEventListener("touchmove", (e) => move(e.touches[0].clientX, e.touches[0].clientY), { passive: true });
      c.addEventListener("touchend", () => (drag = null));
      c.addEventListener("wheel", (e) => {
        e.preventDefault();
        this.zoom = Math.max(0.5, Math.min(1.8, this.zoom * (e.deltaY > 0 ? 0.92 : 1.08)));
      }, { passive: false });
      c.addEventListener("dblclick", () => { this.yaw = -Math.PI / 4; this.pitch = 0.55; this.zoom = 1; });
    }

    _loop(now) {
      requestAnimationFrame(this._loop);
      if (!this.anim && this.queue.length) this._start(this.queue.shift());
      let layerRot = null, layerAxis = null, poseM = this.pose;
      if (this.anim) {
        const a = this.anim;
        // speed up if a backlog builds up so the model never lags the robot
        const speed = 1 + this.queue.length * 0.75;
        let t = Math.min(1, ((now - a.t0) * speed) / Math.max(1, a.dur));
        const e = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
        if (a.kind === "move") {
          const R = rotA(a.axis, a.angle * e, false);
          if (a.layer) { layerRot = R; layerAxis = a.axis; }
          else poseM = mm(R, this.pose);
        } else {
          poseM = slerpM(a.from, a.target, e);
        }
        if (t >= 1) this._finish();
      }
      this._draw(layerRot, layerAxis, poseM);
    }

    _draw(layerRot, layerAxis, poseM) {
      const c = this.canvas, ctx = this.ctx;
      const dpr = window.devicePixelRatio || 1;
      const W = c.clientWidth, H = c.clientHeight;
      if (c.width !== Math.round(W * dpr) || c.height !== Math.round(H * dpr)) {
        c.width = Math.round(W * dpr); c.height = Math.round(H * dpr);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, W, H);
      const scale = Math.min(W, H) / 8.2 * this.zoom;
      const cy = Math.cos(this.yaw), sy = Math.sin(this.yaw);
      const cp = Math.cos(this.pitch), sp = Math.sin(this.pitch);
      const camDist = 14;
      const proj = (v) => {
        // yaw about y, then pitch about x
        let x = cy * v[0] + sy * v[2];
        let z = -sy * v[0] + cy * v[2];
        let y = cp * v[1] - sp * z;
        z = sp * v[1] + cp * z;
        const k = camDist / (camDist - z);
        return [W / 2 + x * scale * k, H / 2 - y * scale * k, z];
      };
      const viewDir = (n) => {
        // z of normal in camera space (positive = facing viewer)
        const z = -sy * n[0] + cy * n[2];
        return sp * n[1] + cp * z;
      };

      // build cubies
      const cubies = [];
      for (let x = -1; x <= 1; x++)
        for (let y = -1; y <= 1; y++)
          for (let z = -1; z <= 1; z++) {
            if (!x && !y && !z) continue;
            cubies.push({ c: [x, y, z], stickers: [] });
          }
      const cubieIdx = (c) => (c[0] + 1) * 9 + (c[1] + 1) * 3 + (c[2] + 1);
      const lookup = {};
      cubies.forEach((cb) => (lookup[cubieIdx(cb.c)] = cb));
      GEOM.forEach((g, i) => {
        const cc = [(g.p[0] - 3 * g.n[0]) / 2 + g.n[0], (g.p[1] - 3 * g.n[1]) / 2 + g.n[1], (g.p[2] - 3 * g.n[2]) / 2 + g.n[2]];
        lookup[cubieIdx(cc)].stickers.push({ i, g });
      });

      const xf = (cb, v) => {
        let w = v;
        if (layerRot && dot(cb.c, layerAxis) > 0) w = mv(layerRot, w);
        return mv(poseM, w);
      };
      const items = [];
      const h = 0.5, gap = 0.035;
      for (const cb of cubies) {
        const center = xf(cb, cb.c);
        const depth = proj(center)[2];
        const faces = [];
        // black body: 6 faces of the cubie
        for (const n of [[1,0,0],[-1,0,0],[0,1,0],[0,-1,0],[0,0,1],[0,0,-1]]) {
          const nn = xf(cb, n);
          if (viewDir(nn) <= 0.001) continue;
          const u = Math.abs(n[0]) ? [0, 1, 0] : [1, 0, 0];
          const w = [n[1]*u[2]-n[2]*u[1], n[2]*u[0]-n[0]*u[2], n[0]*u[1]-n[1]*u[0]];
          const ctr = add(cb.c, mul(h, n));
          const pts = [[-1,-1],[1,-1],[1,1],[-1,1]].map(([a, b]) =>
            proj(xf(cb, add(ctr, add(mul(a * (h - 0.0), u), mul(b * (h - 0.0), w))))));
          faces.push({ pts, fill: "#121212", light: viewDir(nn) });
        }
        for (const s of cb.stickers) {
          const nn = xf(cb, s.g.n);
          const vd = viewDir(nn);
          if (vd <= 0.001) continue;
          const ctr = add(cb.c, mul(h + 0.004, s.g.n));
          const r = s.g.r, d = s.g.d, k = h - gap - 0.04;
          const pts = [[-1,-1],[1,-1],[1,1],[-1,1]].map(([a, b]) =>
            proj(xf(cb, add(ctr, add(mul(a * k, r), mul(b * k, d))))));
          faces.push({ pts, fill: PALETTE[this.colors[s.i]] || "#555", light: vd, sticker: s.i });
        }
        items.push({ depth, faces });
      }
      items.sort((a, b) => a.depth - b.depth);
      for (const it of items)
        for (const f of it.faces) {
          ctx.beginPath();
          rounded(ctx, f.pts, f.sticker !== undefined ? 0.18 : 0.06);
          const shade = 0.55 + 0.45 * f.light;
          ctx.fillStyle = f.sticker !== undefined ? shadeColor(f.fill, shade) : f.fill;
          ctx.fill();
          if (f.sticker !== undefined) {
            if (this.highlight && this.highlight.includes(f.sticker)) {
              ctx.lineWidth = 3; ctx.strokeStyle = "#ffffff"; ctx.stroke();
            }
          }
        }
    }
  }

  function rounded(ctx, pts, r) {
    // polygon with softened corners
    const n = pts.length;
    for (let i = 0; i < n; i++) {
      const p0 = pts[(i + n - 1) % n], p1 = pts[i], p2 = pts[(i + 1) % n];
      const a = [p1[0] + (p0[0] - p1[0]) * r, p1[1] + (p0[1] - p1[1]) * r];
      const b = [p1[0] + (p2[0] - p1[0]) * r, p1[1] + (p2[1] - p1[1]) * r];
      if (i === 0) ctx.moveTo(a[0], a[1]); else ctx.lineTo(a[0], a[1]);
      ctx.quadraticCurveTo(p1[0], p1[1], b[0], b[1]);
    }
    ctx.closePath();
  }
  function shadeColor(hex, k) {
    const n = parseInt(hex.slice(1), 16);
    const r = Math.min(255, ((n >> 16) & 255) * k), g = Math.min(255, ((n >> 8) & 255) * k), b = Math.min(255, (n & 255) * k);
    return `rgb(${r | 0},${g | 0},${b | 0})`;
  }
  // matrix interpolation via axis-angle of A^T B
  function slerpM(A, B, t) {
    const At = [[A[0][0], A[1][0], A[2][0]], [A[0][1], A[1][1], A[2][1]], [A[0][2], A[1][2], A[2][2]]];
    const D = mm(B, At);
    const tr = D[0][0] + D[1][1] + D[2][2];
    let ang = Math.acos(Math.max(-1, Math.min(1, (tr - 1) / 2)));
    if (ang < 1e-6) return B;
    let ax = [D[2][1] - D[1][2], D[0][2] - D[2][0], D[1][0] - D[0][1]];
    let len = Math.hypot(...ax);
    if (len < 1e-6) {
      // 180 deg: axis from diagonal
      ax = [Math.sqrt(Math.max(0, (D[0][0] + 1) / 2)), Math.sqrt(Math.max(0, (D[1][1] + 1) / 2)), Math.sqrt(Math.max(0, (D[2][2] + 1) / 2))];
      if (D[0][1] < 0) ax[1] = -ax[1];
      if (D[0][2] < 0) ax[2] = -ax[2];
      len = Math.hypot(...ax);
    }
    ax = ax.map((v) => v / len);
    return mm(rotA(ax, ang * t, false), A);
  }

  global.Cube3D = { CubeView, applyMove, movePerm, PALETTE, FACES, GEOM };
})(window);
