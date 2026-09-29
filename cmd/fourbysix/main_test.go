package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzwong/fourbysix/internal/pdfdoc"
	"github.com/zzwong/fourbysix/internal/testpdf"
)

// writeFixture builds a PDF with testpdf and writes it into dir.
func writeFixture(t *testing.T, dir string, pages []testpdf.Page) string {
	t.Helper()
	data, err := testpdf.Write(pages)
	if err != nil {
		t.Fatalf("testpdf.Write: %v", err)
	}
	path := filepath.Join(dir, "return.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// box is the four thin filled rects a browser draws for a border.
func box(x0, y0, x1, y1, t float64) []testpdf.RectSpec {
	return []testpdf.RectSpec{
		testpdf.Rect(x0, y0, x1, y0+t),
		testpdf.Rect(x0, y1-t, x1, y1),
		testpdf.Rect(x0, y0, x0+t, y1),
		testpdf.Rect(x1-t, y0, x1, y1),
	}
}

// runCLI isolates the config lookup in a temp XDG dir so tests never read the
// developer's real config.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// pageSizes reopens the output with pdfium and returns the page count and
// sizes.
func pageSizes(t *testing.T, path string) (int, [][2]float64) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	doc, err := pdfdoc.Open(data)
	if err != nil {
		t.Fatalf("pdfium could not open output: %v", err)
	}
	defer doc.Close()
	count, err := doc.PageCount()
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	var sizes [][2]float64
	for i := 0; i < count; i++ {
		w, h, err := doc.PageSize(i)
		if err != nil {
			t.Fatalf("PageSize(%d): %v", i, err)
		}
		sizes = append(sizes, [2]float64{w, h})
	}
	return count, sizes
}

func TestRunOutputPagesAreLabelSized(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{
		Texts: []testpdf.TextSpec{{X: 100, Y: 300, Size: 12, S: "Test content here"}},
	}})
	out := filepath.Join(dir, "out.pdf")

	code, stdout, stderr := runCLI(t, src, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "Saved: "+out) {
		t.Fatalf("stdout missing save line:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Note: No config at") {
		t.Fatalf("expected the no-config note on stderr, got:\n%s", stderr)
	}
	count, sizes := pageSizes(t, out)
	if count != 1 || sizes[0] != [2]float64{288, 432} {
		t.Fatalf("pages = %d %v, want 1 at 288x432", count, sizes)
	}
}

func TestRunCustomLabelSize(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{
		Texts: []testpdf.TextSpec{{X: 50, Y: 100, Size: 12, S: "test"}},
	}})
	out := filepath.Join(dir, "out.pdf")

	code, _, stderr := runCLI(t, src, "--label", "2x4", "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	count, sizes := pageSizes(t, out)
	if count != 1 || sizes[0] != [2]float64{144, 288} {
		t.Fatalf("pages = %d %v, want 1 at 144x288", count, sizes)
	}
}

func TestRunBlankPageProducesBlankLabel(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{}}) // empty page
	out := filepath.Join(dir, "out.pdf")

	code, stdout, stderr := runCLI(t, src, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "blank → empty label") {
		t.Fatalf("stdout missing blank line:\n%s", stdout)
	}
	count, sizes := pageSizes(t, out)
	if count != 1 || sizes[0] != [2]float64{288, 432} {
		t.Fatalf("pages = %d %v, want 1 blank label", count, sizes)
	}
}

func TestRunProcessesEveryPage(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{
		{Texts: []testpdf.TextSpec{{X: 100, Y: 300, Size: 12, S: "Page 1"}}},
		{Texts: []testpdf.TextSpec{{X: 200, Y: 400, Size: 12, S: "Page 2"}}},
		{Texts: []testpdf.TextSpec{{X: 150, Y: 200, Size: 12, S: "Page 3"}}},
	})
	out := filepath.Join(dir, "out.pdf")

	code, stdout, stderr := runCLI(t, src, "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "Page 3/3") {
		t.Fatalf("stdout missing page 3 line:\n%s", stdout)
	}
	count, sizes := pageSizes(t, out)
	if count != 3 {
		t.Fatalf("page count = %d, want 3", count)
	}
	for i, s := range sizes {
		if s != [2]float64{288, 432} {
			t.Fatalf("page %d size = %v, want 288x432", i, s)
		}
	}
}

// TestRunSplitSkipsPagesWithoutLabels ports the Python
// test_cli_split_skips_pages_without_labels, including flags placed after the
// positional argument.
func TestRunSplitSkipsPagesWithoutLabels(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{
		{Texts: []testpdf.TextSpec{{X: 30, Y: 100, Size: 12, S: "Instructions only"}}},
		{
			Rects: append(box(100, 30, 500, 380, 2), testpdf.Rect(130, 500, 480, 600)),
			Texts: []testpdf.TextSpec{{X: 30, Y: 440, Size: 12, S: "Place this barcode inside your package"}},
		},
	})
	out := filepath.Join(dir, "labels.pdf")

	code, stdout, stderr := runCLI(t, src, "--split", "-q", "-o", out)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("quiet run wrote to stdout: %q", stdout)
	}
	count, sizes := pageSizes(t, out)
	if count != 2 {
		t.Fatalf("page count = %d, want 2", count)
	}
	for i, s := range sizes {
		if s != [2]float64{288, 432} {
			t.Fatalf("page %d size = %v, want 288x432", i, s)
		}
	}
}

func TestRunSplitWithoutLabelsFails(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{
		Texts: []testpdf.TextSpec{{X: 30, Y: 100, Size: 12, S: "Instructions only"}},
	}})
	out := filepath.Join(dir, "labels.pdf")

	code, _, stderr := runCLI(t, src, "--split", "-q", "-o", out)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Error: no labels produced") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunDefaultOutputPath(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{
		Texts: []testpdf.TextSpec{{X: 100, Y: 300, Size: 12, S: "content"}},
	}})

	code, _, stderr := runCLI(t, src, "-q")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	want := filepath.Join(dir, "return_label.pdf")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("default output %s not created: %v", want, err)
	}
}

func TestRunVersion(t *testing.T) {
	code, stdout, _ := runCLI(t, "--version")
	if code != 0 || !strings.HasPrefix(stdout, "fourbysix ") {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
}

func TestRunRejectsBadPages(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{}, {}, {}})

	code, _, stderr := runCLI(t, src, "--pages", "0", "-o", filepath.Join(dir, "out.pdf"))
	if code != 1 || !strings.Contains(stderr, "--pages: '0' is outside 1-3") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestRunReportsMissingInput(t *testing.T) {
	code, _, stderr := runCLI(t, filepath.Join(t.TempDir(), "nope.pdf"))
	if code != 1 || !strings.Contains(stderr, "not found") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestRunRejectsBadLabel(t *testing.T) {
	dir := t.TempDir()
	src := writeFixture(t, dir, []testpdf.Page{{}})
	code, _, stderr := runCLI(t, src, "--label", "4x6x7", "-o", filepath.Join(dir, "out.pdf"))
	if code != 1 || !strings.Contains(stderr, "--label must be WxH") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}
