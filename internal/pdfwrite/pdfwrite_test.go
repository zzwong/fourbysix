package pdfwrite

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/zzwong/fourbysix/internal/pdfdoc"
)

func solidRGBA(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestWriteRoundTrip(t *testing.T) {
	red := solidRGBA(100, 150, color.RGBA{200, 10, 10, 255})
	gray := image.NewGray(image.Rect(0, 0, 50, 80))
	for i := range gray.Pix {
		gray.Pix[i] = 64
	}

	var buf bytes.Buffer
	err := Write(&buf, 288, 432, []Page{
		{Image: red, X: 10, Y: 20, W: 100, H: 150},
		{Image: gray, Gray: true, X: 5, Y: 5, W: 50, H: 80},
		{}, // blank page
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	doc, err := pdfdoc.Open(buf.Bytes())
	if err != nil {
		t.Fatalf("pdfium could not open the written PDF: %v", err)
	}
	defer doc.Close()

	count, err := doc.PageCount()
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("page count = %d, want 3", count)
	}
	for i := 0; i < count; i++ {
		w, h, err := doc.PageSize(i)
		if err != nil {
			t.Fatalf("PageSize(%d): %v", i, err)
		}
		if w != 288 || h != 432 {
			t.Fatalf("page %d size = %vx%v, want 288x432", i, w, h)
		}
	}

	// Rendering proves the FlateDecode image stream decodes, not just the
	// page tree. The first page must contain the red rectangle.
	img, err := doc.Render(0, 72, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	r, g, b, _ := img.At(30, 30).RGBA()
	if !(r>>8 > 150 && g>>8 < 120 && b>>8 < 120) {
		t.Fatalf("pixel inside the red image = (%d,%d,%d), want reddish", r>>8, g>>8, b>>8)
	}
}
