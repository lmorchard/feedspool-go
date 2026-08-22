package renderer

import "testing"

const (
	testPluralizeSingular = "feed"
	testPluralizePlural   = "feeds"
)

func TestPluralize(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		singular string
		plural   string
		want     string
	}{
		{
			name: "zero is plural", n: 0,
			singular: testPluralizeSingular, plural: testPluralizePlural, want: testPluralizePlural,
		},
		{
			name: "one is singular", n: 1,
			singular: testPluralizeSingular, plural: testPluralizePlural, want: testPluralizeSingular,
		},
		{
			name: "two is plural", n: 2,
			singular: testPluralizeSingular, plural: testPluralizePlural, want: testPluralizePlural,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pluralize(tt.n, tt.singular, tt.plural); got != tt.want {
				t.Errorf("pluralize(%d, %q, %q) = %q, want %q", tt.n, tt.singular, tt.plural, got, tt.want)
			}
		})
	}
}
