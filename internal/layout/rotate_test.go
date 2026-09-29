package layout

import "testing"

func TestShouldRotate(t *testing.T) {
	const (
		l4x6W, l4x6H = 4 * 72.0, 6 * 72.0
		l6x4W, l6x4H = 6 * 72.0, 4 * 72.0
	)
	tests := []struct {
		name               string
		contentW, contentH float64
		labelW, labelH     float64
		policy             string
		want               bool
	}{
		{"none policy never rotates portrait", 100, 200, l4x6W, l4x6H, "none", false},
		{"none policy never rotates landscape", 500, 100, l4x6W, l4x6H, "none", false},
		{"cw90 always rotates", 100, 200, l4x6W, l4x6H, "cw90", true},
		{"ccw90 always rotates", 100, 200, l4x6W, l4x6H, "ccw90", true},
		// 500x100 on 4x6: 0.864 rotated vs 0.576 current, a 1.5x gain.
		{"best-fit rotates on improvement", 500, 100, l4x6W, l4x6H, "best-fit", true},
		// 200x400 already portrait: rotation would make the fit worse.
		{"best-fit keeps portrait upright", 200, 400, l4x6W, l4x6H, "best-fit", false},
		// 330x310 improves only 6.4%, below the 10% bar.
		{"best-fit skips marginal improvement", 330, 310, l4x6W, l4x6H, "best-fit", false},
		{"auto keeps landscape upright", 500, 100, l4x6W, l4x6H, "auto", false},
		// 275x350 portrait on a landscape label: rotation helps and the
		// content is not landscape, so auto rotates.
		{"auto rotates portrait onto landscape label", 275, 350, l6x4W, l6x4H, "auto", true},
		{"auto with zero content", 0, 0, l4x6W, l4x6H, "auto", false},
		{"auto with zero height", 0, 100, l4x6W, l4x6H, "auto", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldRotate(tt.contentW, tt.contentH, tt.labelW, tt.labelH, tt.policy); got != tt.want {
				t.Fatalf("ShouldRotate(%v, %v, %v, %v, %q) = %v, want %v",
					tt.contentW, tt.contentH, tt.labelW, tt.labelH, tt.policy, got, tt.want)
			}
		})
	}
}
