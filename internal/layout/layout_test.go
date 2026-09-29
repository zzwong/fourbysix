package layout

import (
	"image"
	"image/color"
	"testing"
)

// ── Rotation logic tests (port of Python TestRotation) ──────────────────

var label4x6 = [2]float64{4 * 72, 6 * 72}
var label6x4 = [2]float64{6 * 72, 4 * 72}

func TestShouldRotate(t *testing.T) {
	cases := []struct {
		name           string
		w, h           float64
		labelW, labelH float64
		policy         string
		want           bool
	}{
		{"none never rotates (portrait)", 100, 200, label4x6[0], label4x6[1], "none", false},
		{"none never rotates (landscape)", 500, 100, label4x6[0], label4x6[1], "none", false},
		{"cw90 always rotates", 100, 200, label4x6[0], label4x6[1], "cw90", true},
		{"ccw90 always rotates", 100, 200, label4x6[0], label4x6[1], "ccw90", true},
		{"best-fit rotates on real improvement", 500, 100, label4x6[0], label4x6[1], "best-fit", true},
		{"best-fit keeps already-portrait content", 200, 400, label4x6[0], label4x6[1], "best-fit", false},
		{"best-fit skips marginal improvement", 330, 310, label4x6[0], label4x6[1], "best-fit", false},
		{"auto keeps landscape upright", 500, 100, label4x6[0], label4x6[1], "auto", false},
		{"auto rotates portrait content that benefits", 250, 350, label6x4[0], label6x4[1], "auto", true},
		{"zero content never rotates", 0, 0, label4x6[0], label4x6[1], "auto", false},
		{"zero width never rotates", 0, 100, label4x6[0], label4x6[1], "auto", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldRotate(c.w, c.h, c.labelW, c.labelH, c.policy); got != c.want {
				t.Errorf("ShouldRotate(%v, %v, %v, %v, %q) = %v, want %v",
					c.w, c.h, c.labelW, c.labelH, c.policy, got, c.want)
			}
		})
	}
}

// ── Content detection tests (port of Python TestContentDetection) ───────

func whiteImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	return img
}

func paintBlack(img *image.RGBA, x0, y0, x1, y1 int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
}

func TestContentBBox(t *testing.T) {
	t.Run("blank page reports none", func(t *testing.T) {
		_, ok := ContentBBox(whiteImage(612, 792), 250)
		if ok {
			t.Error("blank image should report no content")
		}
	})

	t.Run("single black square", func(t *testing.T) {
		img := whiteImage(400, 400)
		paintBlack(img, 200, 200, 399, 399) // 200x200 black square
		r, ok := ContentBBox(img, 250)
		if !ok {
			t.Fatal("square not detected")
		}
		// The scan samples at step 2, so the bbox is within one sample of
		// the paint: 198..400.
		if w, h := r.Width(), r.Height(); w < 198 || w > 202 || h < 198 || h > 202 {
			t.Errorf("bbox %v, want ~200x200", r)
		}
	})

	t.Run("single gray element between thresholds", func(t *testing.T) {
		img := whiteImage(100, 100)
		for y := 10; y < 30; y++ {
			for x := 10; x < 90; x++ {
				img.SetRGBA(x, y, color.RGBA{240, 240, 240, 255})
			}
		}
		r, ok := ContentBBox(img, 250)
		if !ok {
			t.Fatal("gray element not detected")
		}
		if r.Width() < 70 || r.Width() > 82 || r.Height() < 8 || r.Height() > 22 {
			t.Errorf("bbox %v, want ~80x20", r)
		}
	})
}

// ── ParsePages tests (port of Python TestParsePages) ────────────────────

