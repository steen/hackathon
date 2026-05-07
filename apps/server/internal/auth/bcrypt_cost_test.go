package auth

import (
	"strings"
	"testing"
)

func TestSetBcryptCost_AcceptsValidRange(t *testing.T) {
	// Floor and ceiling both accepted; restore default after each so the
	// global state does not leak into sibling tests in this package.
	defer func() { _ = SetBcryptCost(DefaultBcryptCost) }()

	for _, c := range []int{MinBcryptCost, MinBcryptCost + 1, 14, MaxBcryptCost} {
		if err := SetBcryptCost(c); err != nil {
			t.Fatalf("SetBcryptCost(%d): unexpected error: %v", c, err)
		}
		if BcryptCost != c {
			t.Fatalf("after SetBcryptCost(%d): BcryptCost = %d", c, BcryptCost)
		}
	}
}

func TestSetBcryptCost_RejectsBelowFloor(t *testing.T) {
	defer func() { _ = SetBcryptCost(DefaultBcryptCost) }()

	for _, c := range []int{MinBcryptCost - 1, 0, -1} {
		err := SetBcryptCost(c)
		if err == nil {
			t.Fatalf("SetBcryptCost(%d): expected error, got nil", c)
		}
		if !strings.Contains(err.Error(), "out of range") {
			t.Errorf("SetBcryptCost(%d): error %q should mention 'out of range'", c, err)
		}
	}
}

func TestSetBcryptCost_RejectsAboveCeiling(t *testing.T) {
	defer func() { _ = SetBcryptCost(DefaultBcryptCost) }()

	for _, c := range []int{MaxBcryptCost + 1, 100} {
		err := SetBcryptCost(c)
		if err == nil {
			t.Fatalf("SetBcryptCost(%d): expected error, got nil", c)
		}
	}
}
