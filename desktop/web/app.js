/* Cube Robot Studio - laptop UI */
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const COLORS = ["W", "Y", "R", "O", "B", "G"];
  const NAMES = { W: "white", Y: "yellow", R: "red", O: "orange", B: "blue", G: "green" };
  const OPP = { W: "Y", Y: "W", R: "O", O: "R", B: "G", G: "B" };
  const P = Cube3D.PALETTE;
  const FACE_LABEL = { U: "U · top", L: "L · left arm", F: "F · front arm", R: "R · right arm",
    B: "B · back arm", D: "D · bottom" };
  const NET_POS = { U: [0, 1], L: [1, 0], F: [1, 1], R: [1, 2], B: [1, 3], D: [2, 1] };

  let S = null;              // last server state
  let paint = "W";
  let base = null;           // colours the move track starts from
  let pbPos = 0;             // preview position in the solution
  let runMap = null;         // robot move index -> solution index while executing
  let running = false;

  const view = new Cube3D.CubeView($("cube3d"));

  // ------------------------------------------------------------------ api
  async function api(path, body) {
    const r = await fetch(path, { method: body === undefined ? "GET" : "POST",
      headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
    const j = await r.json();
    if (j.ok === false) { toast(j.error, true); throw new Error(j.error); }
    return j;
  }
  const post = (p, b = {}) => api(p, b);

  function toast(msg, err) {
    const t = $("toast");
    t.textContent = msg;
    t.className = "toast" + (err ? " err" : "");
    clearTimeout(toast._t);
    toast._t = setTimeout(() => t.classList.add("hidden"), 3500);
  }

  // ------------------------------------------------------------------ net
  function buildNet() {
    const net = $("net");
    net.innerHTML = "";
    for (const f of "URFDLB") {
      const [r, c] = NET_POS[f];
      const el = document.createElement("div");
      el.className = "face";
      el.style.gridRow = r + 1;
      el.style.gridColumn = c + 1;
      const lbl = document.createElement("span");
      lbl.className = "lbl";
      lbl.textContent = FACE_LABEL[f];
      el.appendChild(lbl);
      const fi = "URFDLB".indexOf(f);
      for (let k = 0; k < 9; k++) {
        const s = document.createElement("div");
        s.className = "st" + (k === 4 ? " ctr" : "");
        s.dataset.i = fi * 9 + k;
        s.dataset.f = f;
        s.addEventListener("click", () => onSticker(fi * 9 + k));
        el.appendChild(s);
      }
      net.appendChild(el);
    }
    const pal = $("palette");
    pal.innerHTML = "";
    for (const c of COLORS) {
      const s = document.createElement("div");
      s.className = "sw" + (c === paint ? " sel" : "");
      s.style.background = P[c];
      s.title = NAMES[c];
      s.onclick = () => { paint = c; buildNet(); renderNet(); };
      pal.appendChild(s);
    }
  }
  function renderNet() {
    if (!S) return;
    const cols = S.colors;
    document.querySelectorAll("#net .st").forEach((el) => {
      const i = +el.dataset.i;
      const c = cols[i];
      el.style.background = P[c] || "#555";
      el.classList.toggle("q", c === "?");
    });
  }
  function onSticker(i) {
    if (!S || i % 9 === 4 || S.busy_task) return;
    const cols = S.colors.split("");
    cols[i] = paint;
    S.colors = cols.join("");
    renderNet();
    view.setColors(S.colors);
    post("/api/cube", { colors: S.colors }).catch(() => {});
  }

  // ------------------------------------------------------------------ orientation pickers
  function renderPickers() {
    const fr = S.settings.front, tp = S.settings.top;
    for (const [id, keyName] of [["pick-front", "front"], ["pick-top", "top"]]) {
      const el = $(id);
      el.innerHTML = "";
      for (const c of COLORS) {
        const s = document.createElement("div");
        const other = keyName === "front" ? tp : fr;
        const bad = c === other || OPP[c] === other;
        s.className = "sw" + (S.settings[keyName] === c ? " sel" : "") + (bad ? " dis" : "");
        s.style.background = P[c];
        s.title = NAMES[c];
        s.onclick = () => {
          if (bad || S.busy_task) return;
          post("/api/settings", { [keyName]: c });
        };
        el.appendChild(s);
      }
    }
    const order = S.scan_order.map((c) => `<b>${NAMES[c]}</b>`).join(" → ");
    $("orient-hint").innerHTML =
      `Put <b>${NAMES[fr]}</b> in the front arm, <b>${NAMES[tp]}</b> on top. Scan order: ${order}.`;
  }

  // ------------------------------------------------------------------ scan steps
  function renderScanSteps() {
    const box = $("scan-steps");
    const sc = S.scan;
    box.innerHTML = "";
    S.scan_order.forEach((c, i) => {
      const d = document.createElement("div");
      const active = sc && sc.index === i && !sc.done;
      const done = (S.scanned || []).includes(c) && !active;
      d.className = "sstep" + (active ? " active" : "") + (done ? " done" : "");
      const thumb = S.thumbs[c];
      const phase = active ? sc.phase : done ? "scanned" : "pending";
      d.innerHTML = `<div class="sq" style="background:${thumb ? `url(data:image/jpeg;base64,${thumb})` : P[c]}"></div>
        ${i + 1}. ${NAMES[c]}<br><span class="small">${phase}</span>`;
      box.appendChild(d);
    });
  }

  // ------------------------------------------------------------------ track
  function renderTrack(curIdx = null) {
    const tr = $("track");
    tr.innerHTML = "";
    const sol = S && S.solution;
    if (!sol) {
      const info = S && S.solution_info;
      $("solinfo").textContent = info && info.error ? "⚠ " + info.error : info && info.done ? "Solved ✓" : "No solution yet";
      $("pb-pos").textContent = "";
      return;
    }
    sol.forEach((m, i) => {
      const s = document.createElement("div");
      const pos = curIdx === null ? pbPos : curIdx;
      s.className = "mv" + (i < pos ? " done" : "") + (i === pos ? " cur" : "");
      s.textContent = m;
      s.onclick = () => { if (!running) previewTo(i + 1); };
      tr.appendChild(s);
    });
    const inf = S.solution_info || {};
    $("solinfo").textContent = `${inf.moves} moves · ${inf.flips} cube flips · ≈${(inf.est_ms / 1000).toFixed(1)} s at ${S.settings.delay} ms` +
      (inf.candidates > 1 ? ` · best of ${inf.candidates} (robot-time)` : "");
    $("pb-pos").textContent = `${curIdx === null ? pbPos : curIdx} / ${sol.length}`;
  }
  function previewTo(n) {
    if (!S.solution || !base) return;
    n = Math.max(0, Math.min(S.solution.length, n));
    if (n === pbPos + 1) { view.turn(S.solution[pbPos], 260); }
    else if (n === pbPos - 1) { view.turn(inv(S.solution[n]), 260); }
    else {
      let c = base;
      for (let i = 0; i < n; i++) c = Cube3D.applyMove(c, S.solution[i]);
      view.flush();
      view.colors = c;
    }
    pbPos = n;
    renderTrack();
  }
  const inv = (m) => (m.endsWith("'") ? m[0] : m.endsWith("2") ? m : m + "'");

  // ------------------------------------------------------------------ robot panel
  function renderRobot() {
    const r = S.robot;
    const st = r.connected ? r.state : "OFFLINE";
    $("robot-state").textContent = S.busy_task ? `${st} · ${S.busy_task}` : st;
    $("robot-state").className = "big " + st;
    chip("chip-robot", r.connected ? (r.state === "BUSY" ? "busy" : "on") : "", r.connected ? `Robot: ${r.port}` : "Robot: offline");
    chip("chip-solver", S.solver_ready ? "on" : "busy", S.solver_ready ? "Solver ready" : "Solver: building tables…");
    const phones = (S.phones || []).filter((p) => Date.now() / 1000 - p.seen < 15);
    chip("chip-phone", phones.length ? "on" : "", phones.length ? `Phone: ${phones[0].name || "connected"}` : "Phone: none");
    $("btn-connect").textContent = r.connected ? "Disconnect" : "Connect";
    const idle = r.connected && r.state === "IDLE" && !S.busy_task;
    $("btn-open").disabled = !idle;
    $("btn-close").disabled = !idle;
    $("btn-scramble").disabled = !idle;
    $("btn-solve").disabled = !idle || !S.solution;
    $("btn-abort").disabled = !r.connected || (!S.busy_task && r.state !== "BUSY");
    $("btn-scan").disabled = !!S.busy_task;
    $("btn-scan").textContent = S.busy_task === "scan" ? "Scanning…" : r.connected ? "⦿ Scan with robot" : "⦿ Scan (manual, no robot)";
    $("btn-calc").disabled = !S.solver_ready || !!S.busy_task;
    if (document.activeElement !== $("speed")) $("speed").value = S.settings.delay;
    $("speed-val").textContent = S.settings.delay + " ms";
    $("auto-capture").checked = !!S.settings.auto_capture;
    $("pose-badge").textContent = S.pose === "INVERT" ? "flipped (U/D on arms)" : "home pose";
    const w = (S.warnings || []).map((x) => `<div class="warn">⚠ ${x}</div>`).join("");
    $("est").innerHTML = (S.solution_info && S.solution_info.robot_string
      ? `Arduino: <code>MOVE ${S.settings.delay} 0 ${S.solution_info.robot_string}</code>` : "") + w;
  }
  function chip(id, cls, txt) {
    const c = $(id);
    c.className = "chip " + cls;
    c.querySelector(".txt").textContent = txt;
  }

  // ------------------------------------------------------------------ plan editor
  async function loadPlan() {
    const plan = await api("/api/plan");
    const box = $("plan");
    box.innerHTML = "";
    plan.steps.forEach((st, i) => {
      const d = document.createElement("div");
      d.className = "planstep";
      d.innerHTML = `<b>${i + 1}. ${st.id}</b> <span class="muted">(${st.axis} axis, view ${st.view.join(" ")})</span>
        <div class="r"><input data-k="present" value="${st.present}"><button data-j="present">Jog</button></div>
        <div class="r"><input data-k="revert" value="${st.revert}"><button data-j="revert">Jog</button></div>
        <div class="r small">image rotation <select data-k="image_rot">${[0, 1, 2, 3].map((q) =>
          `<option value="${q}" ${q == (st.image_rot || 0) ? "selected" : ""}>${q * 90}°</option>`).join("")}</select></div>`;
      d.querySelectorAll("button[data-j]").forEach((b) => (b.onclick = () =>
        post("/api/robot/jog", { seq: d.querySelector(`input[data-k=${b.dataset.j}]`).value })));
      box.appendChild(d);
    });
    $("plan-save").onclick = async () => {
      box.querySelectorAll(".planstep").forEach((d, i) => {
        plan.steps[i].present = d.querySelector("input[data-k=present]").value.trim();
        plan.steps[i].revert = d.querySelector("input[data-k=revert]").value.trim();
        plan.steps[i].image_rot = +d.querySelector("select[data-k=image_rot]").value;
      });
      await post("/api/plan", plan);
      toast("Scan plan saved");
    };
  }

  // ------------------------------------------------------------------ events
  function onState(st) {
    const prevColors = S && S.colors;
    const first = !S;
    S = st;
    renderPickers();
    renderNet();
    renderScanSteps();
    renderRobot();
    renderPair();
    if (first && st.pose === "INVERT") view.setPose(["z'"], 1);
    if (!running) {
      if (first || st.colors !== prevColors) {
        view.setColors(st.colors);
        base = st.colors;
        pbPos = 0;
      }
      renderTrack();
    }
    const sc = st.scan;
    const ban = $("scan-banner");
    if (sc && !sc.done) {
      ban.classList.remove("hidden");
      const ph = { moving: "robot presenting", capture: "📷 capture on phone", captured: "checking…",
        confirm: "", confirmed: "turning back", reverting: "turning back" }[sc.phase] ?? sc.phase;
      const head = `Scanning ${NAMES[sc.color]} (${sc.index + 1}/${sc.total})`;
      if (sc.phase === "confirm") {
        if (ban.dataset.mode !== "confirm" + sc.index) {
          ban.dataset.mode = "confirm" + sc.index;
          ban.innerHTML = `${head} — check the colours in the net
            <button class="ghost" id="scan-retake">↺ Retake</button>
            <button class="ok" id="scan-confirm">Confirm ✓</button>`;
          $("scan-retake").onclick = () => post("/api/scan/retake");
          $("scan-confirm").onclick = () => post("/api/scan/confirm");
        }
      } else {
        ban.dataset.mode = "";
        ban.textContent = `${head} — ${ph}`;
      }
      view.highlight = sc.cells || null;
      if (sc.view && JSON.stringify(sc.view) !== view._scanView) {
        view._scanView = JSON.stringify(sc.view);
        view.setPose(sc.view, 450);
      }
    } else {
      ban.classList.add("hidden");
      view.highlight = null;
      if (view._scanView) { view._scanView = null; view.setPose([], 450); }
    }
  }

  function connectEvents() {
    const es = new EventSource("/api/events");
    es.onmessage = (m) => {
      const ev = JSON.parse(m.data);
      switch (ev.type) {
        case "state": onState(ev); break;
        case "log": log(ev.msg, ev.level); break;
        case "run": {
          running = true;
          runMap = [];
          ev.moves.forEach((mv, i) => { runMap.push(i); if (mv.endsWith("2")) runMap.push(i); });
          if (ev.solve) renderTrack(0);
          break;
        }
        case "move":
          view.turn(ev.move, Math.max(120, ev.dur));
          if (runMap && S.solution) renderTrack(runMap[ev.index] ?? null);
          S.colors = ev.colors;
          renderNet();
          break;
        case "pose":
          view.setPose(ev.pose === "INVERT" ? ["z'"] : [], ev.dur * 1.2);
          S.pose = ev.pose;
          $("pose-badge").textContent = ev.pose === "INVERT" ? "flipped (U/D on arms)" : "home pose";
          break;
        case "run_done":
          running = false;
          runMap = null;
          view.setColors(S.colors);
          toast(ev.solve ? "Cube solved ✓ — back in home pose" : "Scramble done");
          break;
        case "scan_done": toast("Scan complete"); break;
        case "scan_prompt": break;
      }
    };
    es.onerror = () => chip("chip-robot", "", "Server offline…");
  }

  function log(msg, level = "info") {
    const el = $("log");
    const d = document.createElement("div");
    d.className = level;
    d.textContent = new Date().toLocaleTimeString() + "  " + msg;
    el.prepend(d);
    while (el.childNodes.length > 300) el.lastChild.remove();
  }

  async function refreshPorts() {
    const ports = await api("/api/ports");
    const sel = $("port");
    const prev = sel.value;
    sel.innerHTML = ports.map((p) => `<option value="${p.device}">${p.device}${p.arduino ? " (Arduino)" : ""} — ${p.description}</option>`).join("");
    const ard = ports.find((p) => p.arduino);
    sel.value = prev && ports.some((p) => p.device === prev) ? prev : ard ? ard.device : ports[0].device;
  }

  // ------------------------------------------------------------------ wiring
  $("btn-refresh").onclick = refreshPorts;
  $("btn-connect").onclick = () => (S.robot.connected ? post("/api/disconnect") :
    post("/api/connect", { port: $("port").value }).then(() => toast("Connected")));
  $("btn-open").onclick = () => post("/api/robot/open");
  $("btn-close").onclick = () => post("/api/robot/close");
  $("btn-abort").onclick = () => post("/api/robot/abort");
  $("btn-scramble").onclick = () => post("/api/scramble", { n: 22 });
  $("btn-solve").onclick = () => post("/api/execute");
  $("btn-calc").onclick = () => post("/api/solve").then(() => toast("Solution ready"));
  $("btn-scan").onclick = () => post("/api/scan/start");
  $("btn-reset").onclick = () => post("/api/reset");
  $("speed").oninput = (e) => ($("speed-val").textContent = e.target.value + " ms");
  $("speed").onchange = (e) => post("/api/settings", { delay: +e.target.value });
  $("auto-capture").onchange = (e) => post("/api/settings", { auto_capture: e.target.checked });
  $("pb-first").onclick = () => previewTo(0);
  $("pb-prev").onclick = () => previewTo(pbPos - 1);
  $("pb-next").onclick = () => previewTo(pbPos + 1);
  $("pb-last").onclick = () => previewTo(S.solution ? S.solution.length : 0);
  $("plan-reset").onclick = () => post("/api/plan/reset").then(loadPlan);
  $("jog-go").onclick = () => post("/api/robot/jog", { seq: $("jog-free").value });
  document.addEventListener("keydown", (e) => {
    if (!S || !S.scan || S.scan.phase !== "confirm" || e.target.tagName === "INPUT") return;
    if (e.key === "Enter") post("/api/scan/confirm");
    else if (e.key.toLowerCase() === "r") post("/api/scan/retake");
  });
  $("btn-theme").onclick = () => document.body.classList.toggle("light");
  $("btn-quit").onclick = () => {
    if (confirm("Quit Cube Robot Studio? (the robot link closes)")) post("/api/quit").then(() => window.close());
  };

  buildNet();
  refreshPorts();
  loadPlan();
  let INFO = null;
  function renderPair() {
    if (!INFO || !S || !S.pair_code) return;
    if (renderPair._code === S.pair_code) return;
    renderPair._code = S.pair_code;
    const c = S.pair_code;
    $("pair-code").textContent = c.slice(0, 3) + " " + c.slice(3);
    const url = `cuberobot://pair?code=${c}&port=${INFO.port}&hosts=${INFO.ips.join(",")}` +
      `&name=${encodeURIComponent(INFO.name || "laptop")}`;
    try { QR.draw($("qr"), url, { size: 132, dark: "#0600AB" }); } catch (e) { log("QR: " + e.message, "error"); }
  }
  api("/api/info").then((i) => {
    INFO = i;
    $("ips").textContent = i.ips.map((ip) => `${ip}:${i.port}`).join(", ") || "no network";
    renderPair();
  });
  $("pair-new").onclick = () => post("/api/pair/new");
  $("chip-phone").onclick = () => { $("phone-box").open = true; $("phone-box").scrollIntoView({ behavior: "smooth" }); };
  connectEvents();
})();