func TestParsePages(t *testing.T) {
	t.Run("empty spec selects all", func(t *testing.T) {
		got, err := ParsePages("", 3)
		if err != nil || !equalInts(got, []int{0, 1, 2}) {
			t.Errorf("ParsePages(\"\", 3) = %v, %v", got, err)
		}
	})

	t.Run("ranges and singles", func(t *testing.T) {
		got, err := ParsePages("2-3,5", 5)
		if err != nil || !equalInts(got, []int{1, 2, 4}) {
			t.Errorf("ParsePages(\"2-3,5\", 5) = %v, %v", got, err)
		}
	})

	for _, spec := range []string{"0", "4", "3-2", "a", "1-x"} {
		t.Run("invalid "+spec, func(t *testing.T) {
			if _, err := ParsePages(spec, 3); err == nil {
				t.Errorf("ParsePages(%q, 3) should fail", spec)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── Split mode tests (port of Python TestSplit, pure rects) ─────────────

// borderBox returns the four thin filled rects a browser draws for a dashed
// border, porting the Python _box helper (t = 2 pt).
func borderBox(x0, y0, x1, y1 float64) []Rect {
	const t = 2
	return []Rect{
		{X0: x0, Y0: y0, X1: x1, Y1: y0 + t},
		{X0: x0, Y0: y1 - t, X1: x1, Y1: y1},
		{X0: x0, Y0: y0, X1: x0 + t, Y1: y1},
		{X0: x1 - t, Y0: y0, X1: x1, Y1: y1},
	}
}

func page612() Rect { return Rect{X0: 0, Y0: 0, X1: 612, Y1: 792} }

func TestFindLabelRegions(t *testing.T) {
	t.Run("heading between boxes separates labels", func(t *testing.T) {
		// 30 pt gap: close enough to merge if the heading were ignored. The
		// text block is the real bbox pymupdf reports for 12 pt Helvetica at
		// baseline 398: y0 = 398-12.9, y1 = 398+3.6, clear of box2 at y=410.
		graphics := append(borderBox(100, 30, 500, 380), Rect{X0: 130, Y0: 410, X1: 480, Y1: 520})
		text := []Rect{{X0: 30, Y0: 385.1, X1: 162.7, Y1: 401.6}}
		regions := FindLabelRegions(graphics, text, page612())
		if len(regions) != 2 {
			t.Fatalf("got %d regions, want 2: %v", len(regions), regions)
		}
		if regions[0].Y1 > 380 || regions[1].Y0 < 410 {
			t.Errorf("regions not separated: %v", regions)
		}
	})

	t.Run("nearby graphics without text between merge", func(t *testing.T) {
		// Barcode image and item table 34 pt apart belong to one slip.
		graphics := []Rect{{X0: 130, Y0: 470, X1: 480, Y1: 575}, {X0: 30, Y0: 609, X1: 580, Y1: 690}}
		regions := FindLabelRegions(graphics, nil, page612())
		if len(regions) != 1 {
			t.Fatalf("got %d regions, want 1", len(regions))
		}
		if regions[0].Y0 > 470 || regions[0].Y1 < 690 {
			t.Errorf("merged region %v should span the two rects", regions[0])
		}
	})

	t.Run("text inside border stays with label", func(t *testing.T) {
		graphics := append(borderBox(50, 50, 400, 300), Rect{X0: 80, Y0: 200, X1: 300, Y1: 280})
		// "SHIP TO: 1850 MERCER RD" at (80, 100); the real pymupdf block bbox.
		text := []Rect{{X0: 80, Y0: 87.1, X1: 237.4, Y1: 103.6}}
		regions := FindLabelRegions(graphics, text, page612())
		if len(regions) != 1 {
			t.Fatalf("got %d regions, want 1: %v", len(regions), regions)
		}
	})

	t.Run("text-only and banner pages have no labels", func(t *testing.T) {
		// Page 1: text only.
		if got := FindLabelRegions(nil, []Rect{{X0: 30, Y0: 100, X1: 200, Y1: 114}}, page612()); len(got) != 0 {
			t.Errorf("text-only page yielded %v", got)
		}
		// Page 2: a 40-pt-tall banner is smaller than 72 pt on a side.
		got := FindLabelRegions([]Rect{{X0: 30, Y0: 40, X1: 580, Y1: 80}}, nil, page612())
		if len(got) != 0 {
			t.Errorf("banner page yielded %v", got)
		}
	})

	t.Run("full-page background is dropped", func(t *testing.T) {
		graphics := []Rect{{X0: 0, Y0: 0, X1: 612, Y1: 792}} // covers 100% of page
		if got := FindLabelRegions(graphics, nil, page612()); len(got) != 0 {
			t.Errorf("full-page rect yielded %v", got)
		}
	})

	t.Run("zero-height line is widened and can merge", func(t *testing.T) {
		// A horizontal rule (zero height) next to a barcode joins the slip.
		graphics := []Rect{{X0: 43, Y0: 463, X1: 570, Y1: 463}, {X0: 131, Y0: 474, X1: 482, Y1: 575}}
		regions := FindLabelRegions(graphics, nil, page612())
		if len(regions) != 1 {
			t.Fatalf("got %d regions, want 1: %v", len(regions), regions)
		}
		if regions[0].Y0 != 463 {
			t.Errorf("region should start at the rule: %v", regions[0])
		}
	})

	t.Run("regions sorted top to bottom then left to right", func(t *testing.T) {
		graphics := []Rect{
			{X0: 200, Y0: 100, X1: 400, Y1: 300},
			{X0: 50, Y0: 100, X1: 150, Y1: 250},
			{X0: 100, Y0: 400, X1: 300, Y1: 500},
		}
		regions := FindLabelRegions(graphics, nil, page612())
		if len(regions) != 3 {
			t.Fatalf("got %d regions", len(regions))
		}
		if regions[0].X0 != 50 || regions[1].X0 != 200 || regions[2].Y0 != 400 {
			t.Errorf("sort wrong: %v", regions)
		}
	})

	t.Run("wrong gap on one axis blocks the merge", func(t *testing.T) {
		graphics := []Rect{{X0: 100, Y0: 30, X1: 500, Y1: 380}, {X0: 130, Y0: 500, X1: 480, Y1: 600}}
		if got := FindLabelRegions(graphics, nil, page612()); len(got) != 2 {
			// dy gap 120 > 45.
			t.Errorf("got %d regions, want 2", len(got))
		}
	})
}

// ── Heading rule matters ────────────────────────────────────────────────

// TestHeadingRuleBlocksMerge documents that the "no free text between" rule
// is what keeps the two boxes in the fixture apart; without it the 30 pt gap
// would merge them. The candidate returns true when a merge happens.
func TestHeadingRuleBlocksMerge(t *testing.T) {
	mergeWith := func(text []Rect) bool {
		graphics := append(borderBox(100, 30, 500, 380), Rect{X0: 130, Y0: 410, X1: 480, Y1: 520})
		return len(FindLabelRegions(graphics, text, page612())) == 1
	}

	if !mergeWith(nil) {
		t.Fatal("boxes 30 pt apart should merge when nothing sits between them")
	}
	if mergeWith([]Rect{{X0: 30, Y0: 385.1, X1: 162.7, Y1: 401.6}}) {
		t.Fatal("heading did not separate the boxes")
	}
}
