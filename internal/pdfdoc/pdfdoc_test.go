package pdfdoc

import (
	"testing"

	"github.com/klippa-app/go-pdfium/enums"

	"github.com/zzwong/fourbysix/internal/layout"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

func mustWrite(t *testing.T, pages []testpdf.Page) []byte {
	t.Helper()
	data, err := testpdf.Write(pages)
	if err != nil {
		t.Fatalf("testpdf.Write: %v", err)
	}
	return data
}

func openBytes(t *testing.T, data []byte) *Doc {
	t.Helper()
	d, err := Open(data)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestPageCountAndSize(t *testing.T) {
	data := mustWrite(t, []testpdf.Page{
		{Texts: []testpdf.TextSpec{{X: 10, Y: 10, S: "a"}}},
		{Width: 300, Height: 200},
	})
	d := openBytes(t, data)
	n, err := d.PageCount()
	if err != nil || n != 2 {
		t.Fatalf("PageCount = %d, %v", n, err)
	}
	w, h, err := d.PageSize(0)
	if err != nil || w != 612 || h != 792 {
		t.Errorf("page 0 size = %g x %g", w, h)
	}
	w, h, err = d.PageSize(1)
	if err != nil || w != 300 || h != 200 {
		t.Errorf("page 1 size = %g x %g", w, h)
	}
}

// TestFormObjectMatrix: a form drawing a 50x50 rect
// at its origin, placed with "1 0 0 1 100 100 cm", must surface as a
// graphic at page-space position (100,100)-(150,150) in PDF coordinates,
// i.e. (100,642)-(150,692) in top-left space on a 612x792 page.
func TestFormObjectMatrix(t *testing.T) {
	data := mustWrite(t, []testpdf.Page{{
		Forms: []testpdf.FormSpec{{Matrix: [6]float64{1, 0, 0, 1, 100, 100}}},
	}})
	d := openBytes(t, data)
	graphics, text, err := d.Regions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 0 {
		t.Errorf("unexpected text rects: %v", text)
	}
	if len(graphics) != 1 {
		t.Fatalf("got %d graphics, want 1: %v", len(graphics), graphics)
	}
	want := layout.Rect{X0: 100, Y0: 792 - 150, X1: 150, Y1: 792 - 100}
	got := graphics[0]
	for _, dim := range []struct {
		name string
		g, w float64
	}{
		{"X0", got.X0, want.X0},
		{"Y0", got.Y0, want.Y0},
		{"X1", got.X1, want.X1},
		{"Y1", got.Y1, want.Y1},
	} {
		if dim.g != dim.w {
			t.Errorf("%s = %g, want %g", dim.name, dim.g, dim.w)
		}
	}
}

// TestNestedFormObjectMatrix places a 50x50 form at (20, 30) inside a form
// placed at (100, 100): the rect must land at (120,130)-(170,180) in PDF
// space, which only works when the two placement matrices are composed.
func TestNestedFormObjectMatrix(t *testing.T) {
	data := mustWrite(t, []testpdf.Page{{
		Forms: []testpdf.FormSpec{{
			Matrix: [6]float64{1, 0, 0, 1, 100, 100},
			Inner:  &testpdf.FormSpec{Matrix: [6]float64{1, 0, 0, 1, 20, 30}},
		}},
	}})
	d := openBytes(t, data)
	graphics, _, err := d.Regions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(graphics) != 1 {
		t.Fatalf("got %d graphics, want 1: %v", len(graphics), graphics)
	}
	want := layout.Rect{X0: 120, Y0: 792 - 180, X1: 170, Y1: 792 - 130}
	if graphics[0] != want {
		t.Errorf("nested form graphic = %v, want %v", graphics[0], want)
	}
}

// TestMatrixComposition covers the form-matrix math without needing nested
// form fixtures: composing two translations must equal their sum, and the
// bbox of a rect through a scale/translate matrix must land on the mapped
// corners.
func TestMatrixComposition(t *testing.T) {
	// Compose translate-by-(100,100) after translate-by-(50,50): p' = (x+150, y+150).
	outer := matrix{A: 1, D: 1, E: 100, F: 100}
	inner := matrix{A: 1, D: 1, E: 50, F: 50}
	got := compose(outer, inner)
	if got.E != 150 || got.F != 150 {
		t.Errorf("compose translations = (%g,%g), want (150,150)", got.E, got.F)
	}

	// A rect through a matrix that halves and translates by (10,20):
	// corners (0,0),(40,20) -> (10,20),(30,30).
	m := matrix{A: 0.5, D: 0.5, E: 10, F: 20}
	x0, y0, x1, y1 := m.bbox(0, 0, 40, 20)
	if x0 != 10 || y0 != 20 || x1 != 30 || y1 != 30 {
		t.Errorf("bbox = (%g,%g,%g,%g), want (10,20,30,30)", x0, y0, x1, y1)
	}
}

// TestStrokedPathInFormGeometry checks that a stroked path inside a form is
// measured by its geometry and that the form placement is applied exactly
// once (the path matrix is local to the form; the form matrix composes on
// top of it). A double transform would land the rect at (200,542).
func TestStrokedPathInFormGeometry(t *testing.T) {
	data := mustWrite(t, []testpdf.Page{{
		Forms: []testpdf.FormSpec{{
			Matrix: [6]float64{1, 0, 0, 1, 100, 100},
			FormW:  50,
			FormH:  50,
			Stroke: true,
		}},
	}})
	d := openBytes(t, data)
	graphics, text, err := d.Regions(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 0 {
		t.Errorf("unexpected text rects: %v", text)
	}
	if len(graphics) != 1 {
		t.Fatalf("got %d graphics, want 1: %v", len(graphics), graphics)
	}
	want := layout.Rect{X0: 100, Y0: 792 - 150, X1: 150, Y1: 792 - 100}
	got := graphics[0]
	if abs(got.X0-want.X0) > 0.01 || abs(got.Y0-want.Y0) > 0.01 ||
		abs(got.X1-want.X1) > 0.01 || abs(got.Y1-want.Y1) > 0.01 {
		t.Fatalf("stroked form path = %v, want %v", got, want)
	}
}

func TestObjectTypes(t *testing.T) {
	data := mustWrite(t, []testpdf.Page{{
		Rects:   []testpdf.RectSpec{testpdf.Rect(10, 10, 100, 100)},
		Images:  []testpdf.ImageSpec{{X0: 200, Y0: 200, X1: 300, Y1: 250}},
		Texts:   []testpdf.TextSpec{{X: 30, Y: 400, S: "hello"}},
		Strokes: []testpdf.RectSpec{testpdf.Rect(400, 400, 500, 450)},
	}})
	d := openBytes(t, data)
	objs, err := d.Objects(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 4 {
		t.Fatalf("got %d objects, want 4", len(objs))
	}
	var haveImage, havePath, haveText bool
	for _, o := range objs {
		switch o.Type {
		case enums.FPDF_PAGEOBJ_TEXT:
			haveText = true
		case enums.FPDF_PAGEOBJ_PATH:
			havePath = true
		case enums.FPDF_PAGEOBJ_IMAGE:
			haveImage = true
		}
	}
	if !haveImage || !havePath || !haveText {
		t.Errorf("missing object kinds: image=%v path=%v text=%v", haveImage, havePath, haveText)
	}
}

// TestAmazonShapedFixture rebuilds the three-page Amazon return PDF from
// geometry measured from a real one and checks the split-mode regions come out
// where the real PDF puts them.
func TestAmazonShapedFixture(t *testing.T) {
	data := mustWrite(t, amazonFixture())
	d := openBytes(t, data)
	n, err := d.PageCount()
	if err != nil || n != 3 {
		t.Fatalf("fixture pages = %d, %v", n, err)
	}

	expected := [][]layout.Rect{
		{},                                   // page 1: text-only instructions page, skipped
		{{X0: 28, Y0: 28, X1: 584, Y1: 406}}, // UPS label box
		{ // UN3481 battery label and RMA slip
			{X0: 116, Y0: 28, X1: 498, Y1: 380},
			{X0: 28, Y0: 463, X1: 585, Y1: 689},
		},
	}
	for page, want := range expected {
		w, h, err := d.PageSize(page)
		if err != nil {
			t.Fatal(err)
		}
		graphics, text, err := d.Regions(page)
		if err != nil {
			t.Fatal(err)
		}
		got := layout.FindLabelRegions(graphics, text, layout.Rect{X0: 0, Y0: 0, X1: w, Y1: h})
		if len(got) != len(want) {
			t.Fatalf("page %d: got %d regions %v, want %d", page+1, len(got), got, len(want))
		}
		for i := range got {
			for _, dim := range []struct {
				name string
				g, w float64
			}{
				{"X0", got[i].X0, want[i].X0},
				{"Y0", got[i].Y0, want[i].Y0},
				{"X1", got[i].X1, want[i].X1},
				{"Y1", got[i].Y1, want[i].Y1},
			} {
				if abs(dim.g-dim.w) > 2 {
					t.Errorf("page %d region %d: %s = %g, want %g (+-2)", page+1, i+1, dim.name, dim.g, dim.w)
				}
			}
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// amazonFixture recreates the measured geometry of the Amazon "Online
// Return Center" sample with testpdf objects: browser header/footer text on
// every page, an instructions page, the UPS label page and the battery +
// RMA slip page.
func amazonFixture() []testpdf.Page {
	rect := testpdf.Rect
	border := func(x0, y0, x1, y1 float64) []testpdf.RectSpec {
		return []testpdf.RectSpec{
			rect(x0, y0, x1, y0+2),
			rect(x0, y1-2, x1, y1),
			rect(x0, y0, x0+2, y1),
			rect(x1-2, y0, x1, y1),
		}
	}
	gray := func(x0, y0, x1, y1 float64) testpdf.RectSpec {
		return testpdf.RectColor(x0, y0, x1, y1, 0.9, 0.9, 0.9)
	}
	headerFooter := func() []testpdf.TextSpec {
		return []testpdf.TextSpec{
			{X: 28, Y: 20, S: "Print label — Online Return Center"},
			{X: 28, Y: 770, S: "Page footer — return instructions"},
		}
	}

	pages := make([]testpdf.Page, 3)
	// Page 1: instructions text, a light banner and 3x3 pt bullets.
	pages[0] = testpdf.Page{
		Rects: []testpdf.RectSpec{
			testpdf.RectColor(28, 38, 584, 80, 0.95, 0.95, 0.95),
			rect(30, 136, 33, 139),
			rect(30, 166, 33, 169),
			rect(30, 196, 33, 199),
			rect(30, 241, 33, 244),
			rect(30, 301, 33, 304),
		},
		Texts: append([]testpdf.TextSpec{
			{X: 40, Y: 100, S: "1. Go to Your Orders and select Return or Replace Items."},
			{X: 40, Y: 130, S: "2. Choose the items you want to return."},
			{X: 40, Y: 160, S: "3. Select a reason for the return."},
			{X: 40, Y: 190, S: "4. Choose your refund method and drop-off location."},
			{X: 40, Y: 235, S: "5. Print your return label and packing slip."},
			{X: 40, Y: 295, S: "6. Package the items and attach the return label."},
		}, headerFooter()...),
	}
	// Page 2: gray-bordered UPS label with a label image and an address.
	pages[1] = testpdf.Page{
		Rects:  border(28, 28, 584, 406),
		Images: []testpdf.ImageSpec{{X0: 32, Y0: 32, X1: 580, Y1: 403}},
		Texts: append([]testpdf.TextSpec{
			{X: 40, Y: 60, S: "SHIP TO: 1850 MERCER RD"},
			{X: 40, Y: 90, S: "FROM: ONLINE RETURN CENTER"},
		}, headerFooter()...),
	}
	// Page 3: hazmat label, separator rule, barcode and stroked table.
	hazmat := border(116, 28, 498, 380)
	table := []testpdf.RectSpec{
		testpdf.Rect(28, 608, 585, 689),               // stroked border below
		testpdf.RectColor(30, 610, 583, 687, 1, 1, 1), // white fill: paints nothing
		gray(29, 633, 521, 634),
		gray(521, 633, 583, 634),
	}
	pages[2] = testpdf.Page{
		Rects:   append(append(hazmat, table...), rect(43, 463, 570, 464)),
		Strokes: []testpdf.RectSpec{rect(28, 608, 585, 689)},
		Images: []testpdf.ImageSpec{
			{X0: 119, Y0: 32, X1: 494, Y1: 376},
			{X0: 131, Y0: 474, X1: 482, Y1: 575},
		},
		Texts: append([]testpdf.TextSpec{
			{X: 28, Y: 405, S: "Return to:"},
			{X: 28, Y: 436, S: "Place this label inside your package."},
		}, headerFooter()...),
	}
	return pages
}
