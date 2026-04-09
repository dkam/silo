package authmgr

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/pbkdf2"

	"github.com/haiwen/seafile-server/fileserver/option"
)

func init() {
	option.JWTPrivateKey = "test-secret-key-for-unit-tests"
}

// Helper: generate a PBKDF2SHA256 stored hash for testing.
func makePBKDF2Hash(password string, iter int, salt []byte) string {
	derived := pbkdf2.Key([]byte(password), salt, iter, sha256.Size, sha256.New)
	return "PBKDF2SHA256$" +
		"10000$" +
		hex.EncodeToString(salt) + "$" +
		hex.EncodeToString(derived)
}

func TestValidatePasswdPBKDF2(t *testing.T) {
	salt := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	stored := makePBKDF2Hash("correct-password", 10000, salt)

	if !validatePasswd("correct-password", stored) {
		t.Error("expected correct password to validate")
	}
	if validatePasswd("wrong-password", stored) {
		t.Error("expected wrong password to fail")
	}
}

func TestValidatePasswdSHA256Salted(t *testing.T) {
	h := sha256.New()
	h.Write([]byte("mypassword"))
	h.Write(legacySalt)
	stored := hex.EncodeToString(h.Sum(nil))

	if len(stored) != sha256.Size*2 {
		t.Fatalf("unexpected hash length: %d", len(stored))
	}

	if !validatePasswd("mypassword", stored) {
		t.Error("expected correct password to validate")
	}
	if validatePasswd("wrong", stored) {
		t.Error("expected wrong password to fail")
	}
}

func TestValidatePasswdSHA1(t *testing.T) {
	h := sha1.New()
	h.Write([]byte("mypassword"))
	stored := hex.EncodeToString(h.Sum(nil))

	if len(stored) != sha1.Size*2 {
		t.Fatalf("unexpected hash length: %d", len(stored))
	}

	if !validatePasswd("mypassword", stored) {
		t.Error("expected correct password to validate")
	}
	if validatePasswd("wrong", stored) {
		t.Error("expected wrong password to fail")
	}
}

func TestValidatePasswdDisabledAccount(t *testing.T) {
	if validatePasswd("anything", "!") {
		t.Error("disabled account (!) should never validate")
	}
}

func TestValidatePasswdMalformedPBKDF2(t *testing.T) {
	// Wrong number of parts
	if validatePasswd("test", "PBKDF2SHA256$only$two") {
		t.Error("malformed PBKDF2 should fail")
	}
	// Non-numeric iterations
	if validatePasswd("test", "PBKDF2SHA256$notanumber$0102$abcd") {
		t.Error("non-numeric iterations should fail")
	}
	// Invalid hex salt
	if validatePasswd("test", "PBKDF2SHA256$10000$zzzz$abcd") {
		t.Error("invalid hex salt should fail")
	}
}

func TestGenerateAndValidateSessionToken(t *testing.T) {
	token, err := GenerateSessionToken("user@example.com")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	email, err := ValidateSessionToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if email != "user@example.com" {
		t.Errorf("expected user@example.com, got %s", email)
	}
}

func TestValidateSessionTokenInvalid(t *testing.T) {
	_, err := ValidateSessionToken("not-a-valid-jwt")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestValidateSessionTokenWrongKey(t *testing.T) {
	token, err := GenerateSessionToken("user@example.com")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Change the signing key
	original := option.JWTPrivateKey
	option.JWTPrivateKey = "different-secret-key"
	defer func() { option.JWTPrivateKey = original }()

	_, err = ValidateSessionToken(token)
	if err == nil {
		t.Error("expected error when validating with wrong key")
	}
}

func TestSessionTokenDifferentUsers(t *testing.T) {
	t1, _ := GenerateSessionToken("alice@example.com")
	t2, _ := GenerateSessionToken("bob@example.com")
	if t1 == t2 {
		t.Error("expected different tokens for different users")
	}
}
