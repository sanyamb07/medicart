package auth

import "testing"

func TestNormaliseMobile(t *testing.T) {
	cases := map[string]string{
		"9876543210":      "9876543210",
		"+91 98765 43210": "9876543210",
		"919876543210":    "9876543210",
		"09876543210":     "9876543210",
		"98765-43210":     "9876543210",
	}
	for in, want := range cases {
		if got := NormaliseMobile(in); got != want {
			t.Errorf("NormaliseMobile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsValidMobile(t *testing.T) {
	valid := []string{"9876543210", "6000000000", "7111111111", "8999999999"}
	for _, m := range valid {
		if !IsValidMobile(m) {
			t.Errorf("%q should be valid", m)
		}
	}
	invalid := []string{"", "123", "12345678901", "5876543210", "abcdefghij", "98765432100"}
	for _, m := range invalid {
		if IsValidMobile(m) {
			t.Errorf("%q should be invalid", m)
		}
	}
}

func TestIsValidPIN(t *testing.T) {
	valid := []string{"400001", "110001", "560001", "999999"}
	for _, p := range valid {
		if !IsValidPIN(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	invalid := []string{"", "12345", "0400001", "40000a", "4000011"}
	for _, p := range invalid {
		if IsValidPIN(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	if err := ValidatePasswordStrength("short1!"); err == nil {
		t.Error("expected short password to be rejected")
	}
	if err := ValidatePasswordStrength("ChangeMe123!dev"); err != nil {
		t.Errorf("expected valid password to pass, got %v", err)
	}
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidatePasswordStrength(string(long)); err == nil {
		t.Error("expected overlong password to be rejected")
	}
}

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("CorrectHorse123!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == "CorrectHorse123!" {
		t.Fatal("hash should not be empty or equal to the plaintext")
	}
}
