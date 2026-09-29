package place

import (
	"image"
	"image/color"
	"testing"
)

func cfg4x6(rotation string) Config {
	return Config{LabelW: 4, LabelH: 6, DPI: 300, Margin: 8, Rotation: rotation}
}

// cornerImage builds a w x h white image with the four corners painted
// distinct colors so rotation direction is observable.
func cornerImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})   // top-left: red
	img.SetRGBA(w-1, 0, color.RGBA{0, 255, 0, 255}) // top-right: green
	img.SetRGBA(0, h-1, color.RGBA{0, 0, 255, 255}) // bottom-left: blue
	img.SetRGBA(w-1, h-1, color.RGBA{0, 0, 0, 255}) // bottom-right: black
	return img
}

func pixelAt(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func assertRGB(t *testing.T, c color.RGBA, want color.RGBA, what string) {
	t.Helper()
	if c.R != want.R || c.G != want.G || c.B != want.B {
		t.Errorf("%s = %v, want %v", what, c, want)
	}
}

func TestRotateDirection(t *testing.T) {
	img := cornerImage(4, 3)

	t.Run("clockwise maps top-left to top-right", func(t *testing.T) {
		rot := rotate90(img, true)
		b := rot.Bounds()
		if b.Dx() != 3 || b.Dy() != 4 {
			t.Fatalf("rotated size = %dx%d, want 3x4", b.Dx(), b.Dy())
		}
		assertRGB(t, pixelAt(rot, 2, 0), color.RGBA{255, 0, 0, 255}, "top-right")
		assertRGB(t, pixelAt(rot, 2, 3), color.RGBA{0, 255, 0, 255}, "bottom-right")
		assertRGB(t, pixelAt(rot, 0, 3), color.RGBA{0, 0, 0, 255}, "bottom-left")
		assertRGB(t, pixelAt(rot, 0, 0), color.RGBA{0, 0, 255, 255}, "top-left")
	})

	t.Run("counter-clockwise maps top-left to bottom-left", func(t *testing.T) {
		rot := rotate90(img, false)
		b := rot.Bounds()
		if b.Dx() != 3 || b.Dy() != 4 {
			t.Fatalf("rotated size = %dx%d, want 3x4", b.Dx(), b.Dy())
		}
		assertRGB(t, pixelAt(rot, 0, 3), color.RGBA{255, 0, 0, 255}, "bottom-left")
		assertRGB(t, pixelAt(rot, 0, 0), color.RGBA{0, 255, 0, 255}, "top-left")
		assertRGB(t, pixelAt(rot, 2, 0), color.RGBA{0, 0, 0, 255}, "top-right")
		assertRGB(t, pixelAt(rot, 2, 3), color.RGBA{0, 0, 255, 255}, "bottom-right")
	})
}

func TestProcessImageRotationDecision(t *testing.T) {
	img := cornerImage(4, 3)

	t.Run("cw90 always rotates", func(t *testing.T) {
		if res := ProcessImage(img, 100, 200, cfg4x6("cw90")); !res.Rotated {
			t.Error("cw90 must rotate")
		}
	})
	t.Run("ccw90 always rotates", func(t *testing.T) {
		if res := ProcessImage(img, 100, 200, cfg4x6("ccw90")); !res.Rotated {
			t.Error("ccw90 must rotate")
		}
	})
	t.Run("none never rotates", func(t *testing.T) {
		if res := ProcessImage(img, 500, 100, cfg4x6("none")); res.Rotated {
			t.Error("none must not rotate")
		}
	})
	t.Run("auto keeps landscape upright", func(t *testing.T) {
		if res := ProcessImage(img, 500, 100, cfg4x6("auto")); res.Rotated {
			t.Error("auto must not rotate landscape content")
		}
	})
	t.Run("best-fit rotates landscape on portrait label", func(t *testing.T) {
		if res := ProcessImage(img, 500, 100, cfg4x6("best-fit")); !res.Rotated {
			t.Error("best-fit should rotate 500x100 on a 4x6 label")
		}
	})
	t.Run("best-fit skips marginal improvement", func(t *testing.T) {
		if res := ProcessImage(img, 330, 310, cfg4x6("best-fit")); res.Rotated {
			t.Error("best-fit must not rotate for a 6% improvement")
		}
	})
}

func TestProcessImageScaleAndCenter(t *testing.T) {
	// Content 288x432 pt at 300 dpi fills the 4x6 label exactly: scale 1,
	// result 69.12 x 103.68 pt, centered at (109.44, 164.16).
	img := solid(288, 432, color.RGBA{10, 20, 30, 255})
	res := ProcessImage(img, 288, 432, cfg4x6("none"))
	if res.Scale != 1 {
		t.Errorf("scale = %g, want 1", res.Scale)
	}
	if res.W != 69.12 || res.H != 103.68 {
		t.Errorf("result size = %g x %g, want 69.12 x 103.68", res.W, res.H)
	}
	if res.X != (288-69.12)/2 || res.Y != (432-103.68)/2 {
		t.Errorf("offset = (%g,%g), want centered", res.X, res.Y)
	}
	if res.Content != "4.0x6.0" || res.Result != "1.0x1.4" {
		t.Errorf("labels = %q / %q", res.Content, res.Result)
	}
}

func TestProcessImageThreshold(t *testing.T) {
	// A 288x432 image (scale 1, no resampling) with large solid areas: the
	// four quadrants sit at luma 0, 127, 128 and 255.
	img := image.NewRGBA(image.Rect(0, 0, 288, 432))
	values := []uint8{0, 127, 128, 255}
	for q, v := range values {
		qx, qy := q%2, q/2
		for y := qy * 216; y < (qy+1)*216; y++ {
			for x := qx * 144; x < (qx+1)*144; x++ {
				img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
			}
		}
	}
	cfg := cfg4x6("none")
	cfg.Threshold = true
	res := ProcessImage(img, 288, 432, cfg)
	g, ok := res.Image.(*image.Gray)
	if !ok {
		t.Fatalf("thresholded image is %T, want *image.Gray", res.Image)
	}
	want := []byte{0, 0, 255, 255} // luma < 128 -> 0, >= 128 -> 255
	for q, w := range want {
		qx, qy := q%2, q/2
		got := g.GrayAt(qx*144+72, qy*216+108).Y
		if got != w {
			t.Errorf("quadrant %d = %d, want %d", q, got, w)
		}
	}
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
