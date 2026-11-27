package main

// Scan plan (port of cuberobot/scan.py). Default for RED front / WHITE top:
// green, yellow, blue, white (front/back-arm rolls), then red, orange
// (left/right-arm rolls), then back to red front / white top.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ScanStep struct {
	ID       string   `json:"id"`
	Axis     string   `json:"axis"`
	Present  string   `json:"present"`
	Revert   string   `json:"revert"`
	View     []string `json:"view"`
	Keep     []string `json:"keep"`
	ImageRot int      `json:"image_rot"`
}

type ScanPlan struct {
	Version    int        `json:"version"`
	CameraFace string     `json:"camera_face"`
	Steps      []ScanStep `json:"steps"`
}

func defaultPlan() ScanPlan {
	// Sliders: L = clamped, C = touching, R = released. Spinners: L -90, C 0 (vertical), R +90.
	// {d} = spinner wait, {s} = slider wait (a spinner only moves after the sliders stopped).
	// Each face is simply rolled straight up to the camera: the rolling pair pre-turns its
	// free grippers, clamps, the other pair lets go, the grippers turn back to vertical
	// (rolling the cube 90 deg). Scan pose: every gripper vertical, front/back hold at C,
	// left/right wait released at R. At every moment one opposite pair holds the cube.
	a := func(id string) ScanStep {
		return ScanStep{ID: id, Axis: "front/back",
			// front/back roll the next face up (z), then ease to C; left/right stay at R
			Present: "FRBR{s}fLbR{d}FLBL{s}RRLR{s}fCfCbCbC{d}FCBC{s}",
			// clamp front/back, then left/right: home pose with the roll kept
			Revert: "FLBL{s}RLLL{s}",
			View:   []string{"z"}, Keep: []string{"z"}}
	}
	// left/right roll red (front) or orange (back) up; front/back take over at C and
	// left/right let go, so the scan pose is the same as for the other faces.
	b := func(id, pre, back, view string) ScanStep {
		return ScanStep{ID: id, Axis: "left/right",
			Present: "RRLR{s}" + pre + "{d}RLLL{s}FRBR{s}rCrClClC{d}FCBC{s}RRLR{s}",
			Revert:  "FLBL{s}" + back + "{d}RLLL{s}FRBR{s}rCrClClC{d}FLBL{s}",
			View:    []string{view}, Keep: []string{}}
	}
	return ScanPlan{CameraFace: "U", Version: 5, Steps: []ScanStep{
		a("A1"), a("A2"), a("A3"), a("A4"),
		b("B1", "rLlR", "rRlL", "x"),  // front (red) -> top
		b("B2", "rRlL", "rLlR", "x'"), // back (orange) -> top
	}}
}

// Second look at a face when grippers hide stickers: left/right ease in to C, then
// front/back let go (the cube is never free); and back again before the revert.
const (
	handOffSeq  = "RCLC{s}FRBR{s}"
	handBackSeq = "FCBC{s}RRLR{s}"
)

// Older default strings (v1: sliders at C, v2: diagonal grippers) are replaced by the
// current defaults; steps the user edited are kept.
var oldDefaults = map[string]bool{
	"RLLLFRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}":     true,
	"fCfCbCbC{d}RCLC{d}FCBC":                      true,
	"fCfCbCbC{d}RLLL{d}":                          true,
	"RLLLFRBR{d}rrll{d}":                          true,
	"rCrClClC{d}FCBC{d}RCLC":                      true,
	"rCrClClC{d}FLBL{d}":                          true,
	"FLBLRRLR{d}rRlL{d}RLLL{d}FRBR{d}rllr{d}":     true,
	"rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RCLC{d}FCBC": true,
	"rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RLLL{d}":     true,
	// v4: 45 deg layer twist, holders horizontal
	"lr{d}LR{d}lClCRR{d}fRbL{d}": true,
	"fCfCbCbC{d}lr{d}LLRL{d}lC{d}LR{d}lClC{d}LL{d}" +
		"FRBR{d}fLbR{d}FLBL{d}RRLR{d}fCfCbCbC{d}RLLL{d}": true,
	"fr{d}FR{d}fCfCBR{d}rRlL{d}":                    true,
	"rCrClClC{d}fr{d}FLBL{d}fC{d}FR{d}fCfC{d}FL{d}": true,
	"br{d}BR{d}bCbCFR{d}rLlR{d}":                    true,
	"rCrClClC{d}br{d}BLFL{d}bC{d}BR{d}bCbC{d}BL{d}": true,
}

