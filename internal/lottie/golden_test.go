package lottie

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestGolden compares frames with rlottie's or lottie-web's. LOTTIE_GOLDEN
// names a directory holding <name>.json, frames.txt ("<name> <frames>"
// lines) and, for frames 0, n/4, n/2 and 3n/4, 256x256 pictures:
// <name>.<frame>.rgba (premultiplied, from rlottie) or, with
// LOTTIE_REF=web, <name>.<frame>.web (straight alpha, from lottie-web's SVG
// renderer in a browser). Side-by-side pictures of the worst go to
// LOTTIE_GOLDEN_OUT.
func TestGolden(t *testing.T) {
	dir := os.Getenv("LOTTIE_GOLDEN")
	if dir == "" {
		t.Skip("LOTTIE_GOLDEN not set")
	}
	out := os.Getenv("LOTTIE_GOLDEN_OUT")
	web := os.Getenv("LOTTIE_REF") == "web"
	f, err := os.Open(filepath.Join(dir, "frames.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type result struct {
		name  string
		frame int
		diff  float64 // share of pixels off by more than 32
	}
	var results []result
	var total time.Duration
	frames := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		name := fields[0]
		n, _ := strconv.Atoi(fields[1])
		data, err := os.ReadFile(filepath.Join(dir, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		a, err := Load(data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for q := 0; q < 4; q++ {
			fr := n * q / 4
			ext := "rgba"
			if web {
				ext = "web"
			}
			want, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%s.%d.%s", name, fr, ext)))
			if err != nil || len(want) != 256*256*4 {
				continue
			}
			if web {
				for i := 0; i < len(want); i += 4 {
					a := int(want[i+3])
					for c := 0; c < 3; c++ {
						want[i+c] = uint8((int(want[i+c])*a + 127) / 255)
					}
				}
			}
			got := image.NewRGBA(image.Rect(0, 0, 256, 256))
			start := time.Now()
			a.Render(got, float64(fr))
			total += time.Since(start)
			frames++
			bad := 0
			for i := 0; i < len(want); i += 4 {
				for c := 0; c < 4; c++ {
					d := int(want[i+c]) - int(got.Pix[i+c])
					if d > 32 || d < -32 {
						bad++
						break
					}
				}
			}
			r := result{name, fr, float64(bad) / (256 * 256)}
			results = append(results, r)
			if out != "" && r.diff > 0.01 {
				writeSide(t, filepath.Join(out, fmt.Sprintf("%s.%d.png", name, fr)), want, got)
			}
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].diff > results[j].diff })
	ok := 0
	for _, r := range results {
		if r.diff <= 0.01 {
			ok++
		}
	}
	t.Logf("%d of %d frames within 1%% of the reference; %.2f ms a frame", ok, len(results), float64(total.Microseconds())/1000/float64(max(frames, 1)))
	for _, r := range results[:min(len(results), 40)] {
		t.Logf("%6.2f%%  %s frame %d", r.diff*100, r.name, r.frame)
	}
}

func writeSide(t *testing.T, path string, want []byte, got *image.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, 256*3, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			i := (y*256 + x) * 4
			w := over(want[i : i+4])
			g := over(got.Pix[i : i+4])
			img.Set(x, y, w)
			img.Set(256+x, y, g)
			d := absd(w.R, g.R) + absd(w.G, g.G) + absd(w.B, g.B)
			img.Set(512+x, y, color.RGBA{uint8(min(255, d)), 0, 0, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}

// over puts a premultiplied pixel on a light gray checker-free background.
func over(p []byte) color.RGBA {
	inv := 255 - int(p[3])
	return color.RGBA{uint8(int(p[0]) + inv*230/255), uint8(int(p[1]) + inv*230/255), uint8(int(p[2]) + inv*230/255), 255}
}

func absd(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
