package sitegroup

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	comicsSlug = "comics"
	techSlug   = "tech"
)

// mkdirs creates the given subdirectories under root.
func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

// writeRawManifest writes content verbatim as the manifest file in dir,
// bypassing WriteManifest so tests can construct hand-edited or corrupt
// manifests.
func writeRawManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	out := t.TempDir()
	sites := []Site{{Slug: comicsSlug}, {Slug: techSlug}}

	if err := WriteManifest(out, sites); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if len(m.Slugs) != 2 || m.Slugs[0] != comicsSlug || m.Slugs[1] != techSlug {
		t.Errorf("Slugs = %v, want [comics tech]", m.Slugs)
	}
	if m.Version != manifestVersion {
		t.Errorf("Version = %d, want %d", m.Version, manifestVersion)
	}
}

func TestReadManifestMissing(t *testing.T) {
	m, err := ReadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil for a missing manifest", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty", m.Slugs)
	}
}

func TestReadManifestUnknownVersion(t *testing.T) {
	out := t.TempDir()
	writeRawManifest(t, out, `{"version":99,"slugs":["comics"]}`)

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil for an unrecognized version", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty for an unrecognized version", m.Slugs)
	}
}

func TestReadManifestNoVersion(t *testing.T) {
	out := t.TempDir()
	writeRawManifest(t, out, `{"slugs":["comics"]}`)

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil for a version-less manifest", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty for a version-less manifest", m.Slugs)
	}
}

func TestReadManifestTruncated(t *testing.T) {
	out := t.TempDir()
	writeRawManifest(t, out, `{"version":1,"slugs":["a","b"`)

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil for a truncated manifest", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty for a truncated manifest", m.Slugs)
	}
}

func TestReadManifestEmptyFile(t *testing.T) {
	out := t.TempDir()
	writeRawManifest(t, out, "")

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil for a zero-byte manifest", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty for a zero-byte manifest", m.Slugs)
	}
}

// TestReadManifestUnreadablePath guards against a bug where a manifest path
// that exists but cannot be read as a regular file (e.g. it is a directory)
// was treated as fatal, wedging every future render until someone manually
// removed it. Per the doc comment above ReadManifest, any problem reading or
// interpreting the manifest must degrade to "nothing to prune".
func TestReadManifestUnreadablePath(t *testing.T) {
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, ManifestName), 0o755); err != nil {
		t.Fatal(err)
	}

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v, want nil when the manifest path is a directory", err)
	}
	if len(m.Slugs) != 0 {
		t.Errorf("Slugs = %v, want empty when the manifest path is a directory", m.Slugs)
	}
}

func TestPruneRemovesDepartedSlug(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, comicsSlug, techSlug)
	if err := WriteManifest(out, []Site{{Slug: comicsSlug}, {Slug: techSlug}}); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(out, []Site{{Slug: techSlug}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != comicsSlug {
		t.Fatalf("removed = %v, want [comics]", removed)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); !os.IsNotExist(err) {
		t.Error("comics directory still exists after prune")
	}
	if _, err := os.Stat(filepath.Join(out, techSlug)); err != nil {
		t.Errorf("tech directory was removed but is still discovered: %v", err)
	}
}

func TestPruneIgnoresUnknownDirectories(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, techSlug, "not-ours")
	if err := WriteManifest(out, []Site{{Slug: techSlug}}); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(out, []Site{{Slug: techSlug}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "not-ours")); err != nil {
		t.Error("Prune removed a directory that was never in the manifest")
	}
}

func TestPruneWithoutManifestIsNoOp(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, "leftover")

	removed, err := Prune(out, []Site{{Slug: techSlug}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty", removed)
	}
	if _, err := os.Stat(filepath.Join(out, "leftover")); err != nil {
		t.Error("Prune removed a directory with no manifest present")
	}
}

