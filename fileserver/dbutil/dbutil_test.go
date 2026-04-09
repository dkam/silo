package dbutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInsertOrReplaceMySQL(t *testing.T) {
	DBEngine = "mysql"

	got := InsertOrReplace("RepoHead", "repo_id, branch_name")
	want := "REPLACE INTO RepoHead (repo_id, branch_name) VALUES (?, ?)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrReplaceSQLite(t *testing.T) {
	DBEngine = "sqlite"

	got := InsertOrReplace("RepoHead", "repo_id, branch_name")
	want := "INSERT OR REPLACE INTO RepoHead (repo_id, branch_name) VALUES (?, ?)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrReplacePostgres(t *testing.T) {
	DBEngine = "postgres"

	got := InsertOrReplace("RepoHead", "repo_id, branch_name")
	want := "INSERT INTO RepoHead (repo_id, branch_name) VALUES (?, ?) ON CONFLICT (repo_id) DO UPDATE SET branch_name=EXCLUDED.branch_name"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrReplacePostgresSingleColumn(t *testing.T) {
	DBEngine = "postgres"

	got := InsertOrReplace("GarbageRepos", "repo_id")
	want := "INSERT INTO GarbageRepos (repo_id) VALUES (?) ON CONFLICT (repo_id) DO NOTHING"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrIgnoreMySQL(t *testing.T) {
	DBEngine = "mysql"

	got := InsertOrIgnore("GarbageRepos", "repo_id")
	want := "INSERT IGNORE INTO GarbageRepos (repo_id) VALUES (?)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrIgnoreSQLite(t *testing.T) {
	DBEngine = "sqlite"

	got := InsertOrIgnore("GarbageRepos", "repo_id")
	want := "INSERT OR IGNORE INTO GarbageRepos (repo_id) VALUES (?)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrIgnorePostgres(t *testing.T) {
	DBEngine = "postgres"

	got := InsertOrIgnore("GarbageRepos", "repo_id")
	want := "INSERT INTO GarbageRepos (repo_id) VALUES (?) ON CONFLICT DO NOTHING"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrIgnoreMultipleColumns(t *testing.T) {
	DBEngine = "mysql"

	got := InsertOrIgnore("RepoUserToken", "repo_id, email, token")
	want := "INSERT IGNORE INTO RepoUserToken (repo_id, email, token) VALUES (?, ?, ?)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertOrReplaceMultipleColumns(t *testing.T) {
	DBEngine = "postgres"

	got := InsertOrReplace("RepoOwner", "repo_id, owner_id")
	want := "INSERT INTO RepoOwner (repo_id, owner_id) VALUES (?, ?) ON CONFLICT (repo_id) DO UPDATE SET owner_id=EXCLUDED.owner_id"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOpenSQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	pair, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer pair.Close()

	if pair.Read == nil || pair.Write == nil {
		t.Fatal("expected non-nil Read and Write handles")
	}
	if pair.Read == pair.Write {
		t.Fatal("SQLite Read and Write should be separate connections")
	}

	// Verify WAL mode
	var mode string
	if err := pair.Write.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %q", mode)
	}
}

func TestOpenSQLiteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "new.db")

	pair, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	pair.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("expected database file to be created")
	}
}

func TestDBPairCloseShared(t *testing.T) {
	// Simulate MySQL where Read == Write — Close should only close once
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "shared.db")

	pair, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	// Make Read point to same handle as Write (MySQL behavior)
	pair.Read.Close()
	pair.Read = pair.Write

	if err := pair.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestSQLiteUpsertIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "upsert.db")

	pair, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer pair.Close()

	DBEngine = "sqlite"

	// Create a test table
	if _, err := pair.Write.Exec("CREATE TABLE test_kv (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert a row
	sql := InsertOrReplace("test_kv", "key, value")
	if _, err := pair.Write.Exec(sql, "k1", "v1"); err != nil {
		t.Fatalf("initial insert failed: %v", err)
	}

	// Upsert should overwrite
	if _, err := pair.Write.Exec(sql, "k1", "v2"); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	var val string
	if err := pair.Read.QueryRow("SELECT value FROM test_kv WHERE key=?", "k1").Scan(&val); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if val != "v2" {
		t.Errorf("expected v2 after upsert, got %q", val)
	}

	// Count should be 1, not 2
	var count int
	if err := pair.Read.QueryRow("SELECT COUNT(*) FROM test_kv").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after upsert, got %d", count)
	}
}

func TestSQLiteInsertOrIgnoreIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ignore.db")

	pair, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer pair.Close()

	DBEngine = "sqlite"

	if _, err := pair.Write.Exec("CREATE TABLE test_ids (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	sql := InsertOrIgnore("test_ids", "id")

	// First insert succeeds
	if _, err := pair.Write.Exec(sql, "abc"); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Duplicate insert should not error
	if _, err := pair.Write.Exec(sql, "abc"); err != nil {
		t.Fatalf("duplicate insert should not error: %v", err)
	}

	// Should still be 1 row
	var count int
	if err := pair.Read.QueryRow("SELECT COUNT(*) FROM test_ids").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestSQLiteReadWriteSplit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "split.db")

	pair, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer pair.Close()

	if _, err := pair.Write.Exec("CREATE TABLE nums (n INTEGER)"); err != nil {
		t.Fatalf("create table failed: %v", err)
	}
	if _, err := pair.Write.Exec("INSERT INTO nums VALUES (42)"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Read connection should see the write (WAL mode)
	var n int
	if err := pair.Read.QueryRow("SELECT n FROM nums").Scan(&n); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}

	// Write via read connection should fail (read-only)
	_, err = pair.Read.Exec("INSERT INTO nums VALUES (99)")
	if err == nil {
		t.Fatal("expected error writing via read-only connection")
	}
}

func openTestDB(path string) (*DBPair, error) {
	return OpenSQLite(path)
}
