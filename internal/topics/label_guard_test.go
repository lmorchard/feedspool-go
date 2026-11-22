package topics

import "testing"

func TestGuardLabelGeneralizesMixedLanguage(t *testing.T) {
	got, changed := GuardLabel(
		"Rust Programming",
		[]string{"Rust declarative macros", "A Go service", "How to make almost anything"},
		"Software Development",
	)
	if !changed || got != "Software Development" {
		t.Fatalf("GuardLabel() = %q, %v; want Software Development, true", got, changed)
	}
}

func TestGuardLabelKeepsUnanimousLanguage(t *testing.T) {
	got, changed := GuardLabel(
		"Rust Programming",
		[]string{"Rust macros", "Rust datetime library", "Learning Rust"},
		"Software Development",
	)
	if changed || got != "Rust Programming" {
		t.Fatalf("GuardLabel() = %q, %v; want the original label", got, changed)
	}
}

func TestGuardLabelKeepsGeneralLabel(t *testing.T) {
	got, changed := GuardLabel(
		"Modern Software Development",
		[]string{"Rust macros", "A Go service"},
		"Software Development",
	)
	if changed || got != "Modern Software Development" {
		t.Fatalf("GuardLabel() = %q, %v; want the original label", got, changed)
	}
}

func TestGuardLabelUsesDefaultGeneric(t *testing.T) {
	got, changed := GuardLabel("Go Programming", []string{"A Go service", "A Rust crate"}, "")
	if !changed || got != "Software Development" {
		t.Fatalf("GuardLabel() = %q, %v; want the default generic", got, changed)
	}
}
