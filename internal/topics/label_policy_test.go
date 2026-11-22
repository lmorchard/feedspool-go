package topics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLabelPolicyGeneralizesMixedLanguage(t *testing.T) {
	policy := DefaultLabelPolicy()
	got, changed := policy.Apply(
		"Rust Programming",
		[]string{"Rust declarative macros", "A Go service", "How to make almost anything"},
		"Software Development",
	)
	if !changed || got != "Software Development" {
		t.Fatalf("Apply() = %q, %v; want Software Development, true", got, changed)
	}
}

func TestLabelPolicyFileAddsCategory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	data := []byte(`rules:
  - name: vendor
    terms: ["Anthropic", "OpenAI"]
    match: word
    require: all
    action: generalize
    replacement: AI Industry
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadLabelPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	got, changed := policy.Apply("Anthropic IPO", []string{"Anthropic raises", "OpenAI ships"}, "")
	if !changed || got != "AI Industry" {
		t.Fatalf("Apply() = %q, %v; want AI Industry, true", got, changed)
	}
}

func TestLabelPolicyMajoritySupport(t *testing.T) {
	policy, err := parseLabelPolicy([]byte(`rules:
  - name: language
    terms: [go]
    require: majority
    action: generalize
    replacement: Programming
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, changed := policy.Apply("Go service", []string{"Go one", "Go two", "Rust"}, "Software"); changed || got != "Go service" {
		t.Fatalf("supported majority Apply() = %q, %v; want unchanged", got, changed)
	}
	if got, changed := policy.Apply("Go service", []string{"Go one", "Rust", "Python"}, "Software"); !changed || got != "Programming" {
		t.Fatalf("unsupported majority Apply() = %q, %v; want Programming, true", got, changed)
	}
}

func TestLabelPolicyRejectsUnknownAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(`rules:
  - name: bad
    terms: [go]
    action: explode
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLabelPolicy(path); err == nil {
		t.Fatal("LoadLabelPolicy() error = nil, want unknown-action error")
	}
}
