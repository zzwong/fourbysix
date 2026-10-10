package pdfdoc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/klippa-app/go-pdfium/requests"

	"github.com/zzwong/fourbysix/internal/layout"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

func TestCopyImagePDFiumStraightAlpha(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  *image.RGBA
	}{
		{"tight stride", image.NewRGBA(image.Rect(0, 0, 256, 256))},
		{"offset origin", image.NewRGBA(image.Rect(11, 23, 267, 279))},
		{"subimage stride", image.NewRGBA(image.Rect(0, 0, 280, 280)).SubImage(image.Rect(11, 23, 267, 279)).(*image.RGBA)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					i := src.PixOffset(src.Rect.Min.X+x, src.Rect.Min.Y+y)
					copy(src.Pix[i:i+4], []byte{byte(x), byte(255 - x), byte(x * 7), byte(y)})
				}
			}
			before := append([]byte(nil), src.Pix...)
			got := copyImage(src).(*image.RGBA)
			if got.Bounds() != src.Bounds() {
				t.Fatalf("bounds = %v, want %v", got.Bounds(), src.Bounds())
			}
			// Independent straight-alpha source-over-white reference, using
			// Go's 16-bit color precision before converting back to 8 bits.
			blend := func(c, a uint32) uint8 {
				return uint8((c*0x101*a/255 + (255-a)*0x101) >> 8)
			}
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					want := color.RGBA{
						R: blend(uint32(x), uint32(y)), G: blend(uint32(255-x), uint32(y)),
						B: blend(uint32(byte(x*7)), uint32(y)), A: 255,
					}
					if c := got.RGBAAt(src.Rect.Min.X+x, src.Rect.Min.Y+y); c != want {
						t.Fatalf("color %d alpha %d = %v, want %v", x, y, c, want)
					}
				}
			}
			if !bytes.Equal(src.Pix, before) {
				t.Fatal("copy modified the source render buffer")
			}
			saved := append([]byte(nil), got.Pix...)
			clear(src.Pix)
			if !bytes.Equal(got.Pix, saved) {
				t.Fatal("copy aliases the source render buffer")
			}
		})
	}
}

func TestCopyImageNilAndGray(t *testing.T) {
	if got := copyImage(nil); got != nil {
		t.Fatalf("copyImage(nil) = %v", got)
	}
	src := image.NewGray(image.Rect(7, 9, 9, 10))
	src.Pix[0], src.Pix[1] = 42, 255
	got := copyImage(src).(*image.RGBA)
	if got.Bounds() != src.Bounds() || got.RGBAAt(7, 9) != (color.RGBA{42, 42, 42, 255}) || got.RGBAAt(8, 9) != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("gray copy = %v, bounds %v", got.Pix, got.Bounds())
	}
}

func TestRenderTransparentPages(t *testing.T) {
	page := testpdf.Page{
		Width: 240, Height: 320,
		Rects: []testpdf.RectSpec{
			testpdf.Rect(60, 80, 180, 84), testpdf.Rect(60, 236, 180, 240),
			testpdf.Rect(60, 80, 64, 240), testpdf.Rect(176, 80, 180, 240),
		},
		AlphaRects: []testpdf.AlphaRectSpec{{RectSpec: testpdf.RectColor(90, 110, 150, 150, 0.25, 0.5, 0.75), Alpha: 0.5, BlendMode: "Darken"}},
	}
	d := openBytes(t, mustWrite(t, []testpdf.Page{page}))
	assertPageTransparency(t, d, 0, true)
	for _, dpi := range []int{72, 144} {
		for _, crop := range []bool{false, true} {
			t.Run(fmt.Sprintf("dpi=%d/crop=%t", dpi, crop), func(t *testing.T) {
				var rect *layout.Rect
				width, height, offsetX, offsetY := 240, 320, 0, 0
				if crop {
					rect = &layout.Rect{X0: 50, Y0: 70, X1: 190, Y1: 250}
					width, height, offsetX, offsetY = 140, 180, 50, 70
				}
				img, err := d.Render(0, dpi, rect)
				if err != nil {
					t.Fatal(err)
				}
				scale := dpi / 72
				if want := image.Rect(0, 0, width*scale, height*scale); img.Bounds() != want {
					t.Fatalf("bounds = %v, want %v", img.Bounds(), want)
				}
				assertPixel(t, img, 2, 2, color.RGBA{255, 255, 255, 255}, 0)
				assertPixel(t, img, (70-offsetX)*scale, (90-offsetY)*scale, color.RGBA{255, 255, 255, 255}, 0)
				assertPixel(t, img, (62-offsetX)*scale, (100-offsetY)*scale, color.RGBA{0, 0, 0, 255}, 0)
				assertPixel(t, img, (100-offsetX)*scale, (120-offsetY)*scale, color.RGBA{159, 191, 223, 255}, 2)
				for y := range img.Bounds().Dy() {
					for x := range img.Bounds().Dx() {
						if _, _, _, a := img.At(x, y).RGBA(); a != 65535 {
							t.Fatalf("pixel (%d,%d) retains alpha %d", x, y, a)
						}
					}
				}
				if dpi == 72 {
					got, ok := layout.ContentBBox(img, 250)
					want := layout.Rect{X0: float64(60 - offsetX), Y0: float64(80 - offsetY), X1: float64(179 - offsetX), Y1: float64(239 - offsetY)}
					if !ok || got != want {
						t.Fatalf("ContentBBox = %v, %t, want %v", got, ok, want)
					}
				}
			})
		}
	}
}

