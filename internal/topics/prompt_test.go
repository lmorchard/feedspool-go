package topics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPromptReplacesItems(t *testing.T) {
	got := RenderPrompt("Label the topic.\n\n{{items}}", []string{testRustMacros, testGoService})
	want := "Label the topic.\n\nItem 1:\n" + testRustMacros + "\n\nItem 2:\n" + testGoService + "\n\n"
	if got != want {
		t.Fatalf("RenderPrompt() = %q, want %q", got, want)
	}
}

func TestRenderPromptAppendsItemsWithoutPlaceholder(t *testing.T) {
	got := RenderPrompt("Label the topic.", []string{"one"})
	if !strings.Contains(got, "Item 1:\none") {
		t.Fatalf("RenderPrompt() = %q, want items appended", got)
	}
}

func TestLoadPromptUsesEmbeddedDefault(t *testing.T) {
	got, err := LoadPrompt("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "{{items}}") {
		t.Fatalf("default prompt missing items placeholder: %q", got)
	}
}

func TestLoadPromptReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "topics.md")
	if err := os.WriteFile(path, []byte("custom prompt\n\n{{items}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPrompt(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "custom prompt\n\n{{items}}\n" {
		t.Fatalf("LoadPrompt() = %q", got)
	}
}

func TestLoadPromptRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrompt(path); err == nil {
		t.Fatal("LoadPrompt() error = nil, want an empty-file error")
	}
}
