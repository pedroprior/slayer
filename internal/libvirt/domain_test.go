package libvirt

import (
	"errors"
	"strings"
	"testing"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"
)

func TestBuildDomainXML(t *testing.T) {
	spec := DomainSpec{
		Name:        "talos-cp-01",
		RAMMB:       4096,
		VCPUs:       2,
		DiskGB:      30,
		DiskDir:     "/var/lib/libvirt/images",
		ISOPath:     "/var/lib/libvirt/images/metal-amd64.iso",
		NetworkName: "default",
		MAC:         "52:54:00:aa:bb:cc",
	}

	xml := buildDomainXML(spec)

	for _, want := range []string{
		`<name>talos-cp-01</name>`,
		`<memory unit="MiB">4096</memory>`,
		`<vcpu placement="static">2</vcpu>`,
		`/var/lib/libvirt/images/talos-cp-01.qcow2`,
		`/var/lib/libvirt/images/metal-amd64.iso`,
		`<source network="default"/>`,
		`<mac address="52:54:00:aa:bb:cc"/>`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("buildDomainXML() missing %q in:\n%s", want, xml)
		}
	}
}

// Regression test: without an explicit <features><acpi/><apic/></features>
// block, libvirt/QEMU boot the domain with ACPI disabled (acpi=off on the
// qemu command line). On a multi-vCPU guest this can prevent the firmware
// from ever completing boot: talos-cp-01 was observed running for 4+ minutes
// pinned near 100% CPU with zero packets ever sent on its NIC and no
// coherent serial console output.
func TestBuildDomainXML_EnablesACPIAndAPIC(t *testing.T) {
	xml := buildDomainXML(testSpec())

	for _, want := range []string{"<acpi/>", "<apic/>"} {
		if !strings.Contains(xml, want) {
			t.Errorf("buildDomainXML() missing %q (ACPI/APIC must be explicitly enabled), got:\n%s", want, xml)
		}
	}
}

// Regression test: without an explicit <cpu> element, QEMU defaults to the
// generic "qemu64" baseline model (fallback="forbid", modern extensions like
// AES-NI/AVX disabled) instead of exposing the host's real CPU, making boot
// and any compute-heavy work needlessly slow.
func TestBuildDomainXML_UsesHostPassthroughCPU(t *testing.T) {
	xml := buildDomainXML(testSpec())

	if !strings.Contains(xml, `<cpu mode="host-passthrough"`) {
		t.Errorf(`buildDomainXML() missing host-passthrough <cpu> element, got:\n%s`, xml)
	}
}

// Regression test: libvirt has no "none" graphics type — `<graphics
// type="none"/>` is rejected at DomainDefineXML with "Invalid value for
// attribute 'type'". A headless domain must simply omit <graphics> entirely.
func TestBuildDomainXML_OmitsGraphicsElement(t *testing.T) {
	xml := buildDomainXML(testSpec())

	if strings.Contains(xml, "<graphics") {
		t.Errorf("buildDomainXML() must not include a <graphics> element for a headless domain, got:\n%s", xml)
	}
}

// fakeDomainClient is an in-memory stand-in for a libvirtd connection,
// implementing domainLifecycler so EnsureDomain/Stop's branching logic can be
// tested without a real libvirt daemon.
type fakeDomainClient struct {
	found       bool
	notFoundErr error

	// isActiveResponses is consumed in order by successive DomainIsActive
	// calls; the last entry repeats once exhausted. Defaults to "inactive".
	isActiveResponses []int32
	isActiveIdx       int
	isActiveErr       error

	defineErr, createErr, shutdownErr, destroyErr         error
	defineCalls, createCalls, shutdownCalls, destroyCalls int
}

func (f *fakeDomainClient) DomainLookupByName(name string) (golibvirt.Domain, error) {
	if !f.found {
		return golibvirt.Domain{}, f.notFoundErr
	}
	return golibvirt.Domain{Name: name}, nil
}

