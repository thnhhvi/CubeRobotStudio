package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

const (
	httpPort      = 8765
	discoveryPort = 47800
)

func localIPs() []string {
	out := []string{}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func newMux(c *Controller) *http.ServeMux {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if r.URL.Path == "/phone" || r.URL.Path == "/phone/" {
			r.URL.Path = "/phone.html"
		}
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no streaming", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		ch := c.bus.Subscribe()
		defer c.bus.Unsubscribe(ch)
		snap, _ := json.Marshal(c.Snapshot())
		w.Write([]byte("data: "))
		w.Write(snap)
		w.Write([]byte("\n\n"))
		fl.Flush()
		ping := time.NewTicker(10 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return
				}
				fl.Flush()
			case data := <-ch:
				w.Write([]byte("data: "))
				w.Write(data)
				if _, err := w.Write([]byte("\n\n")); err != nil {
					return
				}
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, c.Snapshot()) })
	mux.HandleFunc("/api/ports", func(w http.ResponseWriter, r *http.Request) {
		p := listSerialPorts()
		p = append(p, map[string]any{"device": "SIMULATOR", "description": "Built-in robot simulator", "arduino": false})
		writeJSON(w, 200, p)
	})
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		host, _ := os.Hostname()
		writeJSON(w, 200, map[string]any{"app": "CubeRobotStudio", "port": httpPort, "ips": localIPs(),
			"name": host, "code": c.PairCode(), "version": AppVersion})
	})
	mux.HandleFunc("/api/plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			c.mu.Lock()
			p := c.plan
			c.mu.Unlock()
			writeJSON(w, 200, p)
			return
		}
		var p ScanPlan
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		reply(w, c.SetPlan(p))
	})
	post := func(path string, fn func(map[string]any) error) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.WriteHeader(204)
				return
			}
			body := map[string]any{}
			json.NewDecoder(r.Body).Decode(&body)
			reply(w, fn(body))
		})
	}
	post("/api/connect", func(b map[string]any) error { p, _ := b["port"].(string); return c.Connect(p) })
	post("/api/disconnect", func(map[string]any) error { c.Disconnect(); return nil })
	post("/api/robot/open", func(map[string]any) error { return c.OpenArms() })
	post("/api/robot/close", func(map[string]any) error { return c.CloseArms() })
	post("/api/robot/abort", func(map[string]any) error { c.Abort(); return nil })
	post("/api/robot/jog", func(b map[string]any) error { s, _ := b["seq"].(string); return c.Jog(s) })
	post("/api/settings", func(b map[string]any) error { return c.UpdateSettings(b) })
	post("/api/cube", func(b map[string]any) error { s, _ := b["colors"].(string); return c.SetColors(s) })
	post("/api/reset", func(map[string]any) error { return c.Reset() })
	post("/api/solve", func(map[string]any) error { return c.Solve() })
	post("/api/execute", func(map[string]any) error { return c.Execute() })
	post("/api/scramble", func(b map[string]any) error {
		n := int(toFloat(b["n"]))
		if n <= 0 {
			n = 22
		}
		return c.Scramble(n)
	})
	post("/api/scan/start", func(map[string]any) error { return c.StartScan() })
	post("/api/scan/capture", func(b map[string]any) error { return c.Capture(b) })
	post("/api/scan/cancel", func(map[string]any) error { c.CancelScan(); return nil })
	post("/api/scan/begin", func(map[string]any) error { return c.BeginScan() })
	post("/api/scan/trigger", func(map[string]any) error { return c.TriggerCapture() })
	post("/api/scan/confirm", func(map[string]any) error { return c.ConfirmScan("confirm") })
	post("/api/scan/retake", func(map[string]any) error { return c.ConfirmScan("retake") })
	post("/api/plan/reset", func(map[string]any) error { return c.SetPlan(defaultPlan()) })
	post("/api/phone/hello", func(b map[string]any) error { c.PhoneHello(b); return nil })
	post("/api/pair", func(b map[string]any) error { s, _ := b["code"].(string); return c.Pair(s) })
	post("/api/pair/new", func(map[string]any) error { c.NewPairCode(); c.PushState(); return nil })
	post("/api/quit", func(map[string]any) error {
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
		return nil
	})
	return mux
}

func reply(w http.ResponseWriter, err error) {
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func beacon(c *Controller) {
	addr := &net.UDPAddr{IP: net.IPv4bcast, Port: discoveryPort}
	host, _ := os.Hostname()
	for {
		if conn, err := net.DialUDP("udp4", nil, addr); err == nil {
			msg, _ := json.Marshal(map[string]any{"app": "CubeRobotStudio", "port": httpPort, "name": host,
				"ips": localIPs(), "code": c.PairCode()})
			conn.Write(msg)
			conn.Close()
		}
		time.Sleep(2 * time.Second)
	}
}

func findAdb() string {
	exe := "adb"
	if runtime.GOOS == "windows" {
		exe = "adb.exe"
	}
	if p, err := exec.LookPath(exe); err == nil {
		return p
	}
	cands := []string{}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		cands = append(cands, filepath.Join(la, "Android", "Sdk", "platform-tools", exe))
	}
	if h, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(h, "Android", "Sdk", "platform-tools", exe))
	}
	if ex, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(ex), "platform-tools", exe))
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// USB: make the phone's localhost:8765 reach this laptop (APK connects to 127.0.0.1)
func adbReverseLoop(c *Controller) {
	adb := findAdb()
	if adb == "" {
		return
	}
	known := map[string]bool{}
	for {
		cmd := exec.Command(adb, "devices")
		hideWindow(cmd)
		out, err := cmd.Output()
		if err == nil {
			now := map[string]bool{}
			for _, line := range strings.Split(string(out), "\n")[1:] {
				f := strings.Fields(line)
				if len(f) == 2 && f[1] == "device" {
					now[f[0]] = true
					if !known[f[0]] {
						r := exec.Command(adb, "-s", f[0], "reverse", "tcp:8765", "tcp:8765")
						hideWindow(r)
						r.Run()
						c.logf("info", "USB phone %s: adb reverse tcp:%d active", f[0], httpPort)
					}
				}
			}
			known = now
		}
		time.Sleep(3 * time.Second)
	}
}
