package testpdf

import "fmt"

// InheritedPageGeometry builds a fixed, synthetic page tree whose root and
// nearest ancestor disagree about every inherited geometry attribute. Unlike
// Page's drawing API, the literal dictionaries here are raw PDF coordinates so
// the inheritance and negative media origin are explicit in the fixture.
func InheritedPageGeometry() ([]byte, error) {
	b := &builder{}
	catalog := b.add(nil)
	root := b.add(nil)
	parent := b.add(nil)
	content := b.streamObj("140 500 200 150 re f\n")
	var pages []int
	for _, attrs := range []string{
		"",
		" /Rotate 0",
		" /MediaBox [-50 -20 550 780] /CropBox [-80 -40 500 700] /Rotate 180",
	} {
		pages = append(pages, b.add([]byte(fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R%s /Resources << >> /Contents %d 0 R >>",
			parent, attrs, content))))
	}
	b.set(catalog, []byte(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", root)))
	b.set(root, []byte(fmt.Sprintf(
		"<< /Type /Pages /Kids [%d 0 R] /Count 3 /MediaBox [0 0 300 600] /CropBox [90 190 290 590] /Rotate 270 >>", parent)))
	b.set(parent, []byte(fmt.Sprintf(
		"<< /Type /Pages /Parent %d 0 R /Kids [%d 0 R %d 0 R %d 0 R] /Count 3 /MediaBox [0 0 600 800] /CropBox [100 200 500 700] /Rotate 90 >>",
		root, pages[0], pages[1], pages[2])))
	return b.serialize()
}
