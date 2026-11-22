package topics

import "testing"

func TestGuardLabelGeneralizesMixedLanguage(t *testing.T) {
	got, changed := GuardLabel(
		testRustLabel,
		[]string{testRustDeclarative, testGoService, testGeneralSoftware},
		testGenericLabel,
	)
	if !changed || got != testGenericLabel {
		t.Fatalf("GuardLabel() = %q, %v; want %q, true", got, changed, testGenericLabel)
	}
}

func TestGuardLabelKeepsUnanimousLanguage(t *testing.T) {
	got, changed := GuardLabel(
		testRustLabel,
		[]string{testRustMacros, testRustDatetime, testLearningRust},
		testGenericLabel,
	)
	if changed || got != testRustLabel {
		t.Fatalf("GuardLabel() = %q, %v; want the original label", got, changed)
	}
}

func TestGuardLabelKeepsGeneralLabel(t *testing.T) {
	got, changed := GuardLabel(
		testModernSoftware,
		[]string{testRustMacros, testGoService},
		testGenericLabel,
	)
	if changed || got != testModernSoftware {
		t.Fatalf("GuardLabel() = %q, %v; want the original label", got, changed)
	}
}

func TestGuardLabelUsesDefaultGeneric(t *testing.T) {
	got, changed := GuardLabel(testGoProgrammingLabel, []string{testGoService, testRustCrate}, "")
	if !changed || got != testGenericLabel {
		t.Fatalf("GuardLabel() = %q, %v; want the default generic", got, changed)
	}
}
