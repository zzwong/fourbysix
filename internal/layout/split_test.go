package layout

import "testing"

// A page with two boxes 30 pt apart: close enough to merge unless text sits
// between them.
func twoBoxes() []Rect {
	return []Rect{
		{X0: 100, Y0: 30, X1: 500, Y1: 380},
		{X0: 130, Y0: 410, X1: 480, Y1: 520},
	}
}

var letter = Rect{X0: 0, Y0: 0, X1: 612, Y1: 792}

func TestFindLabelRegionsTextBetweenBlocksMerge(t *testing.T) {
	text := []Rect{{X0: 30, Y0: 397, X1: 300, Y1: 413}}
	regions := FindLabelRegions(twoBoxes(), text, letter)
	if len(regions) != 2 {
		t.Fatalf("regions = %v, want 2", regions)
	}
	if regions[0].Y1 > 380 || regions[1].Y0 < 410 {
		t.Fatalf("regions not separated at the heading: %v", regions)
	}
}

func TestFindLabelRegionsMergesWithoutText(t *testing.T) {
	regions := FindLabelRegions(twoBoxes(), nil, letter)
	if len(regions) != 1 {
		t.Fatalf("regions = %v, want 1 merged region", regions)
	}
	if regions[0].Y0 > 30 || regions[0].Y1 < 520 {
		t.Fatalf("merged region = %v, want it to cover both boxes", regions[0])
	}
}

func TestFindLabelRegionsDropsSmallClusters(t *testing.T) {
	// A banner (42 pt tall) and a rule (1 pt) are not labels.
	graphics := []Rect{
		{X0: 28, Y0: 38, X1: 584, Y1: 80},
		{X0: 43, Y0: 463, X1: 570, Y1: 464},
	}
	if regions := FindLabelRegions(graphics, nil, letter); len(regions) != 0 {
		t.Fatalf("regions = %v, want none", regions)
	}
}

func TestFindLabelRegionsSortsTopToBottom(t *testing.T) {
	graphics := []Rect{
		{X0: 400, Y0: 500, X1: 580, Y1: 700},
		{X0: 30, Y0: 30, X1: 300, Y1: 300},
		{X0: 30, Y0: 500, X1: 200, Y1: 700},
	}
	regions := FindLabelRegions(graphics, nil, letter)
	if len(regions) != 3 {
		t.Fatalf("regions = %v, want 3", regions)
	}
	want := []Rect{graphics[1], graphics[2], graphics[0]}
	for i := range want {
		if regions[i] != want[i] {
			t.Fatalf("region %d = %v, want %v (sorted by Y0 then X0)", i, regions[i], want[i])
		}
	}
}
