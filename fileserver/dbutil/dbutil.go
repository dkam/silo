package dbutil

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// DBPair holds separate read and write database connections.
// For SQLite: write has MaxOpenConns(1) to serialize writes,
// read has MaxOpenConns(4) for concurrent reads. Both use WAL mode.
// For MySQL: both Read and Write point to the same *sql.DB.
type DBPair struct {
	Read  *sql.DB
	Write *sql.DB
}

// Close closes both database connections.
func (p *DBPair) Close() error {
	var err error
	if p.Write != nil {
		err = p.Write.Close()
	}
	if p.Read != nil && p.Read != p.Write {
		if rerr := p.Read.Close(); rerr != nil && err == nil {
			err = rerr
		}
	}
	return err
}

// OpenSQLite opens a SQLite database with WAL mode and read/write connection split.
func OpenSQLite(path string) (*DBPair, error) {
	writeDSN := fmt.Sprintf("file:%s?_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000&_pragma=synchronous%%3DNORMAL&_pragma=foreign_keys%%3DON", path)
	writeDB, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite write connection: %v", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)

	// Verify WAL mode is set on the write connection
	var journalMode string
	if err := writeDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("failed to check journal mode: %v", err)
	}
	if journalMode != "wal" {
		// Set WAL explicitly if pragma DSN didn't work
		if _, err := writeDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			writeDB.Close()
			return nil, fmt.Errorf("failed to set WAL mode: %v", err)
		}
	}

	readDSN := fmt.Sprintf("file:%s?mode=ro&_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000&_pragma=synchronous%%3DNORMAL&_pragma=foreign_keys%%3DON", path)
	readDB, err := sql.Open("sqlite", readDSN)
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("failed to open sqlite read connection: %v", err)
	}
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(4)
	readDB.SetConnMaxLifetime(0)

	return &DBPair{Read: readDB, Write: writeDB}, nil
}

// OpenMySQL opens a MySQL database. Both Read and Write use the same connection pool.
func OpenMySQL(dsn string) (*DBPair, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %v", err)
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)

	return &DBPair{Read: db, Write: db}, nil
}
