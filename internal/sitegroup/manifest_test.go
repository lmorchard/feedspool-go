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

func TestPruneRejectsUnsafeSlug(t *testing.T) {
	out := t.TempDir()
	victim := filepath.Join(out, "victim")
	mkdirs(t, out, "victim")

	// Hand-write a manifest containing a traversal attempt.
	manifest := `{"version":1,"slugs":["../victim","tech/nested",".",""]}`
	if err := os.WriteFile(filepath.Join(out, ManifestName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

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
}
