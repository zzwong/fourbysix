// Package pdfwrite emits the output PDF: one label-sized page per page, each
// with a single image XObject drawn with "q w 0 0 h x y cm /Im0 Do Q". It
// writes PDF 1.4 with an uncompressed xref table, so pdfinfo and pdfium can
// both open the result.
package pdfwrite

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"io"
	"math"
	"strconv"
	"strings"
)

// Page is one output label page.
type Page struct {
	Image image.Image // nil for a blank page
	Gray  bool        // true when the image is 1-channel (thresholded)
	X, Y  float64     // target top-left on the label page, points
	W, H  float64     // target size, points
}

// Write renders the pages onto labelW x labelH point pages.
func Write(w io.Writer, labelW, labelH float64, pages []Page) error {
	for _, dimension := range []float64{labelW, labelH} {
		// An empty or overflowing float32 MediaBox makes PDFium silently
		// substitute its default page size instead of the requested label.
		points := float64(float32(dimension))
		if points <= 0 || math.IsNaN(points) || math.IsInf(points, 0) {
			return fmt.Errorf("label dimensions must be finite and positive within PDFium's numeric range")
		}
	}
	b := &builder{}
	catalog := b.add(nil)  // object 1
	pagesObj := b.add(nil) // object 2, referenced as /Parent 2 0 R by every page

	var kids []int
	for _, p := range pages {
		contentObj := b.streamObj(contentStream(labelW, labelH, p))
		var resources int
		if p.Image != nil {
			imgObj := b.imageObj(p.Image, p.Gray)
			resources = b.add([]byte(fmt.Sprintf(
				"<< /XObject << /Im0 %d 0 R >> >>", imgObj)))
		} else {
			resources = b.add([]byte("<< >>"))
		}
		kids = append(kids, b.add([]byte(fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources %d 0 R /Contents %d 0 R >>",
			pdfNumber(labelW), pdfNumber(labelH), resources, contentObj))))
	}

	var kidRefs bytes.Buffer
	for _, k := range kids {
		fmt.Fprintf(&kidRefs, "%d 0 R ", k)
	}
	b.set(catalog, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	b.set(pagesObj, []byte(fmt.Sprintf(
		"<< /Type /Pages /Kids [%s] /Count %d >>", kidRefs.String(), len(kids))))

	_, err := w.Write(b.serialize())
	return err
}

// contentStream is the page content: place the image with a translated,
// scaled cm so its bottom-left lands at the label position.
func contentStream(labelW, labelH float64, p Page) string {
	if p.Image == nil {
		return ""
	}
	return fmt.Sprintf("q %s 0 0 %s %s %s cm /Im0 Do Q\n",
		pdfNumber(p.W), pdfNumber(p.H), pdfNumber(p.X), pdfNumber(labelH-p.Y-p.H))
}

// PDF numbers cannot use exponents; fixed precision could round a positive
// dimension to zero, so keep the shortest round-trippable decimal instead.
func pdfNumber(v float64) string {
	number := strconv.FormatFloat(v, 'f', -1, 64)
	// A decimal point selects PDFium's real-number parser; whole-number
	// tokens otherwise go through its narrower integer representation.
	if !strings.Contains(number, ".") {
		number += ".0"
	}
	return number
}

// builder collects object bodies by number so the catalog and page tree can
// forward-reference page objects without patching bytes in place.
type builder struct {
	objs [][]byte
}

func (b *builder) add(body []byte) int {
	b.objs = append(b.objs, body)
	return len(b.objs)
}

func (b *builder) set(objNum int, body []byte) { b.objs[objNum-1] = body }

func (b *builder) streamObj(content string) int {
	data := []byte(content)
	head := fmt.Sprintf("<< /Length %d >>\nstream\n", len(data))
	body := append([]byte(head), data...)
	body = append(body, "\nendstream"...)
	return b.add(body)
}

// imageObj writes the image as a Flate-compressed 8 bpc XObject, DeviceRGB
// normally or DeviceGray for thresholded images.
func (b *builder) imageObj(img image.Image, gray bool) int {
	pixels, cs := encodeImage(img, gray)

	var comp bytes.Buffer
	// PDF's /FlateDecode wants a zlib stream (RFC 1950), not raw DEFLATE.
	zw := zlib.NewWriter(&comp)
	if _, err := zw.Write(pixels); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	data := comp.Bytes()

	bounds := img.Bounds()
	dict := fmt.Sprintf(
		"/Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/ColorSpace /%s /BitsPerComponent 8 /Filter /FlateDecode",
		bounds.Dx(), bounds.Dy(), cs)
	full := fmt.Sprintf("<< %s /Length %d >>\nstream\n", dict, len(data))
	body := append([]byte(full), data...)
	body = append(body, '\n', 'e', 'n', 'd', 's', 't', 'r', 'e', 'a', 'm')
	return b.add(body)
}

// encodeImage flattens the image to raw scanlines: 3 bytes/pixel RGB, or 1
// byte/pixel gray. Alpha is composited over white because pdfium renders on
// an opaque background and the PDF has no alpha channel.
func encodeImage(img image.Image, gray bool) ([]byte, string) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if gray {
		out := make([]byte, w*h)
		p := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				// Thresholded images are true gray, so any channel works.
				r, _, _, _ := img.At(x, y).RGBA()
				out[p] = byte(r >> 8)
				p++
			}
		}
		return out, "DeviceGray"
	}
	out := make([]byte, w*h*3)
	p := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			out[p] = composite(r, a)
			out[p+1] = composite(g, a)
			out[p+2] = composite(bl, a)
			p += 3
		}
	}
	return out, "DeviceRGB"
}

// composite blends a premultiplied channel over opaque white using its alpha.
func composite(c, a uint32) byte {
	if a == 0xffff {
		return byte(c >> 8)
	}
	src := c >> 8
	num := src*a + 255*(0xffff-a)
	return byte((num + 0x7fff) / 0xffff)
}

func (b *builder) serialize() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	// Binary marker so tools do not treat the file as text.
	buf.WriteString("%\xE2\xE3\xCF\xD3\n")

	offsets := make([]int, len(b.objs))
	for i, body := range b.objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n", i+1)
		buf.Write(body)
		buf.WriteString("\nendobj\n")
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(b.objs)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(b.objs)+1, xref)
	return buf.Bytes()
}
