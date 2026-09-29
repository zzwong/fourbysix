# fourbysix

Crop shipping and return-label PDFs onto thermal label pages (default 4x6 in)
with automatic content detection, rotation, and scaling. `--split` pulls
several labels off one page, such as the UPS label, lithium battery label and
return slip that Amazon's return center prints on letter paper, and drops the
instructions around them.

```sh
fourbysix input.pdf
fourbysix input.pdf --label 2x4 --output stickers.pdf
fourbysix labels.pdf --rotation best-fit --dpi 150
fourbysix "Online Return Center.pdf" --split --rotation best-fit --print
```

## Build

```sh
make build          # CGO_ENABLED=0 go build -trimpath -o bin/fourbysix ./cmd/fourbysix
make check          # gofmt, go vet, go test, static build, static-linkage check
```

The result is a single static binary. PDF access goes through
[go-pdfium](https://github.com/klippa-app/go-pdfium) in webassembly mode, so no
system libpdfium is required.

## How it works

1. **Detect content** — scans each page for non-white pixels (any channel
   below 250) to find the content bounding box.
2. **Crop whitespace** — trims everything outside the content plus a small
   margin.
3. **Decide rotation** — compares content and label aspect ratios.
4. **Scale & center** — scales the content to fit the label and centers it on
   the output page.

Whole-page mode emits one label per input page. Blank pages become blank
labels.

## Split mode (several labels per page)

Return centers print their labels onto letter pages: an instructions page, a
page with the shipping label, and a page with both a battery label and the
return slip, surrounded by headings and browser header/footer text. Whole-page
mode would shrink each page to about 50%.

`--split` reads the PDF object structure instead of the pixels. Images and
vector drawings are grouped into labels; graphics within 45 pt of each other
join the same label unless free-standing text (a heading or instruction line)
sits between them. Groups smaller than 1 inch on a side (banners, rules) are
dropped, and pages with no labels are skipped.

```sh
fourbysix "Online Return Center.pdf" --split --rotation best-fit --print
#   Page 1/3: no labels found, skipped
#   Page 2/3 label 1: 7.9x5.5 in ↻90° → scale 73% → 4.0x5.8 in   (shipping label)
#   Page 3/3 label 1: 5.5x5.1 in → scale 72% → 4.0x3.7 in        (battery label)
#   Page 3/3 label 2: 7.9x3.4 in ↻90° → scale 76% → 2.5x6.0 in   (return slip)
```

For Amazon returns use `--rotation best-fit`: the shipping label is embedded
sideways, and `auto` would keep it that way because it is landscape.

`--print` sends the result to the default printer with `lp`; pick another
queue with `--printer NAME`. `--pages 2-3` limits processing to those pages in
either mode.

## Configuration

Create a user config with `--init`:

```sh
fourbysix --init
# → creates $XDG_CONFIG_HOME/fourbysix/config.yaml
```

Edit that file for your defaults. `config.example.yaml` in this repository is
a reference template; it is never loaded automatically.

```yaml
label:
  width: 4       # inches
  height: 6      # inches
dpi: 300          # rendering resolution
margin: 8         # points of padding around content
rotation: auto    # auto | best-fit | none | cw90 | ccw90
output_suffix: _label
```

Load order (later overrides earlier):

1. Built-in defaults
2. `$XDG_CONFIG_HOME/fourbysix/config.yaml` (or `os.UserConfigDir()`)
3. `--config PATH`
4. CLI flags

Without a user config the tool runs on built-in defaults and prints a reminder
about `--init`.

### Rotation policies

| Policy     | Behavior |
|------------|----------|
| `auto`     | Rotates for better fit, but keeps landscape content upright (landscape usually has horizontal text that becomes unreadable if rotated) |
| `best-fit` | Rotates purely for aspect ratio — fills the label better but text may be sideways |
| `none`     | Never rotates |
| `cw90`     | Always rotates 90° clockwise |
| `ccw90`    | Always rotates 90° counter-clockwise |

## CLI

```
usage: fourbysix [--label LABEL] [--config CONFIG] [-o OUTPUT]
                 [--dpi DPI] [--margin MARGIN]
                 [--rotation {auto,none,cw90,ccw90,best-fit}] [--quiet]
                 [--threshold] [--init] [--pages PAGES] [--split]
                 [--print] [--printer PRINTER] [--version]
                 [input]
```

Flags may appear before or after the input path. `--pages` accepts 1-based
pages and ranges in the order given, e.g. `2-3,5`.

## Limitations

- **Content detection is pixel-based.** It looks for non-white pixels
  (RGB < 250,250,250). It works on white-background documents and fails on
  colored backgrounds, watermarks, or noisy scans.
- **Text orientation is not detected.** The `auto` policy assumes landscape
  content carries horizontal text. Use `--rotation best-fit` or `none` to
  override.
- **Split mode needs graphics.** A label drawn only as text, with no border
  box or image, is not found. Text outside a label's graphics is left out of
  the crop.
- **No upscaling cap.** Content smaller than the label is scaled up, which may
  blur. Raise `--dpi` if that matters.

## Printer tuning (thermal label printers)

Thermal printers have a fixed native DPI — typically 203 (Rollo, older Zebra)
or 300 (newer Zebra, Brother). Set `dpi` in the config to match the printer
exactly; a mismatch forces the print system to resample, which causes faded
spots, gaps, or blurry barcodes.

`--threshold` converts the output to pure 1-bit black and white, giving
thermal printers unambiguous per-dot instructions instead of a dithered
grayscale image.
