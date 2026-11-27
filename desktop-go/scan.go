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
	ID        string   `json:"id"`
	Axis      string   `json:"axis"`
	Present   string   `json:"present"`
	Revert    string   `json:"revert"`
	View      []string `json:"view"`
	Keep      []string `json:"keep"`
	Look2     string   `json:"look2,omitempty"`
	Look2Back string   `json:"look2_back,omitempty"`
	ImageRot  int      `json:"image_rot"`
}

type ScanPlan struct {
	Version    int        `json:"version"`
	CameraFace string     `json:"camera_face"`
	Steps      []ScanStep `json:"steps"`
	Finish     string     `json:"finish,omitempty"` // run after the last face (back home)
}

func defaultPlan() ScanPlan {
	// Sliders: H = clamped a little harder (carrying the cube in a roll), L = clamped,
	// C = touching, T = following the rolling cube (touching at 0/90 deg, backing off for
	// the corners, grippers vertical: a base so the cube cannot drop), R = released.
	// Spinners: L -90, C 0 (vertical), R +90.
	// {s} slider step, {p} paired step (servos glide together), {d} one spinner.
	// The face on top is scanned first without moving. Each further face is rolled up by
	// one pair: it pre-turns its grippers while released, clamps hard, the other pair eases
	// to C and follows the cube while the grippers turn back (the roll). Scan pose: the
	// rolling pair clamped at L with its grippers vertical, the other pair released at R.
	roll := func(id, view string) ScanStep {
		return ScanStep{ID: id, Axis: "front/back",
			Present: "FRBR{s}fLbR{p}FHBH{s}RCLC{s}fCfCbCbCRTLT{p}FLBLRRLR{s}",
			Revert:  "RLLL{s}",
			Look2:   "RLLL{s}FRBR{s}", Look2Back: "FLBL{s}RRLR{s}",
			View: []string{view}, Keep: []string{view}}
	}
	// left/right roll red (front) or orange (back) up, and roll it back afterwards
	b := func(id, pre, back, view string) ScanStep {
		return ScanStep{ID: id, Axis: "left/right",
			Present: "RRLR{s}" + pre + "{p}RHLH{s}FCBC{s}rCrClClCFTBT{p}RLLLFRBR{s}",
			Revert:  "FLBL{s}RRLR{s}" + back + "{p}RHLH{s}FCBC{s}rCrClClCFTBT{p}FLBL{s}RLLL{s}",
			Look2:   "FLBL{s}RRLR{s}", Look2Back: "RLLL{s}FRBR{s}",
			View: []string{view}, Keep: []string{}}
	}
	top := ScanStep{ID: "A0", Axis: "front/back", // the face already on top: no roll
		Present: "RRLR{s}", Revert: "RLLL{s}",
		Look2: "RLLL{s}FRBR{s}", Look2Back: "FLBL{s}RRLR{s}",
		View: []string{}, Keep: []string{}}
	return ScanPlan{CameraFace: "U", Version: 9, Steps: []ScanStep{
		top, roll("A1", "z"), roll("A2", "z"), roll("A3", "z"),
		b("B1", "rLlR", "rRlL", "x"),  // front (red) -> top
		b("B2", "rRlL", "rLlR", "x'"), // back (orange) -> top
	},
		// after the last face: one more roll brings the cube back to where it started
		Finish: "FRBR{s}fLbR{p}FHBH{s}RCLC{s}fCfCbCbCRTLT{p}RLLL{s}FLBL{s}"}
}

