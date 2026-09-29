package layout

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePages turns a page spec like "2-3,5" into zero-based page indexes in
// the order given: 1-based, ranges are inclusive on both ends, and out-of-range, reversed or non-numeric parts
// are errors. An empty spec means all pages in order.
func ParsePages(spec string, pageCount int) ([]int, error) {
	if spec == "" {
		indexes := make([]int, pageCount)
		for i := range indexes {
			indexes[i] = i
		}
		return indexes, nil
	}

	var indexes []int
	for _, raw := range strings.Split(spec, ",") {
		part := strings.TrimSpace(raw)
		var start, end int
		var err error
		if strings.Contains(part, "-") {
			pieces := strings.SplitN(part, "-", 2)
			start, err = strconv.Atoi(pieces[0])
			if err == nil {
				end, err = strconv.Atoi(pieces[1])
			}
		} else {
			start, err = strconv.Atoi(part)
			end = start
		}
		if err != nil {
			return nil, fmt.Errorf("--pages: '%s' is not a page or range", part)
		}
		if !(1 <= start && start <= end && end <= pageCount) {
			return nil, fmt.Errorf("--pages: '%s' is outside 1-%d", part, pageCount)
		}
		for i := start - 1; i < end; i++ {
			indexes = append(indexes, i)
		}
	}
	return indexes, nil
}
