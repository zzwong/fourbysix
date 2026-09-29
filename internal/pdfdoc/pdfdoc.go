// Package pdfdoc wraps go-pdfium (webassembly mode) behind a small surface:
// opening documents from bytes, page counts and sizes, a flattened object
// walk, and page rendering. It is the only package allowed to touch pdfium.
package pdfdoc

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	"github.com/zzwong/fourbysix/internal/layout"
)

var (
	pool    pdfium.Pool
	poolErr error
	poolOne sync.Once
)

// initPool lazily compiles the pdfium wasm module once per process. Without a
// compilation cache every run spends ~2.6 s compiling; the cache made warm
// runs ~0.3 s end to end.
func initPool() error {
	poolOne.Do(func() {
		cacheDir, err := userCacheDir()
		if err != nil {
			poolErr = err
			return
		}
		cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(cacheDir, "fourbysix", "wazero"))
		if err != nil {
			poolErr = err
			return
		}
		// A custom RuntimeConfig MUST re-enable these core features or
		// compilation fails with "tag section not supported as feature
		// exception-handling is disabled".
		p, err := webassembly.Init(webassembly.Config{
			MinIdle:  1,
			MaxIdle:  1,
			MaxTotal: 1,
			RuntimeConfig: wazero.NewRuntimeConfig().
				WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
				WithCompilationCache(cache),
		})
		if err != nil {
			poolErr = err
			return
		}
		pool = p
	})
	return poolErr
}

func userCacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return dir, nil
}

// Doc is an open pdfium document and the instance that holds it.
type Doc struct {
	inst pdfium.Pdfium
	doc  references.FPDF_DOCUMENT
}

// Open reads data into pdfium and returns a Doc. The caller must Close it.
func Open(data []byte) (*Doc, error) {
	if err := initPool(); err != nil {
		return nil, err
	}
	inst, err := pool.GetInstance(30 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("pdfium instance: %w", err)
	}
	opened, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		inst.Close()
		return nil, fmt.Errorf("open document: %w", err)
	}
	return &Doc{inst: inst, doc: opened.Document}, nil
}

// Close releases the document and returns the instance to the pool.
func (d *Doc) Close() error {
	var first error
	if _, err := d.inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.doc}); err != nil {
		first = err
	}
	if err := d.inst.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

// PageCount returns the number of pages in the document.
func (d *Doc) PageCount() (int, error) {
	pc, err := d.inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.doc})
	if err != nil {
		return 0, err
	}
	return pc.PageCount, nil
}

// PageSize returns the page dimensions in points (PDF space, bottom-left
// origin).
func (d *Doc) PageSize(index int) (float64, float64, error) {
	sz, err := d.inst.FPDF_GetPageSizeByIndex(&requests.FPDF_GetPageSizeByIndex{
		Document: d.doc,
		Index:    index,
	})
	if err != nil {
		return 0, 0, err
	}
	return sz.Width, sz.Height, nil
}

// matrix is an affine transform, ordered like pdfium's FPDF_FS_MATRIX, with
// point transform (x, y) -> (A*x + C*y + E, B*x + D*y + F).
type matrix struct {
	A, B, C, D, E, F float64
}

var identityMatrix = matrix{A: 1, D: 1}

// compose returns parent applied after child: p' = parent(child(p)).
func compose(parent, child matrix) matrix {
	return matrix{
		A: parent.A*child.A + parent.C*child.B,
		B: parent.B*child.A + parent.D*child.B,
		C: parent.A*child.C + parent.C*child.D,
		D: parent.B*child.C + parent.D*child.D,
		E: parent.A*child.E + parent.C*child.F + parent.E,
		F: parent.B*child.E + parent.D*child.F + parent.F,
	}
}

