// Package testpdf writes minimal PDFs from a small spec of filled and
// stroked rects, Helvetica text, solid-gray image XObjects and a form
// XObject placed with a translate cm. It exists so tests can build fixtures
// with a known object structure without shipping real label PDFs (which
// carry addresses and tracking numbers). All spec coordinates are top-left
// origin points; the writer flips them to PDF space.
package testpdf

import (
	"bytes"
	"fmt"
	"strings"
)

// RectSpec is a filled or stroked rectangle in top-left-origin points.
// Colors are 0..1 component values; zero is black.
type RectSpec struct {
	X0, Y0, X1, Y1 float64
	R, G, B        float64
}

// Rect is a convenience constructor for a black filled rect.
func Rect(x0, y0, x1, y1 float64) RectSpec { return RectSpec{X0: x0, Y0: y0, X1: x1, Y1: y1} }

// RectColor is like Rect but with an explicit fill color.
func RectColor(x0, y0, x1, y1, r, g, b float64) RectSpec {
	return RectSpec{X0: x0, Y0: y0, X1: x1, Y1: y1, R: r, G: g, B: b}
}

// TextSpec draws one line of Helvetica with its baseline at (X, Y) in
// top-left-origin points.
type TextSpec struct {
	X, Y, Size float64
	S          string
}

// ImageSpec places a solid-gray image XObject so it covers the given
// top-left-origin rectangle.
type ImageSpec struct {
	X0, Y0, X1, Y1 float64
	Gray           uint8
}

// FormSpec places a form XObject whose content is a black rect filling the
// form space, filled by default or stroked with Stroke. Matrix maps form
// space to page space (bottom-left origin), so 1 0 0 1 100 100 translates by
// (100, 100).
type FormSpec struct {
	Matrix       [6]float64
	FormW, FormH float64
	Stroke       bool
}

// Page is one page of the fixture.
type Page struct {
	Width, Height float64 // points; zero dimensions default to letter (612x792)
	Rects         []RectSpec
	Strokes       []RectSpec
	Texts         []TextSpec
	Images        []ImageSpec
	Forms         []FormSpec
}

const defaultPageW, defaultPageH = 612, 792

// Write renders the pages into a PDF 1.4 byte stream.
func Write(pages []Page) ([]byte, error) {
	b := &builder{}
	catalog := b.add(nil)  // object 1: filled in once the page tree exists
	pagesObj := b.add(nil) // object 2: page objects hardcode /Parent 2 0 R
	font := b.add([]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"))

	var kids []int
	for _, p := range pages {
		w, h := p.Width, p.Height
		if w == 0 || h == 0 {
			w, h = defaultPageW, defaultPageH
		}

		// XObjects first so the resources object can reference them.
		var images, forms []int
		for _, im := range p.Images {
			images = append(images, b.imageObj(im.Gray))
		}
		for _, f := range p.Forms {
			forms = append(forms, b.formObj(f.FormW, f.FormH, f.Stroke))
		}
		resources := b.resources(font, images, forms)
		content := b.streamObj(buildContent(w, h, p))
		kids = append(kids, b.add([]byte(fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %g %g] /Resources %d 0 R /Contents %d 0 R >>",
			w, h, resources, content))))
	}

	var kidRefs strings.Builder
	for _, k := range kids {
		fmt.Fprintf(&kidRefs, "%d 0 R ", k)
	}
	b.set(catalog, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	b.set(pagesObj, []byte(fmt.Sprintf(
		"<< /Type /Pages /Kids [%s] /Count %d >>", kidRefs.String(), len(kids))))
	return b.serialize()
}

