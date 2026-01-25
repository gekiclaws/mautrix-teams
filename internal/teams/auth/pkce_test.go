package auth

import "testing"

func TestCodeChallengeS256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := CodeChallengeS256(verifier); got != expected {
		t.Fatalf("unexpected challenge: %s", got)
	}
}

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier failed: %v", err)
	}
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("unexpected verifier length: %d", len(verifier))
	}
	for _, r := range verifier {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			t.Fatalf("unexpected character in verifier: %q", r)
		}
	}
}