func (m matrix) apply(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// bbox maps a rectangle's four corners through m and returns the bounding
// box.
func (m matrix) bbox(x0, y0, x1, y1 float64) (float64, float64, float64, float64) {
	// Start from the (x0,y0) corner and fold in the other three; the first
	// corner is the initial min and max so the result is exact.
	minX, minY := m.apply(x0, y0)
	maxX, maxY := minX, minY
	for _, p := range [3][2]float64{{x1, y0}, {x0, y1}, {x1, y1}} {
		x, y := m.apply(p[0], p[1])
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x)
		maxY = max(maxY, y)
	}
	return minX, minY, maxX, maxY
}

// objectMatrix reads an object's placement matrix, which for a form maps
// form space into its parent's space.
func objectMatrix(inst pdfium.Pdfium, obj references.FPDF_PAGEOBJECT) (matrix, error) {
	res, err := inst.FPDFPageObj_GetMatrix(&requests.FPDFPageObj_GetMatrix{PageObject: obj})
	if err != nil {
		return matrix{}, err
	}
	return matrix{
		A: float64(res.Matrix.A),
		B: float64(res.Matrix.B),
		C: float64(res.Matrix.C),
		D: float64(res.Matrix.D),
		E: float64(res.Matrix.E),
		F: float64(res.Matrix.F),
	}, nil
}

// Object is one flattened page object with its bounds in PDF space
// (bottom-left origin, points). Form children have already been mapped into
// page space with their placement matrix.
type Object struct {
	Type                     enums.FPDF_PAGEOBJ
	Left, Bottom, Right, Top float64
}

// walkObjects loads a page, walks every object including recursion into form
// XObjects, and calls fn once per leaf object with page-space bounds. The
// PageObject reference stays valid until the page is closed, so fn must not
// retain it.
func (d *Doc) walkObjects(page int, fn func(obj Object, ref references.FPDF_PAGEOBJECT) error) error {
	loaded, err := d.inst.FPDF_LoadPage(&requests.FPDF_LoadPage{Document: d.doc, Index: page})
	if err != nil {
		return fmt.Errorf("load page %d: %w", page, err)
	}
	pageRef := loaded.Page
	defer func() {
		// Failure to close would leak a page handle; report nothing so the
		// primary error stays visible.
		_, _ = d.inst.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: pageRef})
	}()

	countRes, err := d.inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{
		Page: requests.Page{ByReference: &pageRef},
	})
	if err != nil {
		return err
	}
	for i := 0; i < countRes.Count; i++ {
		objRes, err := d.inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{
			Page:  requests.Page{ByReference: &pageRef},
			Index: i,
		})
		if err != nil {
			return err
		}
		if err := d.walkFrom(objRes.PageObject, identityMatrix, fn); err != nil {
			return err
		}
	}
	return nil
}

// leafBounds returns a non-form object's bounds in its parent's PDF space.
// A stroked path uses its geometry instead of FPDFPageObj_GetBounds, which
// inflates the box by the stroke extent; PyMuPDF (and therefore the Python
// tool) measures the path itself, and the difference is enough to change the
// reported label size.
func (d *Doc) leafBounds(typ enums.FPDF_PAGEOBJ, obj references.FPDF_PAGEOBJECT) (float64, float64, float64, float64, error) {
	if typ == enums.FPDF_PAGEOBJ_PATH {
		if dm, err := d.inst.FPDFPath_GetDrawMode(&requests.FPDFPath_GetDrawMode{PageObject: obj}); err == nil && dm.Stroke {
			if x0, y0, x1, y1, err := d.pathGeometryBounds(obj); err == nil {
				return x0, y0, x1, y1, nil
			}
		}
	}
	b, err := d.inst.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: obj})
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return float64(b.Left), float64(b.Bottom), float64(b.Right), float64(b.Top), nil
}

