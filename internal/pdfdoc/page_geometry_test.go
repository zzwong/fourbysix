package pdfdoc

import (
	"math"
	"testing"

	"github.com/klippa-app/go-pdfium/enums"

	"github.com/zzwong/fourbysix/internal/layout"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

// These literals use a negative, fractional effective box. They deliberately
// retain sub-point offsets that a 72-DPI integer device conversion would lose.
func TestPageGeometryFractional(t *testing.T) {
	object := Object{Left: -0.125, Bottom: 19.75, Right: 20.125, Top: 50.25}
	for _, tc := range []struct {
		rotation enums.FPDF_PAGE_ROTATION
		page     layout.Rect
		want     layout.Rect
	}{
		{0, layout.Rect{X1: 100, Y1: 200}, layout.Rect{X0: 10.125, Y0: 129.25, X1: 30.375, Y1: 159.75}},
		{1, layout.Rect{X1: 200, Y1: 100}, layout.Rect{X0: 40.25, Y0: 10.125, X1: 70.75, Y1: 30.375}},
		{2, layout.Rect{X1: 100, Y1: 200}, layout.Rect{X0: 69.625, Y0: 40.25, X1: 89.875, Y1: 70.75}},
		{3, layout.Rect{X1: 200, Y1: 100}, layout.Rect{X0: 129.25, Y0: 69.625, X1: 159.75, Y1: 89.875}},
	} {
		g, err := newPageGeometry(-10.25, -20.5, 89.75, 179.5, tc.rotation)
		if err != nil {
			t.Fatal(err)
		}
		if g.bounds != tc.page {
			t.Errorf("rotation %d page = %v, want %v", tc.rotation, g.bounds, tc.page)
		}
		got, visible, err := g.region(object)
		if err != nil || !visible || got != tc.want {
			t.Errorf("rotation %d region = %v, %v, %v; want %v, true, nil", tc.rotation, got, visible, err, tc.want)
		}
	}
}

func TestPageGeometryClippingAndLines(t *testing.T) {
	g, err := newPageGeometry(100, 200, 500, 700, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		object  Object
		want    layout.Rect
		visible bool
	}{
		{"partial", Object{Left: 80, Bottom: 180, Right: 140, Top: 250}, layout.Rect{X0: 0, Y0: 0, X1: 50, Y1: 40}, true},
		{"outside", Object{Left: 0, Bottom: 300, Right: 90, Top: 350}, layout.Rect{}, false},
		{"horizontal line", Object{Left: 140, Bottom: 500, Right: 340, Top: 500}, layout.Rect{X0: 300, Y0: 40, X1: 300, Y1: 240}, true},
		{"vertical line", Object{Left: 140, Bottom: 500, Right: 140, Top: 650}, layout.Rect{X0: 300, Y0: 40, X1: 450, Y1: 40}, true},
		{"boundary line", Object{Left: 100, Bottom: 200, Right: 100, Top: 700}, layout.Rect{X0: 0, Y0: 0, X1: 500, Y1: 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, visible, err := g.region(tc.object)
			if err != nil || visible != tc.visible || (visible && got != tc.want) {
				t.Errorf("region = %v, %v, %v; want %v, %v, nil", got, visible, err, tc.want, tc.visible)
			}
		})
	}
}

func TestPageGeometryRejectsInvalid(t *testing.T) {
	for _, box := range [][4]float64{
		{0, 0, 0, 10}, {0, 10, 10, 0}, {0, 0, math.NaN(), 10},
		{math.Inf(-1), 0, 10, 10}, {0, 0, 10, math.Inf(1)},
		{-math.MaxFloat64, 0, math.MaxFloat64, 10},
	} {
		if _, err := newPageGeometry(box[0], box[1], box[2], box[3], 0); err == nil {
			t.Errorf("accepted invalid page box %v", box)
		}
	}
	if _, err := newPageGeometry(0, 0, 10, 10, 4); err == nil {
		t.Error("accepted invalid page rotation")
	}
	g, err := newPageGeometry(0, 0, 10, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.region(Object{Right: math.NaN(), Top: 1}); err == nil {
		t.Error("accepted nonfinite object bounds")
	}
}

// The raw/form transform contract must survive page normalization, and
// zero-height stroked geometry must still reach layout's widening rule.
func TestRegionsRotatedNestedFormAndLine(t *testing.T) {
	crop := testpdf.Rect(100, 92, 500, 592)
	d := openBytes(t, mustWrite(t, []testpdf.Page{{
		CropBox: &crop, Rotate: 90,
		Strokes: []testpdf.RectSpec{testpdf.Rect(140, 292, 340, 292)},
		Forms: []testpdf.FormSpec{{
			Matrix: [6]float64{1, 0, 0, 1, 100, 400},
			Inner:  &testpdf.FormSpec{Matrix: [6]float64{1, 0, 0, 1, 20, 30}},
		}},
	}}))
	objects, err := d.Objects(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 {
		t.Fatalf("Objects = %v, want line and nested form leaf", objects)
	}
	if o := objects[1]; o.Left != 120 || o.Bottom != 430 || o.Right != 170 || o.Top != 480 {
		t.Errorf("raw nested form bounds = %v, want (120,430)-(170,480)", o)
	}
	graphics, text, err := d.Regions(0)
	if err != nil {
		t.Fatal(err)
	}
	want := []layout.Rect{
		{X0: 300, Y0: 40, X1: 300, Y1: 240},
		{X0: 230, Y0: 20, X1: 280, Y1: 70},
	}
	if len(text) != 0 || len(graphics) != len(want) {
		t.Fatalf("Regions = %v, %v; want %v, no text", graphics, text, want)
	}
	for i := range want {
		if graphics[i] != want[i] {
			t.Errorf("graphic %d = %v, want %v", i, graphics[i], want[i])
		}
	}
}

func TestRegionsEffectivePageBounds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		crop          testpdf.RectSpec
		rotate        int
		rects         []testpdf.RectSpec
		width, height float64
		want          layout.Rect
	}{
		{
			name: "crop beyond media",
			crop: testpdf.Rect(-10, -20, 650, 820),
			rects: []testpdf.RectSpec{
				testpdf.Rect(-5, -5, 20, 30),
				testpdf.Rect(620, 100, 650, 200),
			},
			width: 612, height: 792,
			want: layout.Rect{X0: 0, Y0: 0, X1: 20, Y1: 30},
		},
		{
			name: "fractional crop and rotation",
			crop: testpdf.Rect(100.25, 92.5, 500.75, 592.25), rotate: 270,
			rects: []testpdf.RectSpec{testpdf.Rect(140.375, 142.75, 340.125, 292.625)},
			width: 499.75, height: 400.5,
			want: layout.Rect{X0: 50.25, Y0: 160.625, X1: 200.125, Y1: 360.375},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := openBytes(t, mustWrite(t, []testpdf.Page{{CropBox: &tc.crop, Rotate: tc.rotate, Rects: tc.rects}}))
			w, h, err := d.PageSize(0)
			if err != nil || w != tc.width || h != tc.height {
				t.Fatalf("PageSize = %g x %g, %v; want %g x %g", w, h, err, tc.width, tc.height)
			}
			objects, err := d.Objects(0)
			if err != nil || len(objects) != len(tc.rects) {
				t.Fatalf("Objects = %v, %v; want all %d raw objects", objects, err, len(tc.rects))
			}
			graphics, text, err := d.Regions(0)
			if err != nil || len(graphics) != 1 || len(text) != 0 {
				t.Fatalf("Regions = %v, %v, %v; want one graphic", graphics, text, err)
			}
			if graphics[0] != tc.want {
				t.Errorf("graphic = %v, want %v", graphics[0], tc.want)
			}
		})
	}
}

