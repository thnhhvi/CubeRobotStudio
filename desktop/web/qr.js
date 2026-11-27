/* Minimal QR code generator (byte mode, ECC level M, versions 1-10).
 * Follows the structure of Nayuki's reference implementation (MIT).
 * QR.matrix(text) -> array of rows of booleans;  QR.draw(canvas, text, opts) */
(function (global) {
  "use strict";
  // index = version (1..10), ECC level M
  const ECC_PER_BLOCK = [-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26];
  const NUM_BLOCKS = [-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5];
  const ECL_FORMAT_BITS = 0; // M

  function rawModules(ver) {
    let r = (16 * ver + 128) * ver + 64;
    if (ver >= 2) {
      const na = Math.floor(ver / 7) + 2;
      r -= (25 * na - 10) * na - 55;
      if (ver >= 7) r -= 36;
    }
    return r;
  }
  const dataCodewords = (ver) => Math.floor(rawModules(ver) / 8) - ECC_PER_BLOCK[ver] * NUM_BLOCKS[ver];

  function gfMul(x, y) {
    let z = 0;
    for (let i = 7; i >= 0; i--) {
      z = (z << 1) ^ ((z >>> 7) * 0x11d);
      z ^= ((y >>> i) & 1) * x;
    }
    return z & 0xff;
  }
  function rsDivisor(degree) {
    const r = new Array(degree).fill(0);
    r[degree - 1] = 1;
    let root = 1;
    for (let i = 0; i < degree; i++) {
      for (let j = 0; j < r.length; j++) {
        r[j] = gfMul(r[j], root);
        if (j + 1 < r.length) r[j] ^= r[j + 1];
      }
      root = gfMul(root, 0x02);
    }
    return r;
  }
  function rsRemainder(data, div) {
    const r = div.map(() => 0);
    for (const b of data) {
      const f = b ^ r.shift();
      r.push(0);
      div.forEach((c, i) => (r[i] ^= gfMul(c, f)));
    }
    return r;
  }

  function matrix(text) {
    const bytes = Array.from(new TextEncoder().encode(text));
    let ver = 1;
    for (; ver <= 10; ver++) if (4 + 8 + bytes.length * 8 <= dataCodewords(ver) * 8) break;
    if (ver > 10) throw new Error("QR text too long");
    // ---- data bits
    const bits = [];
    const put = (v, n) => { for (let i = n - 1; i >= 0; i--) bits.push((v >>> i) & 1); };
    put(4, 4);
    put(bytes.length, 8);
    bytes.forEach((b) => put(b, 8));
    const cap = dataCodewords(ver) * 8;
    put(0, Math.min(4, cap - bits.length));
    put(0, (8 - (bits.length % 8)) % 8);
    for (let p = 0xec; bits.length < cap; p ^= 0xec ^ 0x11) put(p, 8);
    const data = [];
    for (let i = 0; i < bits.length; i += 8) data.push(parseInt(bits.slice(i, i + 8).join(""), 2));
    // ---- error correction + interleave
    const nb = NUM_BLOCKS[ver], ecl = ECC_PER_BLOCK[ver];
    const raw = Math.floor(rawModules(ver) / 8);
    const nShort = nb - (raw % nb), shortLen = Math.floor(raw / nb);
    const div = rsDivisor(ecl);
    const blocks = [];
    for (let i = 0, k = 0; i < nb; i++) {
      const dat = data.slice(k, k + shortLen - ecl + (i < nShort ? 0 : 1));
      k += dat.length;
      const ecc = rsRemainder(dat, div);
      if (i < nShort) dat.push(0);
      blocks.push(dat.concat(ecc));
    }
    const all = [];
    for (let i = 0; i < blocks[0].length; i++)
      blocks.forEach((b, j) => { if (i !== shortLen - ecl || j >= nShort) all.push(b[i]); });

    // ---- modules
    const size = ver * 4 + 17;
    const M = Array.from({ length: size }, () => new Array(size).fill(false));
    const F = Array.from({ length: size }, () => new Array(size).fill(false));
    const set = (x, y, d) => { M[y][x] = d; F[y][x] = true; };
    for (let i = 0; i < size; i++) { set(6, i, i % 2 === 0); set(i, 6, i % 2 === 0); }
    const finder = (x, y) => {
      for (let dy = -4; dy <= 4; dy++)
        for (let dx = -4; dx <= 4; dx++) {
          const d = Math.max(Math.abs(dx), Math.abs(dy)), xx = x + dx, yy = y + dy;
          if (xx >= 0 && xx < size && yy >= 0 && yy < size) set(xx, yy, d !== 2 && d !== 4);
        }
    };
    finder(3, 3); finder(size - 4, 3); finder(3, size - 4);
    const align = [];
    if (ver > 1) {
      const na = Math.floor(ver / 7) + 2;
      const step = Math.ceil((ver * 4 + 4) / (na * 2 - 2)) * 2;
      align.push(6);
      for (let pos = size - 7; align.length < na; pos -= step) align.splice(1, 0, pos);
    }
    align.forEach((ax, i) => align.forEach((ay, j) => {
      if ((i === 0 && j === 0) || (i === 0 && j === align.length - 1) || (i === align.length - 1 && j === 0)) return;
      for (let dy = -2; dy <= 2; dy++)
        for (let dx = -2; dx <= 2; dx++) set(ax + dx, ay + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    }));
    const drawFormat = (mask) => {
      const d = (ECL_FORMAT_BITS << 3) | mask;
      let rem = d;
      for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
      const b = ((d << 10) | rem) ^ 0x5412;
      const g = (i) => ((b >>> i) & 1) !== 0;
      for (let i = 0; i <= 5; i++) set(8, i, g(i));
      set(8, 7, g(6)); set(8, 8, g(7)); set(7, 8, g(8));
      for (let i = 9; i < 15; i++) set(14 - i, 8, g(i));
      for (let i = 0; i < 8; i++) set(size - 1 - i, 8, g(i));
      for (let i = 8; i < 15; i++) set(8, size - 15 + i, g(i));
      set(8, size - 8, true);
    };
    drawFormat(0);
    if (ver >= 7) {
      let rem = ver;
      for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
      const b = (ver << 12) | rem;
      for (let i = 0; i < 18; i++) {
        const bit = ((b >>> i) & 1) !== 0, a = size - 11 + (i % 3), c = Math.floor(i / 3);
        set(a, c, bit); set(c, a, bit);
      }
    }
    // zig-zag codewords
    let i = 0;
    for (let right = size - 1; right >= 1; right -= 2) {
      if (right === 6) right = 5;
      for (let vert = 0; vert < size; vert++)
        for (let j = 0; j < 2; j++) {
          const x = right - j, up = ((right + 1) & 2) === 0, y = up ? size - 1 - vert : vert;
          if (!F[y][x] && i < all.length * 8) { M[y][x] = ((all[i >>> 3] >>> (7 - (i & 7))) & 1) !== 0; i++; }
        }
    }
    // masks: pick the lowest simple penalty
    const maskFn = [
      (x, y) => (x + y) % 2 === 0, (x, y) => y % 2 === 0, (x) => x % 3 === 0, (x, y) => (x + y) % 3 === 0,
      (x, y) => (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0, (x, y) => ((x * y) % 2) + ((x * y) % 3) === 0,
      (x, y) => (((x * y) % 2) + ((x * y) % 3)) % 2 === 0, (x, y) => (((x + y) % 2) + ((x * y) % 3)) % 2 === 0,
    ];
    const applyMask = (m) => {
      for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) if (!F[y][x] && maskFn[m](x, y)) M[y][x] = !M[y][x];
    };
    let best = 0, bestP = Infinity;
    for (let m = 0; m < 8; m++) {
      applyMask(m); drawFormat(m);
      const p = penalty(M, size);
      if (p < bestP) { bestP = p; best = m; }
      applyMask(m);
    }
    applyMask(best); drawFormat(best);
    return M;
  }

  function penalty(M, n) {
    let p = 0, dark = 0;
    for (let a = 0; a < n; a++) {
      let runR = 1, runC = 1;
      for (let b = 1; b < n; b++) {
        if (M[a][b] === M[a][b - 1]) { runR++; if (runR === 5) p += 3; else if (runR > 5) p++; } else runR = 1;
        if (M[b][a] === M[b - 1][a]) { runC++; if (runC === 5) p += 3; else if (runC > 5) p++; } else runC = 1;
      }
    }
    for (let y = 0; y < n - 1; y++)
      for (let x = 0; x < n - 1; x++) {
        const c = M[y][x];
        if (c === M[y][x + 1] && c === M[y + 1][x] && c === M[y + 1][x + 1]) p += 3;
      }
    M.forEach((r) => r.forEach((c) => (dark += c ? 1 : 0)));
    p += Math.floor(Math.abs(dark * 20 - n * n * 10) / (n * n)) * 10;
    return p;
  }

  function draw(canvas, text, opts = {}) {
    const M = matrix(text), n = M.length, quiet = 3;
    const px = opts.size || 180;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = canvas.height = px * dpr;
    canvas.style.width = canvas.style.height = px + "px";
    const ctx = canvas.getContext("2d");
    const s = (px * dpr) / (n + quiet * 2);
    ctx.fillStyle = opts.light || "#fff";
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = opts.dark || "#000";
    for (let y = 0; y < n; y++)
      for (let x = 0; x < n; x++)
        if (M[y][x]) ctx.fillRect(Math.floor((x + quiet) * s), Math.floor((y + quiet) * s), Math.ceil(s), Math.ceil(s));
  }

  global.QR = { matrix, draw };
})(typeof window !== "undefined" ? window : globalThis);
