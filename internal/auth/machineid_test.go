package auth

import (
	"testing"
)

func TestMachineKey_Deterministic(t *testing.T) {
	k1, err := machineKey()
	if err != nil {
		t.Fatalf("first key: %v", err)
	}

	k2, err := machineKey()
	if err != nil {
		t.Fatalf("second key: %v", err)
	}

	if len(k1) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(k1))
	}

	for i := range k1 {
		if k1[i] != k2[i] {
			t.Errorf("keys differ at byte %d", i)
			break
		}
	}
}

func TestMachineKey_NotEmpty(t *testing.T) {
	key, err := machineKey()
	if err != nil {
		t.Fatalf("machine key: %v", err)
	}

	zero := true
	for _, b := range key {
		if b != 0 {
			zero = false
			break
		}
	}
	if zero {
		t.Fatal("key is all zeros")
	}
}
