// Command fourbysix converts PDF pages to label-sized pages with automatic
// content detection, cropping, rotation and scaling. See README.md.
package main

import (
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/zzwong/fourbysix/internal/config"
	"github.com/zzwong/fourbysix/internal/layout"
	"github.com/zzwong/fourbysix/internal/pdfdoc"
	"github.com/zzwong/fourbysix/internal/pdfwrite"
	"github.com/zzwong/fourbysix/internal/place"
)

var version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `usage: fourbysix [--label LABEL] [--config CONFIG] [-o OUTPUT]
                   [--dpi DPI] [--margin MARGIN]
                   [--rotation {auto,none,cw90,ccw90,best-fit}] [--quiet]
                   [--init] [--pages PAGES] [--split] [--print]
                   [--printer PRINTER] [--version]
                   [input]

Convert PDF pages to label size with auto-crop and rotation.

Example: fourbysix invoice.pdf --label 4x6 --rotation auto

positional arguments:
  input                 Input PDF file path

optional arguments:
  --label LABEL         Label size as WxH in inches (e.g., 4x6, 2.25x1.25)
  --config CONFIG       Path to YAML config file
  -o, --output OUTPUT   Output PDF path (default: <input>_label.pdf)
  --dpi DPI             Render DPI (default: 300)
  --margin MARGIN       Content padding in points (default: 8)
  --rotation {...}      Rotation policy (default: auto)
  --quiet, -q           Suppress per-page output
  --threshold           Convert to pure 1-bit black/white (no antialiasing)
  --init                Create the user config from the example and exit
  --pages PAGES         Pages to process, 1-based (e.g., 2-3 or 1,3,5-7)
  --split               One label per image or bordered box on each page,
                        dropping instruction text; skips pages with no labels
  --print               Send the output to the printer with lp
  --printer PRINTER     lp destination for --print (default: system default)
  --version             Print the version and exit
`

