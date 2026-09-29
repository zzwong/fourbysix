# AGENTS.md

Conventions for working in this repository.

## What this is

`fourbysix` is a Go port of the Python `labelize` tool: it crops shipping and
return-label PDFs onto label-sized pages (default 4x6 in). The behavioral spec
is the Python source; when porting, match its output text and numbers exactly
unless a change is called out.

## Constraints

- `CGO_ENABLED=0` and a single static binary. PDF access goes only through
  `github.com/klippa-app/go-pdfium` in webassembly mode.
- No AGPL/GPL dependencies. Allowed: go-pdfium (MIT), wazero (Apache-2),
  `github.com/spf13/pflag` (BSD), `go.yaml.in/yaml/v3` (MIT/Apache),
  `golang.org/x/image` (BSD). Justify anything else.
- Never commit a real label PDF: they carry home addresses and tracking
  numbers. Tests build fixtures with `internal/testpdf`.
- Do not create a LICENSE file.

## Layout

- `cmd/fourbysix` — CLI. All logic lives in `run(args, stdout, stderr) int`
  so tests can drive it without spawning a process.
- `internal/layout` — pure geometry and page selection (no pdfium): `Rect`,
  `ContentBBox`, `ShouldRotate`, `FindLabelRegions`, `ParsePages`. Everything
  here is unit-testable without rendering.
- `internal/pdfdoc` — the only package that touches pdfium. Opens documents,
  walks page objects (recursing into form XObjects), and renders crops.
- `internal/place` — crop -> rotate -> scale -> threshold -> centered image.
  `ProcessImage` is pure and tested without a PDF.
- `internal/pdfwrite` — minimal PDF 1.4 writer, one image per page.
- `internal/testpdf` — test-only PDF builder. All coordinates are
  top-left-origin points; the writer flips to PDF space.

## Checks

Run `make check` before committing. It enforces `gofmt -l` empty, `go vet`,
`go test ./...`, a static build, and static linkage. `bin/` is gitignored.

## Style

- Comments explain why, not what. No dates and no changelog-style comments.
- Webassembly render buffers are only valid until `Cleanup()`; copy pixels
  before releasing the response.
- pdfium bounds are PDF space (bottom-left origin); convert to the
  top-left-origin `layout.Rect` at the `pdfdoc` boundary.