// pathGeometryBounds maps every path segment endpoint (and Bezier control
// point, which PyMuPDF also counts) through the object's matrix and returns
// the bounding box in the parent's PDF space.
func (d *Doc) pathGeometryBounds(obj references.FPDF_PAGEOBJECT) (float64, float64, float64, float64, error) {
	m, err := d.inst.FPDFPageObj_GetMatrix(&requests.FPDFPageObj_GetMatrix{PageObject: obj})
	if err != nil {
		return 0, 0, 0, 0, err
	}
	count, err := d.inst.FPDFPath_CountSegments(&requests.FPDFPath_CountSegments{PageObject: obj})
	if err != nil {
		return 0, 0, 0, 0, err
	}

	var minX, minY, maxX, maxY float64
	first := true
	add := func(x, y float64) {
		tx := float64(m.Matrix.A)*x + float64(m.Matrix.C)*y + float64(m.Matrix.E)
		ty := float64(m.Matrix.B)*x + float64(m.Matrix.D)*y + float64(m.Matrix.F)
		if first {
			minX, minY, maxX, maxY = tx, ty, tx, ty
			first = false
			return
		}
		minX, minY = min(minX, tx), min(minY, ty)
		maxX, maxY = max(maxX, tx), max(maxY, ty)
	}

	for i := 0; i < count.Count; i++ {
		seg, err := d.inst.FPDFPath_GetPathSegment(&requests.FPDFPath_GetPathSegment{PageObject: obj, Index: i})
		if err != nil {
			return 0, 0, 0, 0, err
		}
		pt, err := d.inst.FPDFPathSegment_GetPoint(&requests.FPDFPathSegment_GetPoint{PathSegment: seg.PathSegment})
		if err != nil {
			return 0, 0, 0, 0, err
		}
		add(float64(pt.X), float64(pt.Y))

		typ, err := d.inst.FPDFPathSegment_GetType(&requests.FPDFPathSegment_GetType{PathSegment: seg.PathSegment})
		if err == nil && typ.Type == enums.FPDF_SEGMENT_BEZIERTO {
			cp, err := d.inst.FPDFPath_GetBezierControlPoints(&requests.FPDFPath_GetBezierControlPoints{
				PageObject: obj,
				Index:      uint64(i),
			})
			if err == nil {
				add(float64(cp.FirstControlPoint.X), float64(cp.FirstControlPoint.Y))
				add(float64(cp.SecondControlPoint.X), float64(cp.SecondControlPoint.Y))
			}
		}
	}
	if first {
		return 0, 0, 0, 0, errors.New("path has no segments")
	}
	return minX, minY, maxX, maxY, nil
}

// walkFrom reports obj, or every leaf inside it when obj is a form, with
// bounds mapped to page space through acc, the product of the placement
// matrices of the forms that enclose obj.
func (d *Doc) walkFrom(obj references.FPDF_PAGEOBJECT, acc matrix, fn func(Object, references.FPDF_PAGEOBJECT) error) error {
	typRes, err := d.inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj})
	if err != nil {
		return err
	}
	if typRes.Type != enums.FPDF_PAGEOBJ_FORM {
		left, bottom, right, top, err := d.leafBounds(typRes.Type, obj)
		if err != nil {
			return err
		}
		lx, by, rx, ty := acc.bbox(left, bottom, right, top)
		return fn(Object{Type: typRes.Type, Left: lx, Bottom: by, Right: rx, Top: ty}, obj)
	}

	placement, err := objectMatrix(d.inst, obj)
	if err != nil {
		return err
	}
	m := compose(acc, placement)
	countRes, err := d.inst.FPDFFormObj_CountObjects(&requests.FPDFFormObj_CountObjects{PageObject: obj})
	if err != nil {
		return err
	}
	for i := uint64(0); i < uint64(countRes.Count); i++ {
		child, err := d.inst.FPDFFormObj_GetObject(&requests.FPDFFormObj_GetObject{
			PageObject: obj,
			Index:      i,
		})
		if err != nil {
			return err
		}
		if err := d.walkFrom(child.PageObject, m, fn); err != nil {
			return err
		}
	}
	return nil
}

