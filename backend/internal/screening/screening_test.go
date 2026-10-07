package screening

import "testing"

func TestKey(t *testing.T) {
	if Key("Obi, Ada-Mary") != Key("ADA MARY OBI") {
		t.Errorf("keys differ: %q vs %q", Key("Obi, Ada-Mary"), Key("ADA MARY OBI"))
	}
	if Key("  ") != "" {
		t.Error("blank name should have an empty key")
	}
}
