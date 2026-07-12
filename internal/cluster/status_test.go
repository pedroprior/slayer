package cluster

import (
	"strings"
	"testing"
)

func TestFormatStatusReport(t *testing.T) {
	cp := []NodeStatus{{Name: "talos-cp-01", IP: "192.168.122.50", Running: true}}
	worker := []NodeStatus{{Name: "talos-worker-01", IP: "", Running: false}}

	report := formatStatusReport(cp, worker)

	for _, want := range []string{"talos-cp-01", "192.168.122.50", "running", "talos-worker-01", "not running"} {
		if !strings.Contains(report, want) {
			t.Errorf("formatStatusReport() missing %q in:\n%s", want, report)
		}
	}
}