// TestPruneRejectsUnsafeSlug is the load-bearing safety test for isSafeSlug.
// The traversal and nested-path targets are created as real, existing
// directories at the locations those slugs would resolve to if the guard
// were removed: "../victim" resolves (relative to out) to a sibling of out
// named "victim", and "tech/nested" resolves to a real subdirectory of out.
// If isSafeSlug's `slug == filepath.Base(slug)` clause is ever deleted, this
// test must fail, because both targets would then actually exist and be
// removed by RemoveAll.
func TestPruneRejectsUnsafeSlug(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	victim := filepath.Join(root, "victim")
	mkdirs(t, root, "out", "victim")
	mkdirs(t, out, "tech/nested")

	// Hand-write a manifest containing a traversal attempt.
	manifest := `{"version":1,"slugs":["../victim","tech/nested",".",""]}`
	writeRawManifest(t, out, manifest)

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty for unsafe slugs", removed)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Error("Prune followed a path traversal out of the output directory")
	}
	if _, err := os.Stat(filepath.Join(out, "tech", "nested")); err != nil {
		t.Error("Prune followed a nested-path slug into a subdirectory")
	}
}

// TestPruneSkipsSymlink verifies Prune uses Lstat rather than Stat, so a
// symlink at a manifest-listed path is never followed into RemoveAll even
// though its target is a real directory outside the output root.
func TestPruneSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	outsideTarget := filepath.Join(root, "outside-target")
	mkdirs(t, root, "outside-target")

	out := filepath.Join(root, "out")
	if err := WriteManifest(out, []Site{{Slug: comicsSlug}}); err != nil {
		t.Fatal(err)
	}

	symlinkPath := filepath.Join(out, comicsSlug)
	if err := os.Symlink(outsideTarget, symlinkPath); err != nil {
		t.Fatal(err)
	}

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty; Prune must not follow a symlink", removed)
	}
	if _, err := os.Lstat(symlinkPath); err != nil {
		t.Error("Prune removed the symlink itself")
	}
	if _, err := os.Stat(outsideTarget); err != nil {
		t.Error("Prune removed the symlink's target directory")
	}
}

func TestPruneRejectsUnknownVersion(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, comicsSlug)
	writeRawManifest(t, out, `{"version":99,"slugs":["comics"]}`)

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty for an unsupported manifest version", removed)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); err != nil {
		t.Error("Prune removed a directory listed in a manifest with an unsupported version")
	}
}

func TestPruneRejectsVersionlessManifest(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, comicsSlug)
	writeRawManifest(t, out, `{"slugs":["comics"]}`)

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty for a version-less manifest", removed)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); err != nil {
		t.Error("Prune removed a directory listed in a version-less manifest")
	}
}

func TestPruneWithCorruptManifestIsNoOp(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, comicsSlug)
	writeRawManifest(t, out, `{"version":1,"slugs":["comics"`)

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty for a corrupt manifest", removed)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); err != nil {
		t.Error("Prune removed a directory based on a corrupt manifest")
	}
}

func TestPruneWithEmptyManifestFileIsNoOp(t *testing.T) {
	out := t.TempDir()
	mkdirs(t, out, comicsSlug)
	writeRawManifest(t, out, "")

	removed, err := Prune(out, nil)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want empty for a zero-byte manifest", removed)
	}
	if _, err := os.Stat(filepath.Join(out, comicsSlug)); err != nil {
		t.Error("Prune removed a directory based on a zero-byte manifest")
	}
}

func TestIsSafeSlug(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{"", false},
		{".", false},
		{"..", false},
		{"...", true},
		{"/", false},
		{"//", false},
		{"/etc", false},
		{"../x", false},
		{"a/../../x", false},
		{"./x", false},
		{"foo/", false},
		{"a/b", false},
		{comicsSlug, true},
		{"..foo", true},
		{"foo..", true},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := isSafeSlug(tt.slug); got != tt.want {
				t.Errorf("isSafeSlug(%q) = %v, want %v", tt.slug, got, tt.want)
			}
		})
	}
}
