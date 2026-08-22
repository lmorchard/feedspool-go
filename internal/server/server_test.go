package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveIndexPathStaysInsideServedDir(t *testing.T) {
	root := t.TempDir()
	s := &Server{config: &Config{Dir: root}}

	tests := []struct {
		name    string
		urlPath string
		want    string
	}{
		{"root", "/", filepath.Join(root, "index.html")},
		{"subdirectory", "/tech/", filepath.Join(root, "tech", "index.html")},
		{"nested subdirectory", "/a/b/", filepath.Join(root, "a", "b", "index.html")},
		{"traversal is collapsed", "/../", filepath.Join(root, "index.html")},
		{"deep traversal is collapsed", "/tech/../../../etc/", filepath.Join(root, "etc", "index.html")},
		{"dot segments", "/./tech/./", filepath.Join(root, "tech", "index.html")},
	}

	for _, tt := range tests {
		got := s.resolveIndexPath(tt.urlPath)
		if got != tt.want {
			t.Errorf("%s: resolveIndexPath(%q) = %q, want %q", tt.name, tt.urlPath, got, tt.want)
		}
		if got != "" && !strings.HasPrefix(got, root+string(os.PathSeparator)) {
			t.Errorf("%s: resolveIndexPath(%q) escaped the served directory: %q", tt.name, tt.urlPath, got)
		}
	}
}

func TestResolveIndexPathRelativeDir(t *testing.T) {
	// A relative Dir must still resolve to an absolute path inside itself.
	s := &Server{config: &Config{Dir: "./build"}}

	got := s.resolveIndexPath("/tech/")
	if got == "" {
		t.Fatal("resolveIndexPath() = empty, want a path under ./build")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolveIndexPath() = %q, want an absolute path", got)
	}

	root, err := filepath.Abs("./build")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, root+string(os.PathSeparator)) {
		t.Errorf("resolveIndexPath() = %q, want a path under %q", got, root)
	}
}
