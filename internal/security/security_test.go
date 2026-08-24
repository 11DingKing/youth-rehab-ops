package security

import (
	"strings"
	"testing"
)

func TestPasswordHashIsSaltedAndVerifiable(t *testing.T) {
	password := "a sufficiently strong password"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword second: %v", err)
	}
	if first == second {
		t.Fatal("two hashes reused the same salt")
	}
	if strings.Contains(first, password) {
		t.Fatal("encoded hash contains plaintext password")
	}
	if !VerifyPassword(first, password) || !VerifyPassword(second, password) {
		t.Fatal("valid password did not verify")
	}
	if VerifyPassword(first, password+"!") {
		t.Fatal("incorrect password verified")
	}
}

func TestPasswordPolicyRejectsUnsafeLengths(t *testing.T) {
	for _, password := range []string{"", "short", strings.Repeat("x", 257)} {
		if hash, err := HashPassword(password); err == nil || hash != "" {
			t.Fatalf("unsafe password length accepted: len=%d hash=%q err=%v", len(password), hash, err)
		}
	}
	if _, err := HashPassword(strings.Repeat("x", 12)); err != nil {
		t.Fatalf("minimum password length rejected: %v", err)
	}
}

func TestPasswordVerificationRejectsMalformedEncoding(t *testing.T) {
	malformed := []string{
		"",
		"plain",
		"sha256i$abc$salt$digest",
		"sha256i$1$c2FsdA$ZGlnZXN0",
		"sha256i$180000$not-base64$also-not-base64",
		"unknown$180000$c2FsdA$ZGlnZXN0",
	}
	for _, encoded := range malformed {
		if VerifyPassword(encoded, "a sufficiently strong password") {
			t.Errorf("malformed encoding verified: %q", encoded)
		}
	}
}

func TestOpaqueTokensAreUniqueAndOnlyHashesPersist(t *testing.T) {
	seen := map[string]bool{}
	for index := 0; index < 50; index++ {
		plain, hash, err := NewOpaqueToken()
		if err != nil {
			t.Fatalf("NewOpaqueToken: %v", err)
		}
		if len(plain) < 40 || len(hash) != 64 {
			t.Fatalf("unexpected token sizes plain=%d hash=%d", len(plain), len(hash))
		}
		if hash != HashToken(plain) {
			t.Fatal("returned hash does not match plain token")
		}
		if plain == hash {
			t.Fatal("plain token and persisted hash are identical")
		}
		if seen[plain] {
			t.Fatal("opaque token repeated")
		}
		seen[plain] = true
	}
}

func TestPublicIDsCarryDomainPrefixAndEntropy(t *testing.T) {
	for _, prefix := range []string{"inc", "participant", "notice"} {
		first, err := NewPublicID(prefix)
		if err != nil {
			t.Fatalf("NewPublicID(%s): %v", prefix, err)
		}
		second, err := NewPublicID(prefix)
		if err != nil {
			t.Fatalf("NewPublicID(%s) second: %v", prefix, err)
		}
		if !strings.HasPrefix(first, prefix+"_") || first == second {
			t.Fatalf("bad public ids: %q %q", first, second)
		}
	}
}