// The nearest ancestor wins independently for boxes and rotation. A literal
// leaf Rotate 0 must override 90, not be mistaken for an absent attribute.
func TestRegionsInheritedGeometry(t *testing.T) {
	data, err := testpdf.InheritedPageGeometry()
	if err != nil {
		t.Fatal(err)
	}
	d := openBytes(t, data)
	if count, err := d.PageCount(); err != nil || count != 3 {
		t.Fatalf("PageCount = %d, %v; want 3", count, err)
	}
	for page, tc := range []struct {
		name          string
		width, height float64
		want          layout.Rect
	}{
		{"nearest parent", 500, 400, layout.Rect{X0: 300, Y0: 40, X1: 450, Y1: 240}},
		{"explicit leaf zero", 400, 500, layout.Rect{X0: 40, Y0: 50, X1: 240, Y1: 200}},
		{"negative media origin", 550, 720, layout.Rect{X0: 160, Y0: 520, X1: 360, Y1: 670}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := d.PageSize(page)
			if err != nil || w != tc.width || h != tc.height {
				t.Fatalf("PageSize = %g x %g, %v; want %g x %g", w, h, err, tc.width, tc.height)
			}
			objects, err := d.Objects(page)
			if err != nil || len(objects) != 1 {
				t.Fatalf("Objects = %v, %v; want one raw path", objects, err)
			}
			if o := objects[0]; o.Left != 140 || o.Bottom != 500 || o.Right != 340 || o.Top != 650 {
				t.Errorf("raw bounds = %v, want (140,500)-(340,650)", o)
			}
			graphics, text, err := d.Regions(page)
			if err != nil || len(graphics) != 1 || len(text) != 0 {
				t.Fatalf("Regions = %v, %v, %v; want one graphic", graphics, text, err)
			}
			if graphics[0] != tc.want {
				t.Errorf("graphic = %v, want %v", graphics[0], tc.want)
			}
		})
	}
}