func buildContent(w, h float64, p Page) string {
	var s strings.Builder
	// PDF space is bottom-left origin: flip every top-left coordinate.
	for _, r := range p.Rects {
		fmt.Fprintf(&s, "%g %g %g rg %g %g %g %g re f\n",
			r.R, r.G, r.B,
			r.X0, h-r.Y1, r.X1-r.X0, r.Y1-r.Y0)
	}
	for _, r := range p.Strokes {
		fmt.Fprintf(&s, "%g %g %g RG %g %g %g %g re S\n",
			r.R, r.G, r.B,
			r.X0, h-r.Y1, r.X1-r.X0, r.Y1-r.Y0)
	}
	for _, t := range p.Texts {
		size := t.Size
		if size == 0 {
			size = 12
		}
		fmt.Fprintf(&s, "BT /F1 %g Tf %g %g Td (%s) Tj ET\n",
			size, t.X, h-t.Y, escapeText(t.S))
	}
	for i, im := range p.Images {
		fmt.Fprintf(&s, "q %g 0 0 %g %g %g cm /Im%d Do Q\n",
			im.X1-im.X0, im.Y1-im.Y0, im.X0, h-im.Y1, i)
	}
	for i, f := range p.Forms {
		fmt.Fprintf(&s, "q %g %g %g %g %g %g cm /Fm%d Do Q\n",
			f.Matrix[0], f.Matrix[1], f.Matrix[2], f.Matrix[3], f.Matrix[4], f.Matrix[5], i)
	}
	return s.String()
}

func escapeText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `(`, `\(`)
	s = strings.ReplaceAll(s, `)`, `\)`)
	return s
}

// builder collects object bodies by number, so forward references (a page
// tree pointing at page objects, a catalog pointing at the page tree) can be
// filled in before serialization.
type builder struct {
	objs [][]byte
}

// add appends an object body and returns its 1-based object number.
func (b *builder) add(body []byte) int {
	b.objs = append(b.objs, body)
	return len(b.objs)
}

// set replaces the body of an object reserved with add(nil).
func (b *builder) set(objNum int, body []byte) { b.objs[objNum-1] = body }

func (b *builder) serialize() ([]byte, error) {
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
	return buf.Bytes(), nil
}

func (b *builder) streamObj(content string) int {
	data := []byte(content)
	body := fmt.Sprintf("<< /Length %d >>\nstream\n", len(data))
	full := append([]byte(body), data...)
	full = append(full, "\nendstream"...)
	return b.add(full)
}

func (b *builder) resources(font int, images, forms []int) int {
	var xo strings.Builder
	for i, im := range images {
		fmt.Fprintf(&xo, "/Im%d %d 0 R ", i, im)
	}
	for i, fm := range forms {
		fmt.Fprintf(&xo, "/Fm%d %d 0 R ", i, fm)
	}
	return b.add([]byte(fmt.Sprintf(
		"<< /Font << /F1 %d 0 R >> /XObject << %s>> >>", font, xo.String())))
}

// imageObj writes a 16x16 solid-gray image. Content detection only cares
// that the image is present and not white, but honoring the requested shade
// keeps fixtures faithful to the color they claim.
func (b *builder) imageObj(gray uint8) int {
	const n = 16
	body := make([]byte, n*n)
	for i := range body {
		body[i] = gray
	}
	dict := "/Type /XObject /Subtype /Image /Width 16 /Height 16 " +
		"/ColorSpace /DeviceGray /BitsPerComponent 8"
	full := fmt.Sprintf("<< %s /Length %d >>\nstream\n", dict, len(body))
	data := append([]byte(full), body...)
	data = append(data, '\n', 'e', 'n', 'd', 's', 't', 'r', 'e', 'a', 'm')
	return b.add(data)
}

// formObj writes a form XObject whose content is a black rect filling the
// form space. The page content stream decides the placement matrix, not the
// form itself.
func (b *builder) formObj(w, h float64, stroke bool) int {
	if w == 0 || h == 0 {
		w, h = 50, 50
	}
	op := "f"
	if stroke {
		op = "S"
	}
	content := fmt.Sprintf("0 0 %g %g re %s\n", w, h, op)
	dict := fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 %g %g] /Resources << >>", w, h)
	full := fmt.Sprintf("<< %s /Length %d >>\nstream\n", dict, len(content))
	data := append([]byte(full), content...)
	data = append(data, '\n', 'e', 'n', 'd', 's', 't', 'r', 'e', 'a', 'm')
	return b.add(data)
}