func (f *fakeDomainClient) DomainDefineXML(xml string) (golibvirt.Domain, error) {
	f.defineCalls++
	if f.defineErr != nil {
		return golibvirt.Domain{}, f.defineErr
	}
	f.found = true
	return golibvirt.Domain{}, nil
}

func (f *fakeDomainClient) DomainIsActive(dom golibvirt.Domain) (int32, error) {
	if f.isActiveErr != nil {
		return 0, f.isActiveErr
	}
	if len(f.isActiveResponses) == 0 {
		return 0, nil
	}
	idx := f.isActiveIdx
	if idx >= len(f.isActiveResponses) {
		idx = len(f.isActiveResponses) - 1
	} else {
		f.isActiveIdx++
	}
	return f.isActiveResponses[idx], nil
}

func (f *fakeDomainClient) DomainCreate(dom golibvirt.Domain) error {
	f.createCalls++
	return f.createErr
}

func (f *fakeDomainClient) DomainShutdown(dom golibvirt.Domain) error {
	f.shutdownCalls++
	return f.shutdownErr
}

func (f *fakeDomainClient) DomainDestroy(dom golibvirt.Domain) error {
	f.destroyCalls++
	return f.destroyErr
}

func testSpec() DomainSpec {
	return DomainSpec{
		Name:        "talos-cp-01",
		RAMMB:       4096,
		VCPUs:       2,
		DiskGB:      30,
		DiskDir:     "/var/lib/libvirt/images",
		ISOPath:     "/var/lib/libvirt/images/metal-amd64.iso",
		NetworkName: "default",
		MAC:         "52:54:00:aa:bb:cc",
	}
}

func TestEnsureDomain_DefinesAndStartsWhenMissing(t *testing.T) {
	f := &fakeDomainClient{notFoundErr: errors.New("domain not found")}

	if err := ensureDomain(f, testSpec()); err != nil {
		t.Fatalf("ensureDomain() error = %v, want nil", err)
	}
	if f.defineCalls != 1 {
		t.Errorf("defineCalls = %d, want 1", f.defineCalls)
	}
	if f.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", f.createCalls)
	}
}

func TestEnsureDomain_NoopWhenAlreadyRunning(t *testing.T) {
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{1}}

	if err := ensureDomain(f, testSpec()); err != nil {
		t.Fatalf("ensureDomain() error = %v, want nil", err)
	}
	if f.defineCalls != 0 {
		t.Errorf("defineCalls = %d, want 0", f.defineCalls)
	}
	if f.createCalls != 0 {
		t.Errorf("createCalls = %d, want 0 (already running domain must not be touched)", f.createCalls)
	}
}

// Regression test: EnsureDomain previously no-opped on any existing domain,
// even one that was defined but shut off, so `provision` couldn't restart a
// stopped VM.
func TestEnsureDomain_StartsWhenDefinedButStopped(t *testing.T) {
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{0}}

	if err := ensureDomain(f, testSpec()); err != nil {
		t.Fatalf("ensureDomain() error = %v, want nil", err)
	}
	if f.defineCalls != 0 {
		t.Errorf("defineCalls = %d, want 0 (domain already defined)", f.defineCalls)
	}
	if f.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1 (stopped domain must be started)", f.createCalls)
	}
}

func TestEnsureDomain_PropagatesDefineError(t *testing.T) {
	f := &fakeDomainClient{notFoundErr: errors.New("not found"), defineErr: errors.New("boom")}

	if err := ensureDomain(f, testSpec()); err == nil {
		t.Fatal("ensureDomain() error = nil, want error from DomainDefineXML")
	}
}

func TestEnsureDomain_PropagatesCreateError(t *testing.T) {
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{0}, createErr: errors.New("boom")}

	if err := ensureDomain(f, testSpec()); err == nil {
		t.Fatal("ensureDomain() error = nil, want error from DomainCreate")
	}
}

