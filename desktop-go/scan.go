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
	// every slider that holds the cube is at L (clamped); R = released
	a := func(id string) ScanStep {
		return ScanStep{ID: id, Axis: "front/back",
			Present: "RLLLFRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}",
			Revert:  "fCfCbCbC{d}RLLL{d}",
			View:    []string{"z"}, Keep: []string{"z"}}
	}
	return ScanPlan{CameraFace: "U", Version: 2, Steps: []ScanStep{
		a("A1"), a("A2"), a("A3"), a("A4"),
		{ID: "B1", Axis: "left/right", Present: "RLLLFRBR{d}rrll{d}",
			Revert: "rCrClClC{d}FLBL{d}", View: []string{"x"}, Keep: []string{}},
		{ID: "B2", Axis: "left/right", Present: "FLBLRRLR{d}rRlL{d}RLLL{d}FRBR{d}rllr{d}",
			Revert: "rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RLLL{d}", View: []string{"x'"}, Keep: []string{}},
	}}
}

// v1 defaults left the sliders at C; saved plans are upgraded in place to clamp at L.
var v1Upgrade = map[string]string{
	"fCfCbCbC{d}RCLC{d}FCBC":                      "fCfCbCbC{d}RLLL{d}",
	"rCrClClC{d}FCBC{d}RCLC":                      "rCrClClC{d}FLBL{d}",
	"rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RCLC{d}FCBC": "rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RLLL{d}",
}

func upgradePlan(p ScanPlan) ScanPlan {
	if p.Version >= 2 {
		return p
	}
	for i := range p.Steps {
		if n, ok := v1Upgrade[p.Steps[i].Revert]; ok {
			p.Steps[i].Revert = n
		}
	}
	p.Version = 2
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

func fillSeq(seq string, d int) string { return strings.ReplaceAll(seq, "{d}", strconv.Itoa(d)) }

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