// look2 returns the hand-off for a second look at the face (the other pair takes the
// cube, the rolling pair lets go) and the hand-back; steps saved without them get the
// default for their axis.
func (st ScanStep) look2() (string, string) {
	if st.Look2 != "" && st.Look2Back != "" {
		return st.Look2, st.Look2Back
	}
	if st.Axis == "left/right" {
		return "FLBL{s}RRLR{s}", "RLLL{s}FRBR{s}"
	}
	return "RLLL{s}FRBR{s}", "FLBL{s}RRLR{s}"
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
	// v8 (2.1-2.3): first face rolled up before scanning, other pair released while rolling
	"FRBR{s}fLbR{p}FHBH{s}RRLR{s}fCfCbCbC{p}FLBL{s}":               true,
	"RRLR{s}rLlR{p}RHLH{s}FRBR{s}rCrClClC{p}RLLL{s}":               true,
	"FLBL{s}RRLR{s}rRlL{p}RHLH{s}FRBR{s}rCrClClC{p}FLBL{s}RLLL{s}": true,
	"RRLR{s}rRlL{p}RHLH{s}FRBR{s}rCrClClC{p}RLLL{s}":               true,
	"FLBL{s}RRLR{s}rLlR{p}RHLH{s}FRBR{s}rCrClClC{p}FLBL{s}RLLL{s}": true,
	// v7 (1.9/2.0): 45 deg waypoints, holders at L
	"FRBR{s}flbr{h}fLbR{h}FLBL{s}RRLR{s}flbr{h}fCfCbCbC{h}": true,
	"RLLL{s}": true,
	"RRLR{s}rllr{h}rLlR{h}RLLL{s}FRBR{s}rllr{h}rCrClClC{h}":               true,
	"FLBL{s}RRLR{s}rrll{h}rRlL{h}RLLL{s}FRBR{s}rrll{h}rCrClClC{h}FLBL{s}": true,
	"RRLR{s}rrll{h}rRlL{h}RLLL{s}FRBR{s}rrll{h}rCrClClC{h}":               true,
	"FLBL{s}RRLR{s}rllr{h}rLlR{h}RLLL{s}FRBR{s}rllr{h}rCrClClC{h}FLBL{s}": true,
	// v6 (1.8): direct roll, holders horizontal
	"RRLR{s}frbl{h}fRbL{h}":                   true,
	"RLLL{s}FRBR{s}frbl{h}fCfCbCbC{h}FLBL{s}": true,
	"FRBR{s}rrll{h}rRlL{h}":                   true,
	"rrll{h}rCrClClC{h}FLBL{s}":               true,
	"FRBR{s}rllr{h}rLlR{h}":                   true,
	"rllr{h}rCrClClC{h}FLBL{s}":               true,
	// v5 (1.7): pre-turned grippers, holders at C
	"FRBR{s}fLbR{d}FLBL{s}RRLR{s}fCfCbCbC{d}FCBC{s}": true,
	"FLBL{s}RLLL{s}": true,
	"RRLR{s}rLlR{d}RLLL{s}FRBR{s}rCrClClC{d}FCBC{s}RRLR{s}": true,
	"RRLR{s}rRlL{d}RLLL{s}FRBR{s}rCrClClC{d}FCBC{s}RRLR{s}": true,
	"FLBL{s}rRlL{d}RLLL{s}FRBR{s}rCrClClC{d}FLBL{s}":        true,
	"FLBL{s}rLlR{d}RLLL{s}FRBR{s}rCrClClC{d}FLBL{s}":        true,
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
	if p.Version >= 9 {
		return p
	}
	// a plan made only of old default steps becomes the current default plan (keeping
	// each face's picture rotation); a plan with edited steps is kept as it is
	for _, st := range p.Steps {
		if !oldDefaults[st.Present] || !oldDefaults[st.Revert] {
			p.Version = 9
			return p
		}
	}
	rot := map[string]int{}
	for _, st := range p.Steps {
		id := st.ID
		if id == "A4" { // the old last front/back face is the face on top at the start
			id = "A0"
		}
		rot[id] = st.ImageRot
	}
	np := defaultPlan()
	np.CameraFace = p.CameraFace
	for i := range np.Steps {
		np.Steps[i].ImageRot = rot[np.Steps[i].ID]
	}
	return np
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

// fillSeq puts the speed into a plan string: {d} one spinner, {s} sliders, {p} two
// spinners together. ({h} from older saved plans counts as half a paired move.)
func fillSeq(seq string, d int) string {
	seq = strings.ReplaceAll(seq, "{s}", strconv.Itoa(sliderWait(d)))
	seq = strings.ReplaceAll(seq, "{p}", strconv.Itoa(pairWait(d)))
	seq = strings.ReplaceAll(seq, "{h}", strconv.Itoa(pairWait(d)/2))
	return strings.ReplaceAll(seq, "{d}", strconv.Itoa(spinWait(d)))
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
