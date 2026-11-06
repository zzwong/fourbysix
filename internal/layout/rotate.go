package layout

// ShouldRotate decides whether rotating the content 90 degrees improves the
// fit on the label. The direction of the rotation is the caller's business.
//
// Policies:
//   - none: never rotate.
//   - cw90 / ccw90: always rotate.
//   - best-fit: rotate only when the scaled fit improves by more than 10%.
//   - auto: like best-fit, but keeps content wider than 1.2x its height
//     upright, on the assumption that landscape content carries horizontal
//     text that would become unreadable sideways.
func ShouldRotate(contentW, contentH, labelW, labelH float64, policy string) bool {
	if policy == "none" {
		return false
	}
	if policy == "cw90" || policy == "ccw90" {
		return true
	}

	if contentW <= 0 || contentH <= 0 {
		return false
	}

	fitCurrent := min(labelW/contentW, labelH/contentH)
	fitRotated := min(labelW/contentH, labelH/contentW)

	// Only rotate if meaningfully better (>10% improvement).
	if fitRotated <= fitCurrent*1.10 {
		return false
	}

	// best-fit: rotate whenever the aspect ratio improves.
	if policy == "best-fit" {
		return true
	}

	// auto: keep landscape content upright.
	isLandscape := contentW > contentH*1.2
	return !isLandscape
}
