package talos

import (
	"errors"
	"testing"
)

func TestIsAlreadyBootstrapped(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"already bootstrapped message", errors.New("rpc error: etcd data-dir is not empty"), true},
		{"unrelated error", errors.New("connection refused"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAlreadyBootstrapped(tc.err); got != tc.want {
				t.Errorf("isAlreadyBootstrapped(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
