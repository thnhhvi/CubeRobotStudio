package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

// thumb draws a 96x96 face picture: 9 stickers, the centre one with an optional
// "GAN"-like blue mark (three bars) rotated by angle degrees.
func thumb(center color.RGBA, logo bool, angle float64) string {
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	others := []color.RGBA{{200, 20, 30, 255}, {20, 170, 70, 255}, {240, 220, 40, 255}, {250, 120, 20, 255}}
	a := angle * math.Pi / 180
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			cx, cy := x/32, y/32
			c := others[(cx+cy)%4]
			if cx == 1 && cy == 1 {
				c = center
				if logo {
					// coordinates relative to the centre, rotated back by the logo angle
					dx, dy := float64(x)-47.5, float64(y)-47.5
					u := dx*math.Cos(a) + dy*math.Sin(a)
					v := -dx*math.Sin(a) + dy*math.Cos(a)
					if math.Abs(v) < 4 && (math.Abs(u+8) < 3 || math.Abs(u) < 3 || math.Abs(u-8) < 3) {
						c = color.RGBA{30, 80, 210, 255}
					}
				}
			}
			if x%32 < 2 || y%32 < 2 { // black gaps between stickers
				c = color.RGBA{15, 15, 15, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70})
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestLogoAnyDirection(t *testing.T) {
	white := color.RGBA{235, 235, 230, 255}
	for _, ang := range []float64{0, 90, 180, 270, 45, 135, 30} {
		if got, ok := logoInThumb(thumb(white, true, ang)); !ok || !got {
			t.Errorf("logo at %v deg not detected", ang)
		}
	}
	cases := map[string]color.RGBA{"plain white": white, "blue": {20, 70, 200, 255},
		"red": {200, 20, 30, 255}, "yellow": {240, 220, 40, 255}, "green": {20, 170, 70, 255}}
	for name, c := range cases {
		if got, _ := logoInThumb(thumb(c, false, 0)); got {
			t.Errorf("%s centre wrongly detected as logo", name)
		}
	}
	// blue centre with a white mark must not count either
	if got, _ := logoInThumb(thumb(color.RGBA{20, 70, 200, 255}, true, 0)); got {
		t.Errorf("blue centre with a mark detected as logo")
	}
}

func TestOrientationFromTwoCentres(t *testing.T) {
	// every orientation must be recovered from the first two scanned centres
	plan := defaultPlan()
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			f, tp := COLORS[i], COLORS[j]
			cc, err := centerColors(f, tp)
			if err != nil {
				continue
			}
			steps := walkPlan(plan, f, tp)
			obs := []centerObs{}
			for k := 0; k < 2; k++ {
				face := FACES[steps[k].Cells[4]/9]
				col := cc[face]
				o := centerObs{Face: face, RGB: defaultRGB[col]}
				if col == 'W' {
					o.RGB, o.Logo = [3]int{70, 90, 190}, true // logo makes it read blue-ish
				}
				obs = append(obs, o)
			}
			best := rankOrientations(obs)[0]
			if best.Front != f || best.Top != tp {
				t.Errorf("%c/%c detected as %c/%c", f, tp, best.Front, best.Top)
			}
		}
	}
}