func TestRenderMixedPagesDetached(t *testing.T) {
	pages := []testpdf.Page{
		{Width: 120, Height: 160, AlphaRects: []testpdf.AlphaRectSpec{{RectSpec: testpdf.RectColor(20, 30, 80, 110, 1, 0, 0), Alpha: 0.5, BlendMode: "Darken"}}},
		{Width: 180, Height: 100, Rects: []testpdf.RectSpec{testpdf.RectColor(20, 30, 80, 90, 0, 0, 1)}},
		{Width: 100, Height: 140, AlphaRects: []testpdf.AlphaRectSpec{{RectSpec: testpdf.Rect(20, 30, 80, 110), Alpha: 0, BlendMode: "Darken"}}},
	}
	d, err := Open(mustWrite(t, pages))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if d != nil {
			d.Close()
		}
	}()
	var images []*image.RGBA
	var snapshots [][]byte
	for _, page := range []int{0, 1, 2, 2, 1, 0} {
		assertPageTransparency(t, d, page, page != 1)
		img, err := d.Render(page, 72, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := image.Rect(0, 0, int(pages[page].Width), int(pages[page].Height)); img.Bounds() != want {
			t.Fatalf("page %d bounds = %v, want %v", page, img.Bounds(), want)
		}
		assertPixel(t, img, 5, 5, color.RGBA{255, 255, 255, 255}, 0)
		want := []color.RGBA{{255, 128, 128, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}[page]
		assertPixel(t, img, 40, 50, want, 1)
		if _, ok := layout.ContentBBox(img, 250); ok != (page != 2) {
			t.Fatalf("page %d content present = %t", page, ok)
		}
		rgba := img.(*image.RGBA)
		images = append(images, rgba)
		snapshots = append(snapshots, append([]byte(nil), rgba.Pix...))
	}
	d.Close()
	d = nil
	for i, img := range images {
		if !bytes.Equal(img.Pix, snapshots[i]) {
			t.Fatalf("render %d changed after later renders or document close", i)
		}
	}
}

func assertPageTransparency(t *testing.T, d *Doc, page int, want bool) {
	t.Helper()
	// Fill alpha alone does not make PDFium request a transparent bitmap.
	// Darken triggers that path and is identical to normal blending on white.
	res, err := d.inst.FPDFPage_HasTransparency(&requests.FPDFPage_HasTransparency{
		Page: requests.Page{ByIndex: &requests.PageByIndex{Document: d.doc, Index: page}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.HasTransparency != want {
		t.Fatalf("page %d HasTransparency = %t, want %t", page, res.HasTransparency, want)
	}
}

func assertPixel(t *testing.T, img image.Image, x, y int, want color.RGBA, tolerance int) {
	t.Helper()
	r, g, b, a := img.At(x, y).RGBA()
	got := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	if abs(float64(got.R)-float64(want.R)) > float64(tolerance) ||
		abs(float64(got.G)-float64(want.G)) > float64(tolerance) ||
		abs(float64(got.B)-float64(want.B)) > float64(tolerance) || got.A != want.A {
		t.Fatalf("pixel (%d,%d) = %v, want %v (+/-%d RGB)", x, y, got, want, tolerance)
	}
}
