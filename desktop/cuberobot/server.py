"""HTTP + Server-Sent-Events API used by the laptop UI and the Android app."""
from __future__ import annotations

import json
import mimetypes
import os
import shutil
import socket
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

from .controller import Controller
from .robot import SerialLink

PORT = 8765
DISCOVERY_PORT = 47800


def web_root():
    base = getattr(sys, "_MEIPASS", os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    return os.path.join(base, "web")


def local_ips():
    ips = set()
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("10.255.255.255", 1))
        ips.add(s.getsockname()[0])
        s.close()
    except Exception:
        pass
    try:
        for info in socket.getaddrinfo(socket.gethostname(), None, socket.AF_INET):
            ips.add(info[4][0])
    except Exception:
        pass
    ips.discard("127.0.0.1")
    return sorted(ips)


class Handler(BaseHTTPRequestHandler):
    ctl: Controller = None
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):  # quiet
        pass

    # ---------------------------------------------------------------- helpers
    def _json(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def _body(self):
        n = int(self.headers.get("Content-Length") or 0)
        if not n:
            return {}
        return json.loads(self.rfile.read(n).decode("utf-8") or "{}")

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.send_header("Content-Length", "0")
        self.end_headers()

    # ---------------------------------------------------------------- GET
    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/api/events":
            return self._sse()
        if path == "/api/state":
            return self._json(self.ctl.snapshot())
        if path == "/api/ports":
            return self._json(SerialLink.list_ports())
        if path == "/api/plan":
            return self._json(self.ctl.plan)
        if path == "/api/info":
            return self._json({"app": "CubeRobotStudio", "port": PORT, "ips": local_ips()})
        if path in ("/", "/index.html"):
            path = "/index.html"
        elif path in ("/phone", "/phone/"):
            path = "/phone.html"
        fp = os.path.normpath(os.path.join(web_root(), path.lstrip("/")))
        if not fp.startswith(web_root()) or not os.path.isfile(fp):
            return self._json({"error": "not found"}, 404)
        with open(fp, "rb") as f:
            body = f.read()
        self.send_response(200)
        self.send_header("Content-Type", mimetypes.guess_type(fp)[0] or "application/octet-stream")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        self.wfile.write(body)

    def _sse(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        q = self.ctl.bus.subscribe()
        try:
            self._sse_send(self.ctl.snapshot())
            while True:
                try:
                    ev = q.get(timeout=10)
                except Exception:
                    self.wfile.write(b": ping\n\n")
                    self.wfile.flush()
                    continue
                self._sse_send(ev)
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError, OSError):
            pass
        finally:
            self.ctl.bus.unsubscribe(q)

    def _sse_send(self, ev):
        self.wfile.write(b"data: " + json.dumps(ev).encode() + b"\n\n")
        self.wfile.flush()

    # ---------------------------------------------------------------- POST
    def do_POST(self):
        path = urlparse(self.path).path
        c = self.ctl
        try:
            data = self._body()
            routes = {
                "/api/connect": lambda: c.connect(data["port"]),
                "/api/disconnect": c.disconnect,
                "/api/robot/open": c.open_arms,
                "/api/robot/close": c.close_arms,
                "/api/robot/abort": c.abort,
                "/api/robot/jog": lambda: c.jog(data["seq"]),
                "/api/settings": lambda: c.update_settings(data),
                "/api/cube": lambda: c.set_colors(data["colors"]),
                "/api/reset": c.reset,
                "/api/solve": c.solve,
                "/api/execute": c.execute_solution,
                "/api/scramble": lambda: c.scramble(int(data.get("n", 22))),
                "/api/scan/start": c.start_scan,
                "/api/scan/capture": lambda: c.capture(data),
                "/api/scan/cancel": c.cancel_scan,
                "/api/plan": lambda: c.set_plan(data),
                "/api/plan/reset": c.reset_plan,
                "/api/phone/hello": lambda: c.phone_hello(data),
            }
            fn = routes.get(path)
            if fn is None:
                return self._json({"error": "unknown endpoint"}, 404)
            res = fn()
            return self._json({"ok": True, "result": res})
        except Exception as e:
            return self._json({"ok": False, "error": str(e)}, 400)


def _beacon(stop):
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
    name = socket.gethostname()
    while not stop.is_set():
        msg = json.dumps({"app": "CubeRobotStudio", "port": PORT, "name": name,
                          "ips": local_ips()}).encode()
        for target in ("255.255.255.255", "<broadcast>"):
            try:
                s.sendto(msg, (target, DISCOVERY_PORT))
                break
            except Exception:
                continue
        stop.wait(2.0)


def find_adb():
    exe = "adb.exe" if os.name == "nt" else "adb"
    p = shutil.which(exe)
    if p:
        return p
    cands = []
    if os.name == "nt":
        la = os.environ.get("LOCALAPPDATA", "")
        cands.append(os.path.join(la, "Android", "Sdk", "platform-tools", exe))
    cands.append(os.path.join(os.path.expanduser("~"), "Android", "Sdk", "platform-tools", exe))
    base = getattr(sys, "_MEIPASS", os.path.dirname(os.path.abspath(__file__)))
    cands.append(os.path.join(base, "platform-tools", exe))
    for c in cands:
        if os.path.isfile(c):
            return c
    return None


def _adb_reverse(stop, ctl):
    """USB link: forward the phone's localhost:8765 to this laptop (adb reverse)."""
    adb = find_adb()
    if not adb:
        return
    flags = 0x08000000 if os.name == "nt" else 0  # CREATE_NO_WINDOW
    known = set()
    while not stop.is_set():
        try:
            out = subprocess.run([adb, "devices"], capture_output=True, text=True,
                                 timeout=5, creationflags=flags).stdout
            devs = [l.split()[0] for l in out.splitlines()[1:] if l.strip().endswith("device")]
            for d in devs:
                if d not in known:
                    subprocess.run([adb, "-s", d, "reverse", f"tcp:{PORT}", f"tcp:{PORT}"],
                                   capture_output=True, timeout=5, creationflags=flags)
                    ctl.log(f"USB phone {d}: adb reverse tcp:{PORT} active")
            known = set(devs)
        except Exception:
            pass
        stop.wait(3.0)


def serve(ctl: Controller, host="0.0.0.0", port=PORT):
    Handler.ctl = ctl
    httpd = ThreadingHTTPServer((host, port), Handler)
    httpd.daemon_threads = True
    stop = threading.Event()
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    threading.Thread(target=_beacon, args=(stop,), daemon=True).start()
    threading.Thread(target=_adb_reverse, args=(stop, ctl), daemon=True).start()
    return httpd, stop
