package cluster

import (
	"errors"
	"testing"
)

func TestCollectErrors_FiltersNil(t *testing.T) {
	errs := collectErrors(nil, errors.New("boom"), nil, errors.New("bang"))
	if len(errs) != 2 {
		t.Fatalf("collectErrors() returned %d errors, want 2", len(errs))
	}
}

func TestCollectErrors_AllNil(t *testing.T) {
	errs := collectErrors(nil, nil)
	if len(errs) != 0 {
		t.Fatalf("collectErrors() returned %d errors, want 0", len(errs))
	}
}
