package pdfwrite

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
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

func TestWriteRejectsInvalidDimensionsBeforeWriting(t *testing.T) {
	for _, axis := range []string{"width", "height"} {
		for _, value := range []float64{0, math.Copysign(0, -1), -1, math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, math.MaxFloat64 / 2} {
			t.Run(axis+"/"+fmt.Sprint(value), func(t *testing.T) {
				w, h := 288.0, 432.0
				if axis == "width" {
					w = value
				} else {
					h = value
				}
				var buf bytes.Buffer
				buf.WriteString("untouched")
				err := Write(&buf, w, h, []Page{{}})
				if err == nil || !strings.Contains(err.Error(), "finite and positive") {
					t.Fatalf("error = %v, want invalid dimensions", err)
				}
				if got := buf.String(); got != "untouched" {
					t.Fatalf("writer changed: %q", got)
				}
			})
		}
	}
}

// These assertions check PDF number syntax and float64 preservation, not
// a renderer's supported page-size range.
func TestWriteDimensionsUsePDFDecimals(t *testing.T) {
	for _, axis := range []string{"width", "height"} {
		for _, value := range []float64{7.2e-7, 7.2e6, math.SmallestNonzeroFloat32, math.MaxFloat32} {
			t.Run(axis+"/"+fmt.Sprint(value), func(t *testing.T) {
				w, h := 288.0, 432.0
				if axis == "width" {
					w = value
				} else {
					h = value
				}
				var buf bytes.Buffer
				if err := Write(&buf, w, h, []Page{{}}); err != nil {
					t.Fatal(err)
				}
				_, box, ok := strings.Cut(buf.String(), "/MediaBox [")
				if !ok {
					t.Fatal("MediaBox missing")
				}
				box, _, ok = strings.Cut(box, "]")
				if !ok {
					t.Fatal("MediaBox is unterminated")
				}
				fields := strings.Fields(box)
				if len(fields) != 4 || fields[0] != "0" || fields[1] != "0" {
					t.Fatalf("MediaBox = %q", box)
				}
				assertPDFDecimal(t, fields[2], w)
				assertPDFDecimal(t, fields[3], h)
			})
		}
	}
}

func TestContentStreamUsesPDFDecimals(t *testing.T) {
	p := Page{Image: image.NewGray(image.Rect(0, 0, 1, 1)), W: 7.2e-7, H: 7.2e6, X: 1e-8, Y: 1e-8}
	fields := strings.Fields(contentStream(288, 432, p))
	if len(fields) != 11 || fields[0] != "q" || fields[7] != "cm" {
		t.Fatalf("content stream = %q", strings.Join(fields, " "))
	}
	assertPDFDecimal(t, fields[1], p.W)
	assertPDFDecimal(t, fields[4], p.H)
	assertPDFDecimal(t, fields[5], p.X)
	assertPDFDecimal(t, fields[6], 432-p.Y-p.H)
}

func TestWriteRendererNumericBoundaries(t *testing.T) {
	zeroBoundary := float64(math.SmallestNonzeroFloat32) / 2
	infinityBoundary := float64(math.MaxFloat32) + math.Ldexp(1, 103)
	for _, axis := range []string{"width", "height"} {
		for _, tc := range []struct {
			name  string
			value float64
			valid bool
		}{
			{"rounds to zero", zeroBoundary, false},
			{"above zero boundary", math.Nextafter(zeroBoundary, math.Inf(1)), true},
			{"smallest float32", math.SmallestNonzeroFloat32, true},
			{"largest float32", math.MaxFloat32, true},
			{"beyond signed integer range", float64(math.MaxInt32) + 1, true},
			{"below infinity boundary", math.Nextafter(infinityBoundary, 0), true},
			{"rounds to infinity", infinityBoundary, false},
		} {
			t.Run(axis+"/"+tc.name, func(t *testing.T) {
				w, h := 288.0, 432.0
				if axis == "width" {
					w = tc.value
				} else {
					h = tc.value
				}
				var buf bytes.Buffer
				err := Write(&buf, w, h, []Page{{}})
				if !tc.valid {
					if err == nil || buf.Len() != 0 {
						t.Fatalf("error = %v, bytes = %d, want rejection before writing", err, buf.Len())
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				doc, err := pdfdoc.Open(buf.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				defer doc.Close()
				gotW, gotH, err := doc.PageSize(0)
				wantW, wantH := float64(float32(w)), float64(float32(h))
				if err != nil || gotW != wantW || gotH != wantH {
					t.Fatalf("PDFium page = %gx%g, error = %v, want %gx%g", gotW, gotH, err, wantW, wantH)
				}
			})
		}
	}
}

func assertPDFDecimal(t *testing.T, token string, want float64) {
	t.Helper()
	if strings.ContainsAny(token, "eE") {
		t.Fatalf("PDF number %q uses exponent notation", token)
	}
	got, err := strconv.ParseFloat(token, 64)
	if err != nil || got != want {
		t.Fatalf("PDF number %q = %v, error = %v, want %v", token, got, err, want)
	}
}
