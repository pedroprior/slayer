package cluster

import (
	"reflect"
	"testing"
)

func TestNodeNames(t *testing.T) {
	got := nodeNames("talos-cp", 3)
	want := []string{"talos-cp-01", "talos-cp-02", "talos-cp-03"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nodeNames(%q, 3) = %v, want %v", "talos-cp", got, want)
	}
}

func TestNodeNames_DoubleDigit(t *testing.T) {
	got := nodeNames("talos-worker", 10)
	if got[9] != "talos-worker-10" {
		t.Errorf("nodeNames(...)[9] = %q, want talos-worker-10", got[9])
	}
}
