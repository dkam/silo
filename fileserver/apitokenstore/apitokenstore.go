// Package apitokenstore persists SeaDrive/Seahub-style API tokens (40-char hex
// strings) in the seafile DB so they survive server restarts. Each token maps
// to a user email.
package apitokenstore

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

var (
	readDB  *sql.DB
	writeDB *sql.DB
)

// Init wires up the read/write DB handles. Must be called at startup before
// any Create/Lookup/Delete calls.
func Init(read, write *sql.DB) {
	readDB = read
	writeDB = write
}

// Create generates a new 40-char hex API token for the given email, stores it
// in the ApiToken table, and returns it.
func Create(email string) (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	_, err := writeDB.Exec(
		"INSERT INTO ApiToken (token, email, ctime) VALUES (?, ?, ?)",
		token, email, time.Now().Unix(),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Lookup returns the email associated with a token, or "" if not found.
func Lookup(token string) string {
	var email string
	err := readDB.QueryRow(
		"SELECT email FROM ApiToken WHERE token = ?", token,
	).Scan(&email)
	if err != nil {
		return ""
	}
	return email
}

// Delete removes a token from the store.
func Delete(token string) {
	_, _ = writeDB.Exec("DELETE FROM ApiToken WHERE token = ?", token)
}
