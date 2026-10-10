package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/zzwong/fourbysix/internal/pdfdoc"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

func TestRunTransparentLabels(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, threshold := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("split=%t/threshold=%t/reverse=%t", split, threshold, reverse), func(t *testing.T) {
					pages := []testpdf.Page{
						{
							Width: 240, Height: 320,
							Rects:      append(box(80, 100, 160, 220, 4), testpdf.Rect(92, 140, 108, 180)),
							AlphaRects: []testpdf.AlphaRectSpec{{RectSpec: testpdf.RectColor(125, 145, 145, 165, 0.25, 0.5, 0.75), Alpha: 0.5, BlendMode: "Darken"}},
						},
						{
							Width: 360, Height: 280,
							Rects: append(box(180, 60, 260, 180, 4), testpdf.Rect(228, 100, 244, 140)),
						},
					}
					args := []string{"--label", "2x3", "--dpi", "72", "--rotation", "none", "--margin", "8", "-q"}
					if split {
						args = append(args, "--split")
					}
					if threshold {
						args = append(args, "--threshold")
					}
					order := []int{0, 1}
					if reverse {
						args = append(args, "--pages", "2,1")
						order = []int{1, 0}
					}
					got := renderLabelOutput(t, pages, args)
					// Painting an explicit paper background is an independent
					// PDFium reference for the page with unpainted margins.
					pages[0].Rects = append([]testpdf.RectSpec{testpdf.RectColor(0, 0, 240, 320, 1, 1, 1)}, pages[0].Rects...)
					want := renderLabelOutput(t, pages, args)
					if len(got) != 2 || len(want) != 2 {
						t.Fatalf("output page counts = %d, %d; want 2 each", len(got), len(want))
					}
					for page, img := range got {
						for _, point := range []image.Point{{2, 108}, {141, 108}, {72, 108}, {72, 2}, {72, 213}} {
							assertLabelShade(t, img, point.X, point.Y, 255)
						}
						markerX := 40
						if order[page] == 1 {
							markerX = 104
						}
						assertLabelShade(t, img, markerX, 108, 0)
						for y := range img.Bounds().Dy() {
							for x := range img.Bounds().Dx() {
								r, g, b, a := img.At(x, y).RGBA()
								wr, wg, wb, wa := want[page].At(x, y).RGBA()
								for channel, pair := range [][2]uint32{{r, wr}, {g, wg}, {b, wb}, {a, wa}} {
									if delta := int(pair[0]>>8) - int(pair[1]>>8); delta < -2 || delta > 2 {
										t.Fatalf("page %d pixel (%d,%d) channel %d = %d, paper reference %d", page, x, y, channel, pair[0]>>8, pair[1]>>8)
									}
								}
							}
						}
					}
				})
			}
		}
	}
}

func renderLabelOutput(t *testing.T, pages []testpdf.Page, options []string) []image.Image {
	t.Helper()
	dir := t.TempDir()
	src := writeFixture(t, dir, pages)
	out := filepath.Join(dir, "out.pdf")
	args := append([]string{src, "-o", out}, options...)
	if code, stdout, stderr := runCLI(t, args...); code != 0 || stdout != "" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	d, err := pdfdoc.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	count, err := d.PageCount()
	if err != nil {
		t.Fatal(err)
	}
	var images []image.Image
	for page := range count {
		w, h, err := d.PageSize(page)
		if err != nil || w != 144 || h != 216 {
			t.Fatalf("page %d size = %gx%g, error %v; want 144x216", page, w, h, err)
		}
		img, err := d.Render(page, 72, nil)
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != image.Rect(0, 0, 144, 216) {
			t.Fatalf("page %d render bounds = %v", page, img.Bounds())
		}
		images = append(images, img)
	}
	return images
}

func assertLabelShade(t *testing.T, img image.Image, x, y int, want uint32) {
	t.Helper()
	r, g, b, a := img.At(x, y).RGBA()
	if r>>8 != want || g>>8 != want || b>>8 != want || a != 65535 {
		t.Fatalf("pixel (%d,%d) = (%d,%d,%d,%d), want opaque shade %d", x, y, r>>8, g>>8, b>>8, a>>8, want)
	}
}
