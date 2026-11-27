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
	// Sliders: L = clamped, R = released. Spinner angles: L -90, l -45, C 0, r +45, R +90.
	// A gripper can only clamp square to a face (C or +-90). While the cube turns about one
	// axis, the released pair's grippers are turned HORIZONTAL (+-90, prongs along the turn
	// axis) so the corners of the turning cube cannot hit them. Before each lift the robot
	// changes axis: the lifting pair clamps, the other pair releases, turns horizontal and
	// clamps again; then the lifting pair re-grips at its start angle, the other pair
	// releases, and the face is lifted 45 deg towards the robot front for the camera.
	a := func(id string) ScanStep {
		return ScanStep{ID: id, Axis: "front/back",
			// right/left grippers horizontal; front/back re-grip at -90; lift 135 deg (L -> r)
			Present: "FLBLRRLR{d}rRlL{d}RLLL{d}FRBR{d}fLbR{d}FLBL{d}RRLR{d}frbl{d}",
			// back 45 deg: cube square again (net 90 deg); right/left clamp (still horizontal)
			Revert: "fCfCbCbC{d}RLLL{d}",
			View:   []string{"z"}, Keep: []string{"z"}}
	}
	return ScanPlan{CameraFace: "U", Version: 3, Steps: []ScanStep{
		a("A1"), a("A2"), a("A3"), a("A4"),
		// front/back grippers horizontal; right/left re-grip at -90; lift 45 deg (L -> l): red up
		{ID: "B1", Axis: "left/right",
			Present: "RLLLFRBR{d}fRbL{d}FLBL{d}RRLR{d}rLlR{d}RLLL{d}FRBR{d}rllr{d}",
			Revert:  "rLlR{d}FLBL{d}", View: []string{"x"}, Keep: []string{}},
		// right/left re-grip at +90; lift 135 deg the other way (R -> l): orange up
		{ID: "B2", Axis: "left/right",
			Present: "RLLLFRBR{d}fRbL{d}FLBL{d}RRLR{d}rRlL{d}RLLL{d}FRBR{d}rllr{d}",
			// back to home, then every gripper back to vertical (C) and clamped for solving
			Revert: "rRlL{d}FLBL{d}RRLR{d}rCrClClC{d}RLLL{d}FRBR{d}fCfCbCbC{d}FLBL{d}",
			View:   []string{"x'"}, Keep: []string{}},
	}}
}

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
}

func upgradePlan(p ScanPlan) ScanPlan {
	if p.Version >= 3 {
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
	p.Version = 3
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
