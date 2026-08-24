package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const passwordRounds = 180000

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", errors.New("password must contain 12 to 256 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	digest := derive([]byte(password), salt, passwordRounds)
	return fmt.Sprintf("sha256i$%d$%s$%s", passwordRounds,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(digest)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "sha256i" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 100000 || rounds > 1000000 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := derive([]byte(password), salt, rounds)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func derive(password, salt []byte, rounds int) []byte {
	seed := make([]byte, 0, len(salt)+len(password))
	seed = append(seed, salt...)
	seed = append(seed, password...)
	sum := sha256.Sum256(seed)
	for i := 1; i < rounds; i++ {
		h := sha256.New()
		h.Write(sum[:])
		h.Write(salt)
		h.Write(password)
		copy(sum[:], h.Sum(nil))
	}
	return sum[:]
}
