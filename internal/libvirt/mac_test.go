package libvirt

import "testing"

func TestDeterministicMAC_StableAndUnique(t *testing.T) {
	a1 := DeterministicMAC("talos-cp-01")
	a2 := DeterministicMAC("talos-cp-01")
	b := DeterministicMAC("talos-cp-02")

	if a1 != a2 {
		t.Errorf("DeterministicMAC(%q) = %q then %q, want stable across calls", "talos-cp-01", a1, a2)
	}
	if a1 == b {
		t.Errorf("DeterministicMAC() returned same MAC %q for different names", a1)
	}
	if want := "52:54:00:"; a1[:len(want)] != want {
		t.Errorf("DeterministicMAC() = %q, want %s prefix", a1, want)
	}
}