func TestStop_NoopWhenDomainMissing(t *testing.T) {
	f := &fakeDomainClient{notFoundErr: errors.New("not found")}

	if err := stopDomain(f, "talos-cp-01", 3, time.Millisecond); err != nil {
		t.Fatalf("stopDomain() error = %v, want nil", err)
	}
	if f.shutdownCalls != 0 {
		t.Errorf("shutdownCalls = %d, want 0", f.shutdownCalls)
	}
}

func TestStop_NoopWhenAlreadyStopped(t *testing.T) {
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{0}}

	if err := stopDomain(f, "talos-cp-01", 3, time.Millisecond); err != nil {
		t.Fatalf("stopDomain() error = %v, want nil", err)
	}
	if f.shutdownCalls != 0 {
		t.Errorf("shutdownCalls = %d, want 0", f.shutdownCalls)
	}
}

func TestStop_GracefulShutdownSucceeds(t *testing.T) {
	// Active on the initial check and the first poll, then reports stopped.
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{1, 1, 0}}

	if err := stopDomain(f, "talos-cp-01", 5, time.Millisecond); err != nil {
		t.Fatalf("stopDomain() error = %v, want nil", err)
	}
	if f.shutdownCalls != 1 {
		t.Errorf("shutdownCalls = %d, want 1", f.shutdownCalls)
	}
	if f.destroyCalls != 0 {
		t.Errorf("destroyCalls = %d, want 0 (graceful shutdown succeeded, force destroy should not run)", f.destroyCalls)
	}
}

func TestStop_ForcesPowerOffWhenGracefulShutdownTimesOut(t *testing.T) {
	// Always reports active, so graceful shutdown never converges.
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{1}}

	if err := stopDomain(f, "talos-cp-01", 2, time.Millisecond); err != nil {
		t.Fatalf("stopDomain() error = %v, want nil", err)
	}
	if f.shutdownCalls != 1 {
		t.Errorf("shutdownCalls = %d, want 1", f.shutdownCalls)
	}
	if f.destroyCalls != 1 {
		t.Errorf("destroyCalls = %d, want 1 (must force power-off after timeout)", f.destroyCalls)
	}
}

func TestStop_PropagatesShutdownError(t *testing.T) {
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{1}, shutdownErr: errors.New("boom")}

	if err := stopDomain(f, "talos-cp-01", 2, time.Millisecond); err == nil {
		t.Fatal("stopDomain() error = nil, want error from DomainShutdown")
	}
}

// Regression test: DomainGetXMLDesc returns libvirt's normalized XML, which
// uses single-quoted attributes (<mac address='...'/>) regardless of the
// double-quoted form buildDomainXML wrote when defining the domain. A regex
// anchored only to double quotes silently fails to find a real domain's MAC.
func TestParseDomainMAC_HandlesSingleAndDoubleQuotes(t *testing.T) {
	for _, xml := range []string{
		`<interface type='network'><mac address='52:54:00:fb:9b:c0'/><source network='default'/></interface>`,
		`<interface type="network"><mac address="52:54:00:fb:9b:c0"/><source network="default"/></interface>`,
	} {
		mac, err := parseDomainMAC(xml)
		if err != nil {
			t.Fatalf("parseDomainMAC(%q) error = %v, want nil", xml, err)
		}
		if mac != "52:54:00:fb:9b:c0" {
			t.Errorf("parseDomainMAC(%q) = %q, want 52:54:00:fb:9b:c0", xml, mac)
		}
	}
}

func TestParseDomainMAC_NoMatch(t *testing.T) {
	if _, err := parseDomainMAC(`<domain></domain>`); err == nil {
		t.Fatal("parseDomainMAC() error = nil, want error for XML with no mac element")
	}
}

func TestStop_PropagatesDestroyError(t *testing.T) {
	f := &fakeDomainClient{found: true, isActiveResponses: []int32{1}, destroyErr: errors.New("boom")}

	if err := stopDomain(f, "talos-cp-01", 1, time.Millisecond); err == nil {
		t.Fatal("stopDomain() error = nil, want error from DomainDestroy")
	}
}
