package layout

import (
	"reflect"
	"testing"
)

func TestParsePages(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		pageCount int
		want      []int
	}{
		{"default is all pages", "", 3, []int{0, 1, 2}},
		{"ranges and singles in given order", "2-3,5", 5, []int{1, 2, 4}},
		{"repeated and reordered pages", "3,1", 3, []int{2, 0}},
		{"whitespace around parts", " 2 , 4 ", 5, []int{1, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePages(tt.spec, tt.pageCount)
			if err != nil {
				t.Fatalf("ParsePages(%q, %d) error: %v", tt.spec, tt.pageCount, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParsePages(%q, %d) = %v, want %v", tt.spec, tt.pageCount, got, tt.want)
			}
		})
	}
}

func TestParsePagesRejectsBadSpecs(t *testing.T) {
	for _, spec := range []string{"0", "4", "3-2", "a", "1-x"} {
		t.Run(spec, func(t *testing.T) {
			if _, err := ParsePages(spec, 3); err == nil {
				t.Fatalf("ParsePages(%q, 3) succeeded, want error", spec)
			}
		})
	}
}

func TestParsePagesErrorMessages(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{"4", "--pages: '4' is outside 1-3"},
		{"3-2", "--pages: '3-2' is outside 1-3"},
		{"a", "--pages: 'a' is not a page or range"},
		{"1-x", "--pages: '1-x' is not a page or range"},
	}
	for _, tt := range tests {
		_, err := ParsePages(tt.spec, 3)
		if err == nil {
			t.Fatalf("ParsePages(%q, 3) succeeded, want error", tt.spec)
		}
		if got := err.Error(); got != tt.want {
			t.Fatalf("ParsePages(%q, 3) error = %q, want %q", tt.spec, got, tt.want)
		}
	}
}
