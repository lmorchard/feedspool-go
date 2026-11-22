package topics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLabelPolicyGeneralizesMixedLanguage(t *testing.T) {
	policy := DefaultLabelPolicy()
	got, changed := policy.Apply(
		testRustLabel,
		[]string{testRustDeclarative, testGoService, testGeneralSoftware},
		testGenericLabel,
	)
	if !changed || got != testGenericLabel {
		t.Fatalf("Apply() = %q, %v; want %q, true", got, changed, testGenericLabel)
	}
}

func TestLabelPolicyFileAddsCategory(t *testing.T) {
	path := filepath.Join(t.TempDir(), testPolicyFile)
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
	if !changed || got != testAI {
		t.Fatalf("Apply() = %q, %v; want %q, true", got, changed, testAI)
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
	if got, changed := policy.Apply(testGoLabel, []string{testGoOne, testGoTwo, testRust}, testSoftware); changed || got != testGoLabel {
		t.Fatalf("supported majority Apply() = %q, %v; want unchanged", got, changed)
	}
	if got, changed := policy.Apply(testGoLabel, []string{testGoOne, testRust, testPython}, testSoftware); !changed || got != testProgramming {
		t.Fatalf("unsupported majority Apply() = %q, %v; want %q, true", got, changed, testProgramming)
	}
}

func TestLabelPolicyEvaluateReportsMatch(t *testing.T) {
	match := DefaultLabelPolicy().Evaluate(
		testRustLabel,
		[]string{testRustDeclarative, testGoService, testGeneralSoftware},
		testGenericLabel,
	)
	if !match.Changed || match.Rule != testRuleLanguage || match.Term != testTermRust ||
		match.Support != 1 || match.Total != 3 || match.Label != testGenericLabel {
		t.Fatalf("Evaluate() = %+v; want language/rust support 1/3 changed", match)
	}
}

func TestLabelPolicyEvaluateKeepsSupportedLabel(t *testing.T) {
	match := DefaultLabelPolicy().Evaluate(
		testRustLabel,
		[]string{testRustMacros, testRustDatetime, testLearningRust},
		testGenericLabel,
	)
	if match.Changed || match.Rule != testRuleLanguage || match.Term != testTermRust ||
		match.Support != 3 || match.Total != 3 || match.Label != testRustLabel {
		t.Fatalf("Evaluate() = %+v; want supported language/rust 3/3 unchanged", match)
	}
}

func TestLabelPolicyEvaluateNoMatch(t *testing.T) {
	match := DefaultLabelPolicy().Evaluate(
		testModernSoftware,
		[]string{testRustMacros, testGoService},
		testGenericLabel,
	)
	if match.Changed || match.Rule != "" || match.Support != 0 || match.Total != 2 {
		t.Fatalf("Evaluate() = %+v; want no match", match)
	}
}

func TestLabelPolicyRejectsUnknownAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), testPolicyFile)
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
