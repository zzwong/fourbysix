# fourbysix

[![check](https://github.com/zzwong/fourbysix/actions/workflows/check.yml/badge.svg)](https://github.com/zzwong/fourbysix/actions/workflows/check.yml)
[![release](https://img.shields.io/github/v/release/zzwong/fourbysix)](https://github.com/zzwong/fourbysix/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/zzwong/fourbysix.svg)](https://pkg.go.dev/github.com/zzwong/fourbysix)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

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

## Install

Download a binary for macOS, Linux or Windows from the
[latest release](https://github.com/zzwong/fourbysix/releases/latest), or
build it with Go 1.26 or newer:

```sh
go install github.com/zzwong/fourbysix/cmd/fourbysix@latest
```

This puts `fourbysix` in `$(go env GOPATH)/bin`. Runs on macOS, Linux and
Windows; `--print` needs CUPS `lp`, so it works on macOS and Linux only.

## Build

```sh
make build          # CGO_ENABLED=0 go build -trimpath -o bin/fourbysix ./cmd/fourbysix
make check          # gofmt, go vet, go test, static build, static-linkage check
make lint           # golangci-lint (install separately; CI runs it too)
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

## eBay return labels

eBay's printable return label is already one label per page: the FedEx or
USPS label on page 1, the packing slip on page 2. Use whole-page mode, which
is the default:

```sh
fourbysix "ebay.com_rt_ReturnDetails_returnId=….pdf" --print
#   Page 1/2: 4.1x5.8 in → scale 99% → 4.0x5.7 in    (shipping label)
#   Page 2/2: 6.1x2.9 in → scale 66% → 4.0x1.9 in    (packing slip)
```

Don't use `--split` here. The carrier label is drawn as text between ruled
lines, which split mode cannot see as one label; it keeps only the barcode
and skips the text-only packing slip. When split mode keeps under a third of
a page's content, it prints a hint to rerun without `--split`.

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
# → creates the user config (path below)
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
2. The user config, the first of:
   - `$XDG_CONFIG_HOME/fourbysix/config.yaml`, when `XDG_CONFIG_HOME` is set
     to an absolute path;
   - `~/.config/fourbysix/config.yaml`, when that file exists;
   - the platform config dir: `~/Library/Application Support/fourbysix/`
     on macOS, `~/.config/fourbysix/` on Linux, `%AppData%\fourbysix\` on
     Windows.
3. `--config PATH`
4. CLI flags

The effective label width and height must be positive and finite, including
after conversion from inches to PDF points in PDFium's float32 numeric range.
Dimensions that become zero or infinity in that conversion are rejected
before the output file is created or overwritten. This prevents PDFium from
silently substituting its default page size.

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


### Saving safely

Output is serialized into a unique temporary file in the resolved destination
parent, closed, then renamed over the output. Ordinary reported write, permission,
close or rename failures leave the previous destination bytes (or its absence)
unchanged; this attempt's temporary file is removed. Cleanup errors are reported.
Parent symlinks and `..` retain filesystem resolution order.

Final-component symlinks, including dangling links, nonregular destinations, and
files with setuid/setgid/sticky bits are rejected. Regular-file permission bits
are retained; new outputs use private 0600 permissions. Replacement creates a new inode: other hard links keep the old bytes,
including an input aliased through another hard link. Using the same input/output
pathname is supported because the input is read before saving. Ownership, ACLs,
extended attributes and other inode metadata are not preserved. Replacement is
controlled by parent-directory permissions, so even a read-only regular output
can be replaced when the directory permits it.

On Unix, same-directory rename provides atomic pathname replacement. Non-Unix
platforms do not have that atomicity guarantee and have not been qualified here.
There is no fsync or power-loss/crash durability guarantee. Concurrent filesystem
changes, especially adversarial directory or link swaps, are outside this local
CLI's guarantee. Successful-save output and printing occur only after rename.

## License

[MIT](LICENSE)
