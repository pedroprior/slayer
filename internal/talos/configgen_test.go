package talos

import (
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

func TestBuildVIPPatch(t *testing.T) {
	patch := BuildVIPPatch("192.168.122.100", "52:54:00:aa:bb:cc")

	for _, want := range []string{
		`hardwareAddr: "52:54:00:aa:bb:cc"`,
		"dhcp: true",
		"ip: 192.168.122.100",
	} {
		if !strings.Contains(patch, want) {
			t.Errorf("BuildVIPPatch() missing %q in:\n%s", want, patch)
		}
	}
}

// TestApplyVIPPatch guards against regressing to naive "---"-separated YAML
// concatenation, which Talos rejects with "duplicate document /v1alpha1/ is
// not allowed" because it produces two top-level config documents instead of
// one strategically merged document.
func TestApplyVIPPatch(t *testing.T) {
	secretsBundle, err := secrets.NewBundle(secrets.NewClock(), nil)
	if err != nil {
		t.Fatalf("generating secrets bundle: %v", err)
	}

	input, err := generate.NewInput("test-cluster", "https://192.168.122.250:6443", "", generate.WithSecretsBundle(secretsBundle))
	if err != nil {
		t.Fatalf("building generate input: %v", err)
	}

	cpProvider, err := input.Config(machine.TypeControlPlane)
	if err != nil {
		t.Fatalf("generating control plane config: %v", err)
	}

	cpBytes, err := cpProvider.Bytes()
	if err != nil {
		t.Fatalf("marshaling control plane config: %v", err)
	}

	patched, err := ApplyVIPPatch(cpBytes, "192.168.122.100", "52:54:00:aa:bb:cc")
	if err != nil {
		t.Fatalf("ApplyVIPPatch() returned error: %v", err)
	}

	if _, err := configloader.NewFromBytes(patched); err != nil {
		t.Errorf("ApplyVIPPatch() output is not a valid single Talos config document: %v", err)
	}

	if !strings.Contains(string(patched), "ip: 192.168.122.100") {
		t.Errorf("ApplyVIPPatch() output missing VIP:\n%s", patched)
	}

	if !strings.Contains(string(patched), `hardwareAddr: 52:54:00:aa:bb:cc`) {
		t.Errorf("ApplyVIPPatch() output missing MAC device selector:\n%s", patched)
	}
}
