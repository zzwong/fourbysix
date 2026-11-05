package main

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zzwong/fourbysix/internal/testpdf"
)

// ebayReturnPages mimics eBay's printable return label: page 1 is a 4x6
// carrier label in the top-left of a letter page, drawn as text between
// full-width rules with a barcode near the bottom; page 2 is the packing slip,
// text only. Coordinates follow a real eBay FedEx return PDF.
func ebayReturnPages() []testpdf.Page {
	label := testpdf.Page{
		Rects: []testpdf.RectSpec{
			testpdf.Rect(34, 80, 306, 81),
			testpdf.Rect(34, 176, 314, 177),
			testpdf.Rect(34, 281, 314, 282),
		},
		Texts: []testpdf.TextSpec{
			{X: 42, Y: 40, Size: 6, S: "FROM: Sender"},
			{X: 42, Y: 60, Size: 6, S: "123 Example St"},
			{X: 42, Y: 100, Size: 10, S: "TO EXAMPLE RETURNS"},
			{X: 42, Y: 130, Size: 10, S: "1 WAREHOUSE AVE"},
			{X: 42, Y: 160, Size: 10, S: "ANYTOWN ST 00000"},
			{X: 42, Y: 300, Size: 12, S: "TRK# 0000 0000 0000"},
			{X: 80, Y: 450, Size: 8, S: "0000 0000 0 (000 000 0000) 0 00"},
		},
	}
	for x := 58.0; x < 296; x += 4 {
		label.Rects = append(label.Rects, testpdf.Rect(x, 364, x+1.5, 436))
	}
	slip := testpdf.Page{Texts: []testpdf.TextSpec{
		{X: 40, Y: 40, Size: 14, S: "Packing slip"},
		{X: 40, Y: 60, Size: 9, S: "Include this packing slip inside your package"},
		{X: 40, Y: 100, Size: 9, S: "Item: Example item"},
		{X: 40, Y: 120, Size: 9, S: "eBay return ID: 0000000000"},
		{X: 40, Y: 140, Size: 9, S: "eBay order number: 00-00000-00000"},
		{X: 340, Y: 140, Size: 9, S: "Return reason: Example"},
	}}
	return []testpdf.Page{label, slip}
}

var scaleRE = regexp.MustCompile(`Page (\d)/2: [^\n]*?(↻90°)? → scale (\d+)%`)

// Whole-page mode is the eBay path: one 4x6 per page, the label kept upright
// at roughly its printed size.
func TestRunEbayReturnWholePage(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, ebayReturnPages())
	out := filepath.Join(dir, "out.pdf")

	code, stdout, stderr := runCLI(t, src, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if strings.Contains(stderr, "Hint:") {
		t.Fatalf("whole-page run printed a hint: %q", stderr)
	}
	count, _ := pageSizes(t, out)
	if count != 2 {
		t.Fatalf("page count = %d, want 2 (label + packing slip)", count)
	}
	matches := scaleRE.FindAllStringSubmatch(stdout, -1)
	if len(matches) != 2 {
		t.Fatalf("want 2 page report lines, got:\n%s", stdout)
	}
	label := matches[0]
	if label[2] != "" {
		t.Errorf("label was rotated: %s", label[0])
	}
	if scale, _ := strconv.Atoi(label[3]); scale < 90 {
		t.Errorf("label scale = %d%%, want >= 90%% (a 4x6 label printed near actual size)", scale)
	}
}

// --split can't see a text-drawn label as one block, so it must tell the user
// to drop the flag instead of silently printing just the barcode.
func TestRunSplitHintsOnTextDrawnLabel(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, ebayReturnPages())
	out := filepath.Join(dir, "out.pdf")

	code, _, stderr := runCLI(t, src, "--split", "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stderr, "Hint: --split kept under a third of the content on page 1.") {
		t.Fatalf("stderr missing split hint: %q", stderr)
	}
}

func TestRunSplitNoHintWhenLabelsFillPage(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{
		Rects: append(box(100, 30, 500, 380, 2), testpdf.Rect(130, 500, 480, 600)),
		Texts: []testpdf.TextSpec{{X: 30, Y: 440, Size: 12, S: "Place this barcode inside your package"}},
	}})
	out := filepath.Join(dir, "out.pdf")

	code, _, stderr := runCLI(t, src, "--split", "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if strings.Contains(stderr, "Hint:") {
		t.Fatalf("unexpected hint: %q", stderr)
	}
}
