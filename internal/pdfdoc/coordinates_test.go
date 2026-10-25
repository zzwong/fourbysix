package pdfdoc

import (
	"image"
	"image/color"
	"testing"

	"github.com/klippa-app/go-pdfium/enums"

	"github.com/zzwong/fourbysix/internal/layout"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

// The crop is offset from the media origin, and the graphic and text occupy
// different corners, so a missing translation or rotation cannot look right.
// Expected rectangles are hand-derived in visible-page coordinates, not from
// pdfdoc's conversion or PDFium's coordinate-conversion APIs.
func TestVisiblePageCoordinateBoundary(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rotate        int
		inherit       bool
		width, height int
		graphic       layout.Rect
		textWindow    layout.Rect
	}{
		{"offset crop", 0, false, 400, 500, layout.Rect{X0: 40, Y0: 50, X1: 240, Y1: 200}, layout.Rect{X0: 270, Y0: 420, X1: 350, Y1: 450}},
		{"explicit 90", 90, false, 500, 400, layout.Rect{X0: 300, Y0: 40, X1: 450, Y1: 240}, layout.Rect{X0: 50, Y0: 270, X1: 80, Y1: 350}},
		{"explicit 180", 180, false, 400, 500, layout.Rect{X0: 160, Y0: 300, X1: 360, Y1: 450}, layout.Rect{X0: 50, Y0: 50, X1: 130, Y1: 80}},
		{"explicit 270", 270, false, 500, 400, layout.Rect{X0: 50, Y0: 160, X1: 200, Y1: 360}, layout.Rect{X0: 420, Y0: 50, X1: 450, Y1: 130}},
		{"inherited 90", 90, true, 500, 400, layout.Rect{X0: 300, Y0: 40, X1: 450, Y1: 240}, layout.Rect{X0: 50, Y0: 270, X1: 80, Y1: 350}},
		{"inherited 180", 180, true, 400, 500, layout.Rect{X0: 160, Y0: 300, X1: 360, Y1: 450}, layout.Rect{X0: 50, Y0: 50, X1: 130, Y1: 80}},
		{"inherited 270", 270, true, 500, 400, layout.Rect{X0: 50, Y0: 160, X1: 200, Y1: 360}, layout.Rect{X0: 420, Y0: 50, X1: 450, Y1: 130}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// testpdf uses top-left media coordinates. These become PDF
			// CropBox [100 200 500 700] and rect [140 500 340 650].
			crop := testpdf.Rect(100, 92, 500, 592)
			d := openBytes(t, mustWrite(t, []testpdf.Page{{
				Width: 612, Height: 792,
				CropBox: &crop, Rotate: tc.rotate, InheritRotate: tc.inherit,
				Rects: []testpdf.RectSpec{testpdf.Rect(140, 142, 340, 292)},
				Texts: []testpdf.TextSpec{{X: 380, Y: 532, Size: 12, S: "TOP"}},
			}}))

			w, h, err := d.PageSize(0)
			if err != nil {
				t.Fatal(err)
			}
			if w != float64(tc.width) || h != float64(tc.height) {
				t.Errorf("PageSize = %g x %g, want %d x %d", w, h, tc.width, tc.height)
			}

			// Objects remains raw PDF space, even when Regions is normalized.
			objects, err := d.Objects(0)
			if err != nil {
				t.Fatal(err)
			}
			if len(objects) != 2 {
				t.Fatalf("Objects = %v, want one path and one text object", objects)
			}
			var paths, texts int
			for _, o := range objects {
				switch o.Type {
				case enums.FPDF_PAGEOBJ_PATH:
					paths++
					if o.Left != 140 || o.Bottom != 500 || o.Right != 340 || o.Top != 650 {
						t.Errorf("raw path bounds = %v, want PDF bounds (140,500)-(340,650)", o)
					}
				case enums.FPDF_PAGEOBJ_TEXT:
					texts++
					if o.Left < 370 || o.Bottom < 250 || o.Right > 450 || o.Top > 280 || o.Right <= o.Left || o.Top <= o.Bottom {
						t.Errorf("raw text bounds = %v, want nonempty PDF bounds inside (370,250)-(450,280)", o)
					}
				}
			}
			if paths != 1 || texts != 1 {
				t.Errorf("object kinds: paths=%d texts=%d, want one each", paths, texts)
			}

			graphics, text, err := d.Regions(0)
			if err != nil {
				t.Fatal(err)
			}
			if len(graphics) != 1 || len(text) != 1 {
				t.Fatalf("Regions = graphics %v, text %v; want one each", graphics, text)
			}
			if graphics[0] != tc.graphic {
				t.Errorf("graphic region = %v, want %v", graphics[0], tc.graphic)
			}
			if text[0].Empty() || text[0].Clip(tc.textWindow) != text[0] {
				t.Errorf("text region = %v, want nonempty bounds inside %v", text[0], tc.textWindow)
			}

			full, err := d.Render(0, 72, nil)
			if err != nil {
				t.Fatal(err)
			}
			if want := image.Rect(0, 0, tc.width, tc.height); full.Bounds() != want {
				t.Fatalf("full render bounds = %v, want %v", full.Bounds(), want)
			}
			assertPixel(t, full, 5, 5, color.RGBA{255, 255, 255, 255}, 0)
			assertPixel(t, full, int(tc.graphic.X0)+10, int(tc.graphic.Y0)+10, color.RGBA{0, 0, 0, 255}, 0)
			assertPixel(t, full, int(tc.graphic.X1)-10, int(tc.graphic.Y1)-10, color.RGBA{0, 0, 0, 255}, 0)
			if !coordinateWindowHasInk(full, tc.textWindow) {
				t.Errorf("full render has no text ink in %v", tc.textWindow)
			}

			// Rendering the returned region must select the same graphic.
			cropped, err := d.Render(0, 72, &graphics[0])
			if err != nil {
				t.Fatal(err)
			}
			if want := image.Rect(0, 0, int(tc.graphic.Width()), int(tc.graphic.Height())); cropped.Bounds() != want {
				t.Fatalf("region render bounds = %v, want %v", cropped.Bounds(), want)
			}
			assertPixel(t, cropped, 10, 10, color.RGBA{0, 0, 0, 255}, 0)
			assertPixel(t, cropped, cropped.Bounds().Dx()-10, cropped.Bounds().Dy()-10, color.RGBA{0, 0, 0, 255}, 0)
		})
	}
}

func coordinateWindowHasInk(img image.Image, window layout.Rect) bool {
	for y := int(window.Y0); y < int(window.Y1); y++ {
		for x := int(window.X0); x < int(window.X1); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r < 250*257 || g < 250*257 || b < 250*257 {
				return true
			}
		}
	}
	return false
}
