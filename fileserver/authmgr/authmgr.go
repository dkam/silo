package authmgr

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/haiwen/seafile-server/fileserver/option"
	log "github.com/sirupsen/logrus"
)

var readDB *sql.DB
var writeDB *sql.DB

func Init(ccnetReadDB, ccnetWriteDB *sql.DB) {
	readDB = ccnetReadDB
	writeDB = ccnetWriteDB
}

// Legacy fixed salt used by old Seafile SHA256 password hashing.
var legacySalt = []byte{0xdb, 0x91, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}

// ValidatePassword checks email/password against the EmailUser table.
// Returns the user email (possibly lowercased) on success.
func ValidatePassword(email, password string) (string, error) {
	if password == "!" {
		return "", fmt.Errorf("invalid password")
	}

	var storedPasswd string
	row := readDB.QueryRow("SELECT passwd FROM EmailUser WHERE email=?", email)
	err := row.Scan(&storedPasswd)
	if err == sql.ErrNoRows {
		// Try lowercased email
		emailDown := strings.ToLower(email)
		row = readDB.QueryRow("SELECT passwd FROM EmailUser WHERE email=?", emailDown)
		err = row.Scan(&storedPasswd)
		if err != nil {
			return "", fmt.Errorf("user not found")
		}
		email = emailDown
	} else if err != nil {
		return "", fmt.Errorf("database error: %v", err)
	}

	if !validatePasswd(password, storedPasswd) {
		return "", fmt.Errorf("incorrect password")
	}

	return email, nil
}

func validatePasswd(password, storedPasswd string) bool {
	if storedPasswd == "!" {
		return false
	}

	hashLen := len(storedPasswd)

	switch {
	case hashLen == sha256.Size*2:
		// Legacy SHA256 with fixed salt
		return validateSHA256Salted(password, storedPasswd)
	case hashLen == sha1.Size*2:
		// Legacy plain SHA1
		return validateSHA1(password, storedPasswd)
	default:
		// PBKDF2SHA256$iter$salt$hash
		return validatePBKDF2SHA256(password, storedPasswd)
	}
}

func validatePBKDF2SHA256(password, storedPasswd string) bool {
	parts := strings.Split(storedPasswd, "$")
	if len(parts) != 4 {
		return false
	}

	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}

	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}

	expectedHash := parts[3]

	derived := pbkdf2.Key([]byte(password), salt, iter, sha256.Size, sha256.New)
	computedHash := hex.EncodeToString(derived)

	return subtle.ConstantTimeCompare([]byte(computedHash), []byte(expectedHash)) == 1
}

func validateSHA256Salted(password, storedPasswd string) bool {
	h := sha256.New()
	h.Write([]byte(password))
	h.Write(legacySalt)
	computed := hex.EncodeToString(h.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedPasswd)) == 1
}

func validateSHA1(password, storedPasswd string) bool {
	h := sha1.New()
	h.Write([]byte(password))
	computed := hex.EncodeToString(h.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedPasswd)) == 1
}

type SessionClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateSessionToken creates a JWT session token for the given user.
func GenerateSessionToken(email string) (string, error) {
	now := time.Now()
	claims := SessionClaims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(option.JWTPrivateKey))
	if err != nil {
		return "", fmt.Errorf("failed to sign session token: %v", err)
	}

	return tokenString, nil
}

// ValidateSessionToken parses and validates a JWT session token.
// Returns the user's email on success.
func ValidateSessionToken(tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &SessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(option.JWTPrivateKey), nil
	})
	if err != nil {
		return "", fmt.Errorf("invalid token: %v", err)
	}

	claims, ok := token.Claims.(*SessionClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token claims")
	}

	return claims.Email, nil
}

// hashPassword creates a PBKDF2SHA256 hash string compatible with Seafile's format.
func hashPassword(password string) (string, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %v", err)
	}
	iterations := 10000
	derived := pbkdf2.Key([]byte(password), salt, iterations, sha256.Size, sha256.New)
	return fmt.Sprintf("PBKDF2SHA256$%d$%s$%s",
		iterations, hex.EncodeToString(salt), hex.EncodeToString(derived)), nil
}

// EnsureAdmin creates an admin user if it doesn't already exist.
func EnsureAdmin(email, password string) error {
	if email == "" || password == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), option.DBOpTimeout)
	defer cancel()

	var exists int
	err := readDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM EmailUser WHERE email=?", email).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check user: %v", err)
	}
	if exists > 0 {
		log.Infof("Admin user %s already exists", email)
		return nil
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	_, err = writeDB.ExecContext(ctx,
		"INSERT INTO EmailUser (email, passwd, is_staff, is_active, ctime) VALUES (?, ?, 1, 1, ?)",
		email, hash, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to create admin user: %v", err)
	}

	log.Infof("Created admin user: %s", email)
	return nil
}
