// Package layout holds the pure, PDF-free geometry and page-analysis logic:
// the Rect type, content detection, rotation policy, split-mode region
// clustering and page-spec parsing. Keeping it free of pdfium makes every
// rule here unit-testable without rendering anything.
package layout

// Rect is an axis-aligned rectangle in points with a top-left origin
// (X0 <= X1, Y0 <= Y1).
type Rect struct {
	X0, Y0, X1, Y1 float64
}

// Width returns X1 - X0.
func (r Rect) Width() float64 { return r.X1 - r.X0 }

// Height returns Y1 - Y0.
func (r Rect) Height() float64 { return r.Y1 - r.Y0 }

// Clip returns the intersection of two rects, which has zero or negative
// size when they do not overlap.
func (r Rect) Clip(o Rect) Rect {
	return Rect{
		X0: max(r.X0, o.X0),
		Y0: max(r.Y0, o.Y0),
		X1: min(r.X1, o.X1),
		Y1: min(r.Y1, o.Y1),
	}
}

// Union returns the smallest rect containing both inputs.
func (r Rect) Union(o Rect) Rect {
	return Rect{
		X0: min(r.X0, o.X0),
		Y0: min(r.Y0, o.Y0),
		X1: max(r.X1, o.X1),
		Y1: max(r.Y1, o.Y1),
	}
}

// Expanded grows the rect by margin on every side.
func (r Rect) Expanded(margin float64) Rect {
	return Rect{r.X0 - margin, r.Y0 - margin, r.X1 + margin, r.Y1 + margin}
}

// Intersects reports whether the two rects share any area. Touching edges or
// corners do not count, so a heading that merely abuts a label's box does
// not block merging, and an empty rect never intersects.
func (r Rect) Intersects(o Rect) bool {
	return !r.Empty() && !o.Empty() &&
		r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1
}

// Empty reports whether the rect has no area, including the inverted case.
func (r Rect) Empty() bool { return r.X1 <= r.X0 || r.Y1 <= r.Y0 }
