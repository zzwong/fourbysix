package layout

import "sort"

// SplitMergeGap is the whitespace, on each axis, within which graphics join
// one label: a return slip's barcode and item table sit ~34 pt apart.
// SplitMinSize drops clusters smaller than 1 inch on a side, which are
// banners and rules, not labels.
const (
	SplitMergeGap = 45
	SplitMinSize  = 72
)

// gaps returns the whitespace between two rects on each axis, 0 when they
// overlap.
func gaps(a, b Rect) (dx, dy float64) {
	dx = max(0, max(a.X0, b.X0)-min(a.X1, b.X1))
	dy = max(0, max(a.Y0, b.Y0)-min(a.Y1, b.Y1))
	return dx, dy
}

// FindLabelRegions clusters graphic rects into labels and returns one rect
// per label, top to bottom.
//
// Web-printed return PDFs put several labels on one page with headings and
// instructions between them. A heading can sit closer to a label than two
// parts of the same label sit to each other, so whitespace alone cannot
// separate them. Instead, graphics within SplitMergeGap of each other join
// unless the merged box would swallow free-standing text (a heading or instruction
// line), which marks the boundary between two labels. Text only ends up in a
// label when it lies inside that label's graphics, such as an address inside
// a bordered box.
//
// graphics must already be filtered (the "white fill paints nothing" rule
// needs fill colors, which only the pdfium layer can read) and clipped to
// the page; page is the full page rect in the same coordinate space.
func FindLabelRegions(graphics, text []Rect, page Rect) []Rect {
	regions := make([]Rect, 0, len(graphics))
	pageArea := page.Width() * page.Height()
	for _, r := range graphics {
		// Clip each rect to the page, then widen zero-width or zero-height
		// rects so straight lines stay 1 pt thick.
		r = r.Clip(page)
		r.X1 = max(r.X1, r.X0+1)
		r.Y1 = max(r.Y1, r.Y0+1)
		// Drop rects covering >= 90% of the page: full-page backgrounds are
		// not labels.
		if r.Width()*r.Height() < 0.9*pageArea {
			regions = append(regions, r)
		}
	}

	mergeable := func(a, b Rect) bool {
		dx, dy := gaps(a, b)
		if dx > SplitMergeGap || dy > SplitMergeGap {
			return false
		}
		union := a.Union(b)
		for _, t := range text {
			if t.Intersects(union) && !t.Intersects(a) && !t.Intersects(b) {
				return false
			}
		}
		return true
	}

	changed := true
	for changed {
		changed = false
		for i := 0; i < len(regions); i++ {
			for j := i + 1; j < len(regions); {
				if mergeable(regions[i], regions[j]) {
					regions[i] = regions[i].Union(regions[j])
					regions = append(regions[:j], regions[j+1:]...)
					changed = true
				} else {
					j++
				}
			}
		}
	}

	labels := regions[:0]
	for _, r := range regions {
		if min(r.Width(), r.Height()) >= SplitMinSize {
			labels = append(labels, r)
		}
	}

	sort.Slice(labels, func(i, j int) bool {
		if labels[i].Y0 != labels[j].Y0 {
			return labels[i].Y0 < labels[j].Y0
		}
		return labels[i].X0 < labels[j].X0
	})
	return labels
}
