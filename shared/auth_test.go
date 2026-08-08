package shared

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword returned an error: %v", err)
	}
	if !VerifyPassword("correct horse battery staple", encoded) {
		t.Fatal("expected the original password to verify")
	}
	if VerifyPassword("incorrect password", encoded) {
		t.Fatal("expected an incorrect password not to verify")
	}
}

func TestPasswordValidation(t *testing.T) {
	if err := ValidatePassword("too short"); err == nil {
		t.Fatal("expected a short password to be rejected")
	}
	if err := ValidatePassword("long enough password"); err != nil {
		t.Fatalf("expected a valid password, got %v", err)
	}
}

func TestNormalizeEmail(t *testing.T) {
	email, err := NormalizeEmail("  Person@Example.COM ")
	if err != nil {
		t.Fatalf("NormalizeEmail returned an error: %v", err)
	}
	if email != "person@example.com" {
		t.Fatalf("expected normalized email, got %q", email)
	}
	if _, err := NormalizeEmail("not an email"); err == nil {
		t.Fatal("expected invalid email to be rejected")
	}
}
