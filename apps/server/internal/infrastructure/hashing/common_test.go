package hashing

import "testing"

func TestIsCommonPassword(t *testing.T) {
	for _, pw := range []string{"password1", "PASSWORD1", "1234567890"} {
		if !IsCommonPassword(pw) {
			t.Errorf("IsCommonPassword(%q) = false; want true", pw)
		}
	}
	if IsCommonPassword("Zx9!kelp-orbit") {
		t.Error("IsCommonPassword(strong) = true; want false")
	}
	if n := len(commonPasswords()); n < 9_000 {
		t.Errorf("list has %d entries; want >= 9000", n)
	}
	if IsCommonPassword("# Source") {
		t.Error("comment lines must not be loaded")
	}
}
