// Package place turns one page region into a positioned label image:
// crop (with margin) -> rotate -> scale -> optional threshold -> centered on
// the label page.
package place

import (
	"fmt"
	"image"
	"image/color"

	"golang.org/x/image/draw"

	"github.com/zzwong/fourbysix/internal/layout"
	"github.com/zzwong/fourbysix/internal/pdfdoc"
)

// Config is the subset of the tool config that placement needs.
type Config struct {
	LabelW, LabelH float64 // inches
	DPI            int
	Margin         float64 // points
	Rotation       string
	Threshold      bool
}

// Result describes one placed label. Image is nil for a blank label page.
type Result struct {
	Image   image.Image // RGB(A) or *image.Gray when thresholded
	X, Y    float64     // target top-left on the label page, points
	W, H    float64     // target size, points
	Rotated bool
	Scale   float64
	Content string // "7.9x5.5" — clip size in inches, 1 decimal
	Result  string // "4.0x5.8" — final size in inches, 1 decimal
}

// Region processes one region of a page: expands it by the margin, clips to
// the page, renders the crop at the configured DPI, then rotates, scales and
// centers it onto a label-sized page.
func Region(d *pdfdoc.Doc, page int, region layout.Rect, cfg Config) (*Result, error) {
	pageW, pageH, err := d.PageSize(page)
	if err != nil {
		return nil, err
	}
	pageRect := layout.Rect{X0: 0, Y0: 0, X1: pageW, Y1: pageH}
	clip := layout.Rect{
		X0: region.X0 - cfg.Margin,
		Y0: region.Y0 - cfg.Margin,
		X1: region.X1 + cfg.Margin,
		Y1: region.Y1 + cfg.Margin,
	}.Clip(pageRect)
	contentW, contentH := clip.Width(), clip.Height()

	img, err := d.Render(page, cfg.DPI, &clip)
	if err != nil {
		return nil, err
	}

	res := ProcessImage(img, contentW, contentH, cfg)
	return res, nil
}

// ProcessImage applies rotation, scaling and thresholding to an already
// cropped render whose content spans contentW x contentH points, and returns
// the centered placement. Extracted so the math is testable without a PDF.
func ProcessImage(img image.Image, contentW, contentH float64, cfg Config) *Result {
	labelW := cfg.LabelW * 72
	labelH := cfg.LabelH * 72

	doRotate := layout.ShouldRotate(contentW, contentH, labelW, labelH, cfg.Rotation)

	rotW, rotH := contentW, contentH
	if doRotate {
		img = rotate90(img, cfg.Rotation != "ccw90")
		rotW, rotH = contentH, contentW
	}

	scale := 1.0
	if rotW > 0 && rotH > 0 {
		scale = min(labelW/rotW, labelH/rotH)
	}
	newW := int(float64(img.Bounds().Dx()) * scale)
	newH := int(float64(img.Bounds().Dy()) * scale)
	if newW <= 0 || newH <= 0 {
		newW, newH = 1, 1
	}
	img = resize(img, newW, newH)

	if cfg.Threshold {
		img = threshold(img)
	}
	finalW := float64(newW) * 72 / float64(cfg.DPI)
	finalH := float64(newH) * 72 / float64(cfg.DPI)

	xOff := (labelW - finalW) / 2
	yOff := (labelH - finalH) / 2

	return &Result{
		Image:   img,
		X:       xOff,
		Y:       yOff,
		W:       finalW,
		H:       finalH,
		Rotated: doRotate,
		Scale:   scale,
		Content: fmt.Sprintf("%.1fx%.1f", contentW/72, contentH/72),
		Result:  fmt.Sprintf("%.1fx%.1f", finalW/72, finalH/72),
	}
}

// rotate90 turns the image 90 degrees; clockwise picks the direction. A
// clockwise turn maps the top-left pixel to the top-right of the result
// (src(x, y) -> dst(h-1-y, x)); counter-clockwise maps it to the
// bottom-left (src(x, y) -> dst(y, w-1-x)).
func rotate90(src image.Image, clockwise bool) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			if clockwise {
				dst.Set(h-1-y, x, c)
			} else {
				dst.Set(y, w-1-x, c)
			}
		}
	}
	return dst
}

// resize scales with CatmullRom, the sharpest kernel x/image/draw offers,
// because barcodes must stay crisp after downscaling.
func resize(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// threshold maps each pixel to pure black or white by ITU-R 601 luma < 128.
// Thermal printers are binary devices; sending pure black and white stops
// the driver from dithering gray edges.
func threshold(src image.Image) *image.Gray {
	b := src.Bounds()
	gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(bl>>8)
			luma := (19595*r8 + 38470*g8 + 7471*b8) >> 16
			v := byte(255)
			if luma < 128 {
				v = 0
			}
			gray.SetGray(x-b.Min.X, y-b.Min.Y, color.Gray{Y: v})
		}
	}
	return gray
}
