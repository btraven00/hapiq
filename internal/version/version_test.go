package version

import "testing"

func TestStringPrefersStampedVersion(t *testing.T) {
	t.Cleanup(func() { Version = "" })

	Version = "0.1.0"
	if got := String(); got != "0.1.0" {
		t.Errorf("String() = %q, want 0.1.0", got)
	}

	Version = ""
	if got := String(); got == "" || got == "0.1.0" {
		t.Errorf("String() = %q, want VCS-derived fallback", got)
	}
}