func upgradePlan(p ScanPlan) ScanPlan {
	if p.Version >= 5 {
		return p
	}
	def := map[string]ScanStep{}
	for _, st := range defaultPlan().Steps {
		def[st.ID] = st
	}
	for i, st := range p.Steps {
		d, ok := def[st.ID]
		if ok && oldDefaults[st.Present] && oldDefaults[st.Revert] {
			d.ImageRot = st.ImageRot
			p.Steps[i] = d
		}
	}
	p.Version = 5
	return p
}

func loadPlan(dir string) ScanPlan {
	b, err := os.ReadFile(filepath.Join(dir, "scan_plan.json"))
	if err == nil {
		var p ScanPlan
		if json.Unmarshal(b, &p) == nil && len(p.Steps) > 0 {
			if p.CameraFace == "" {
				p.CameraFace = "U"
			}
			return upgradePlan(p)
		}
	}
	return defaultPlan()
}

func savePlan(dir string, p ScanPlan) error {
	b, _ := json.MarshalIndent(p, "", "  ")
	return os.WriteFile(filepath.Join(dir, "scan_plan.json"), b, 0o644)
}

// fillSeq puts the speed into a plan string: {d} spinner wait, {s} slider wait.
func fillSeq(seq string, d int) string {
	seq = strings.ReplaceAll(seq, "{s}", strconv.Itoa(sliderWait(d)))
	return strings.ReplaceAll(seq, "{d}", strconv.Itoa(d))
}

type WalkStep struct {
	Step  ScanStep
	View  []string
	Cells [9]int
	Color byte
}

func indexStateAfter(rot []string) [54]int {
	var idx [54]int
	for i := range idx {
		idx[i] = i
	}
	for _, mv := range rot {
		p := movePerm[mv]
		var n [54]int
		for i := 0; i < 54; i++ {
			n[i] = idx[p[i]]
		}
		idx = n
	}
	return idx
}

func walkPlan(plan ScanPlan, front, top byte) []WalkStep {
	home := solvedColors(front, top)
	fi := strings.IndexByte(FACES, plan.CameraFace[0])
	if fi < 0 {
		fi = 0
	}
	orient := []string{}
	out := []WalkStep{}
	for _, st := range plan.Steps {
		view := append(append([]string{}, orient...), st.View...)
		perm := indexStateAfter(view)
		var robotFace [9]int
		for k := 0; k < 9; k++ {
			robotFace[k] = perm[9*fi+k]
		}
		img := [9]int{0, 1, 2, 3, 4, 5, 6, 7, 8}
		for t := 0; t < ((st.ImageRot%4)+4)%4; t++ {
			var n [9]int
			for k := 0; k < 9; k++ {
				n[k] = img[qrot[k]]
			}
			img = n
		}
		var cells [9]int
		for k := 0; k < 9; k++ {
			cells[k] = robotFace[img[k]]
		}
		out = append(out, WalkStep{Step: st, View: view, Cells: cells, Color: home[cells[4]]})
		orient = append(orient, st.Keep...)
	}
	return out
}

func validPlan(plan ScanPlan) bool {
	for _, st := range plan.Steps {
		for _, m := range append(append([]string{}, st.View...), st.Keep...) {
			if _, ok := movePerm[m]; !ok {
				return false
			}
		}
	}
	return len(plan.Steps) > 0
}