// run is the whole CLI, factored so tests can drive it with injected
// streams. It returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("fourbysix", pflag.ContinueOnError)
	fs.SetOutput(io.Discard) // pflag noise is replaced by the usage text

	var (
		input     string
		label     string
		cfgPath   string
		output    string
		dpi       int
		margin    int
		rotation  string
		quiet     bool
		threshold bool
		init      bool
		pages     string
		split     bool
		printIt   bool
		printer   string
		showVer   bool
	)
	fs.StringVar(&label, "label", "", "Label size as WxH in inches (e.g., 4x6, 2.25x1.25)")
	fs.StringVar(&cfgPath, "config", "", "Path to YAML config file")
	fs.StringVarP(&output, "output", "o", "", "Output PDF path (default: <input>_label.pdf)")
	fs.IntVar(&dpi, "dpi", 0, "Render DPI")
	fs.IntVar(&margin, "margin", 0, "Content padding in points")
	fs.StringVar(&rotation, "rotation", "", "Rotation policy")
	fs.BoolVarP(&quiet, "quiet", "q", false, "Suppress per-page output")
	fs.BoolVar(&threshold, "threshold", false, "Convert to pure 1-bit black/white")
	fs.BoolVar(&init, "init", false, "Create the user config from the example and exit")
	fs.StringVar(&pages, "pages", "", "Pages to process, 1-based (e.g., 2-3 or 1,3,5-7)")
	fs.BoolVar(&split, "split", false, "One label per image or bordered box on each page")
	fs.BoolVar(&printIt, "print", false, "Send the output to the printer with lp")
	fs.StringVar(&printer, "printer", "", "lp destination for --print")
	fs.BoolVar(&showVer, "version", false, "Print the version and exit")

	fs.ParseErrorsWhitelist.UnknownFlags = false
	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, usageText)
		return 1
	}
	if len(fs.Args()) > 1 {
		fmt.Fprint(stderr, usageText)
		return 1
	}
	input = fs.Arg(0)

	if showVer {
		fmt.Fprintf(stdout, "fourbysix %s\n", version)
		return 0
	}

	userPath := config.UserConfigPath()

	// --init mode: copy the example config, refusing to overwrite.
	if init {
		if err := config.Init(userPath); err == config.ErrExists {
			fmt.Fprintf(stderr, "Config already exists at %s\n", userPath)
			fmt.Fprintln(stderr, "Remove it first or edit it directly.")
			return 1
		} else if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Created %s\n", userPath)
		fmt.Fprintln(stdout, "Edit this file to change defaults (label size, DPI, rotation, etc.).")
		return 0
	}

	// Require an input file like argparse: usage to stdout, exit 1.
	if input == "" {
		fmt.Fprint(stdout, usageText)
		return 1
	}

	// Load order: defaults -> user config -> --config -> CLI flags.
	cfg := config.Defaults()
	if _, err := os.Stat(userPath); err == nil {
		cfg, err = config.Load(userPath, cfg)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	} else if !quiet {
		fmt.Fprintf(stderr, "Note: No config at %s. Using built-in defaults.\n", userPath)
		fmt.Fprintf(stderr, "      Run 'fourbysix --init' to create one.\n\n")
	}

	if cfgPath != "" {
		var err error
		cfg, err = config.Load(cfgPath, cfg)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}

	// CLI flags override everything.
	if fs.Changed("label") {
		w, h, ok := parseLabel(label)
		if !ok {
			fmt.Fprintln(stderr, "Error: --label must be WxH (e.g., 4x6, 2.25x1.25)")
			return 1
		}
		cfg.LabelW, cfg.LabelH = w, h
	}
	if fs.Changed("dpi") {
		if dpi <= 0 {
			fmt.Fprintln(stderr, "Error: --dpi must be positive")
			return 1
		}
		cfg.DPI = dpi
	}
	if fs.Changed("margin") {
		cfg.Margin = float64(margin)
	}
	if fs.Changed("rotation") {
		if !validRotation(rotation) {
			fmt.Fprintf(stderr, "Error: --rotation must be one of auto, none, cw90, ccw90, best-fit\n")
			return 1
		}
		cfg.Rotation = rotation
	}
	if fs.Changed("threshold") {
		cfg.Threshold = threshold
	}

	// Output path: explicit, or <input dir>/<stem><suffix>.pdf.
	inputPath := input
	if _, err := os.Stat(inputPath); err != nil {
		fmt.Fprintf(stderr, "Error: '%s' not found\n", inputPath)
		return 1
	}
	outputPath := output
	if outputPath == "" {
		base := filepath.Base(inputPath)
		dir := filepath.Dir(inputPath)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		outputPath = filepath.Join(dir, stem+cfg.OutputSuffix+".pdf")
	}

	if !quiet {
		fmt.Fprintf(stdout, "fourbysix  %s  →  %s\n", inputPath, outputPath)
		fmt.Fprintf(stdout, "  Label:   %g×%g in\n", cfg.LabelW, cfg.LabelH)
		fmt.Fprintf(stdout, "  DPI:     %d\n", cfg.DPI)
		fmt.Fprintf(stdout, "  Rotate:  %s\n", cfg.Rotation)
		fmt.Fprintf(stdout, "  Margin:  %g pt\n", cfg.Margin)
		if cfg.Threshold {
			fmt.Fprintln(stdout, "  Bitdepth: 1-bit (thresholded)")
		}
		fmt.Fprintln(stdout)
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	doc, err := pdfdoc.Open(data)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer doc.Close()

	pageCount, err := doc.PageCount()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	pageNumbers, err := layout.ParsePages(pages, pageCount)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	placeCfg := place.Config{
		LabelW:    cfg.LabelW,
		LabelH:    cfg.LabelH,
		DPI:       cfg.DPI,
		Margin:    cfg.Margin,
		Rotation:  cfg.Rotation,
		Threshold: cfg.Threshold,
	}

	var labels []pdfwrite.Page
	report := func(tag string, res *place.Result) {
		if quiet {
			return
		}
		if res.Image == nil {
			fmt.Fprintf(stdout, "  %s: blank → empty label\n", tag)
			return
		}
		rotTag := ""
		if res.Rotated {
			rotTag = " ↻90°"
		}
		fmt.Fprintf(stdout, "  %s: %s in%s → scale %.0f%% → %s in\n",
			tag, res.Content, rotTag, res.Scale*100, res.Result)
	}

	for _, i := range pageNumbers {
		pageW, pageH, err := doc.PageSize(i)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		pageRect := layout.Rect{X0: 0, Y0: 0, X1: pageW, Y1: pageH}
		tag := fmt.Sprintf("Page %d/%d", i+1, pageCount)

		if !split {
			low, err := doc.Render(i, 72, nil)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			bbox, ok := layout.ContentBBox(low, 250)
			if !ok {
				// Blank page -> blank label.
				report(tag, &place.Result{})
				labels = append(labels, pdfwrite.Page{})
				continue
			}
			res, err := place.Region(doc, i, bbox, placeCfg)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			report(tag, res)
			labels = append(labels, toPDFPage(res))
			continue
		}

		graphics, text, err := doc.Regions(i)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		regions := layout.FindLabelRegions(graphics, text, pageRect)
		if len(regions) == 0 && !quiet {
			fmt.Fprintf(stdout, "  %s: no labels found, skipped\n", tag)
		}
		for n, region := range regions {
			res, err := place.Region(doc, i, region, placeCfg)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			report(fmt.Sprintf("%s label %d", tag, n+1), res)
			labels = append(labels, toPDFPage(res))
		}
	}

	if len(labels) == 0 {
		fmt.Fprintln(stderr, "Error: no labels produced")
		return 1
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	err = pdfwrite.Write(outFile, cfg.LabelW*72, cfg.LabelH*72, labels)
	closeErr := outFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if !quiet {
		fmt.Fprintf(stdout, "\nSaved: %s\n", outputPath)
	}

	if printIt {
		cmdArgs := []string{}
		if printer != "" {
			cmdArgs = append(cmdArgs, "-d", printer)
		}
		cmdArgs = append(cmdArgs, outputPath)
		cmd := exec.Command("lp", cmdArgs...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			fmt.Fprintf(stderr, "Error: lp: %v\n", err)
			return 1
		}
	}
	return 0
}

// toPDFPage converts a placement result into the writer's page struct.
func toPDFPage(res *place.Result) pdfwrite.Page {
	return pdfwrite.Page{
		Image: res.Image,
		Gray:  isGray(res.Image),
		X:     res.X,
		Y:     res.Y,
		W:     res.W,
		H:     res.H,
	}
}

// isGray reports whether the image is the 1-channel thresholded output so the
// writer can switch to /DeviceGray.
func isGray(img image.Image) bool {
	_, ok := img.(*image.Gray)
	return ok
}

func parseLabel(s string) (w, h float64, ok bool) {
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, errW := parseFloat(parts[0])
	h, errH := parseFloat(parts[1])
	if errW != nil || errH != nil {
		return 0, 0, false
	}
	return w, h, true
}

func parseFloat(s string) (float64, error) {
	var f float64
	var err error
	if f, err = strconv.ParseFloat(s, 64); err != nil {
		return 0, err
	}
	return f, nil
}

func validRotation(s string) bool {
	switch s {
	case "auto", "none", "cw90", "ccw90", "best-fit":
		return true
	}
	return false
}
