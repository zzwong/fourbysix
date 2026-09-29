package layout

import "image"

// ContentBBox finds the bounding box of pixels with any channel below the
// threshold, sampling every max(1, min(w,h)/200) pixels so large pages stay
// cheap. The returned rect is in pixel coordinates; ok is false for a blank
// page.
func ContentBBox(img image.Image, threshold int) (Rect, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// Adaptive step: scan fewer pixels on large images.
	step := min(w, h) / 200
	if step < 1 {
		step = 1
	}

	minX, minY := w, h
	maxX, maxY := 0, 0

	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(bl>>8)
			if r8 < threshold || g8 < threshold || b8 < threshold {
				px, py := x-b.Min.X, y-b.Min.Y
				minX = min(minX, px)
				minY = min(minY, py)
				maxX = max(maxX, px)
				maxY = max(maxY, py)
			}
		}
	}

	if minX > maxX {
		return Rect{}, false
	}
	return Rect{float64(minX), float64(minY), float64(maxX), float64(maxY)}, true
}
