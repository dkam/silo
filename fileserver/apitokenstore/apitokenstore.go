// Package apitokenstore persists SeaDrive/Seahub-style API tokens (40-char hex
// strings) in the seafile DB so they survive server restarts. Each token maps
// to a user email.
package apitokenstore

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// ErrNotFound is returned by Lookup when the token is absent from the store.
// Callers should treat this distinctly from DB/connectivity errors so an
// outage doesn't masquerade as "invalid token".
var ErrNotFound = errors.New("api token not found")

var (
	readDB  *sql.DB
	writeDB *sql.DB
)

func Init(read, write *sql.DB) {
	readDB = read
	writeDB = write
}

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

func Lookup(token string) (string, error) {
	var email string
	err := readDB.QueryRow(
		"SELECT email FROM ApiToken WHERE token = ?", token,
	).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return email, nil
}

func Delete(token string) error {
	_, err := writeDB.Exec("DELETE FROM ApiToken WHERE token = ?", token)
	return err
}