// Objects returns every flattened page object with PDF-space bounds.
func (d *Doc) Objects(page int) ([]Object, error) {
	var out []Object
	err := d.walkObjects(page, func(o Object, _ references.FPDF_PAGEOBJECT) error {
		out = append(out, o)
		return nil
	})
	return out, err
}

// toTopLeft converts PDF-space bounds to a top-left-origin layout.Rect.
func toTopLeft(pageH float64, o Object) layout.Rect {
	return layout.Rect{
		X0: o.Left,
		Y0: pageH - o.Top,
		X1: o.Right,
		Y1: pageH - o.Bottom,
	}
}

// whiteFillPath reports whether a path has no stroke, some fill, and a fill
// color with all channels above 242: such a path paints nothing visible and
// is skipped, exactly like the Python's get_drawings filter
// (min(fill) > 0.95 with no stroke color).
func (d *Doc) whiteFillPath(ref references.FPDF_PAGEOBJECT) (bool, error) {
	dm, err := d.inst.FPDFPath_GetDrawMode(&requests.FPDFPath_GetDrawMode{PageObject: ref})
	if err != nil {
		return false, err
	}
	if dm.Stroke || dm.FillMode == enums.FPDF_FILLMODE_NONE {
		return false, nil
	}
	fc, err := d.inst.FPDFPageObj_GetFillColor(&requests.FPDFPageObj_GetFillColor{PageObject: ref})
	if err != nil {
		return false, err
	}
	return fc.FillColor.R > 242 && fc.FillColor.G > 242 && fc.FillColor.B > 242, nil
}

// Regions splits a page's objects into graphic rects (images, shadings, and
// paths that paint something) and text rects, both in top-left-origin page
// space, ready for layout.FindLabelRegions.
func (d *Doc) Regions(page int) (graphics, text []layout.Rect, err error) {
	_, pageH, err := d.PageSize(page)
	if err != nil {
		return nil, nil, err
	}
	err = d.walkObjects(page, func(o Object, ref references.FPDF_PAGEOBJECT) error {
		switch o.Type {
		case enums.FPDF_PAGEOBJ_TEXT:
			text = append(text, toTopLeft(pageH, o))
		case enums.FPDF_PAGEOBJ_IMAGE, enums.FPDF_PAGEOBJ_SHADING:
			graphics = append(graphics, toTopLeft(pageH, o))
		case enums.FPDF_PAGEOBJ_PATH:
			skip, err := d.whiteFillPath(ref)
			if err != nil {
				return err
			}
			if !skip {
				graphics = append(graphics, toTopLeft(pageH, o))
			}
		}
		return nil
	})
	return graphics, text, err
}

// Render draws a page at the given DPI, optionally cropped to rect (top-left
// origin, points; nil means the full page). The returned image is detached
// from pdfium's wasm memory, so it outlives the render.
func (d *Doc) Render(page, dpi int, rect *layout.Rect) (image.Image, error) {
	req := &requests.RenderPageInDPI{
		DPI: dpi,
		Page: requests.Page{ByIndex: &requests.PageByIndex{
			Document: d.doc,
			Index:    page,
		}},
	}
	if rect != nil {
		if rect.Width() <= 0 || rect.Height() <= 0 {
			return nil, errors.New("render crop must have positive size")
		}
		req.Crop = &requests.RenderPageCrop{
			X:      rect.X0,
			Y:      rect.Y0,
			Width:  rect.Width(),
			Height: rect.Height(),
		}
	}
	res, err := d.inst.RenderPageInDPI(req)
	if err != nil {
		return nil, fmt.Errorf("render page %d: %w", page, err)
	}
	defer res.Cleanup()
	return copyImage(res.Result.RenderedImage), nil
}

// copyImage copies pixels out of pdfium's wasm heap, which is only valid
// until Cleanup.
func copyImage(src image.Image) image.Image {
	if src == nil {
		return nil
	}
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}
