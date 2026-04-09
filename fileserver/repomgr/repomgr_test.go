package repomgr

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/haiwen/seafile-server/fileserver/commitmgr"
)

const (
	user            = "seafile"
	password        = "seafile"
	host            = "127.0.0.1"
	port            = 3306
	dbName          = "seafile-db"
	useTLS          = false
	seafileConfPath = "/root/conf"
	seafileDataDir  = "/root/conf/seafile-data"
)

// repoID must be set to an existing repo for tests to pass.
// TODO: replace with Go-native repo creation once repo management API exists.
var repoID string

func TestMain(m *testing.M) {
	repoID = os.Getenv("TEST_REPO_ID")
	if repoID == "" {
		fmt.Println("Skipping repomgr tests: TEST_REPO_ID not set")
		os.Exit(0)
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?tls=%t", user, password, host, port, dbName, useTLS)
	seafDB, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Printf("Failed to open database: %v", err)
		os.Exit(1)
	}
	Init(seafDB)
	commitmgr.Init(seafileConfPath, seafileDataDir)
	code := m.Run()
	os.Exit(code)
}

func TestGet(t *testing.T) {
	repo := Get(repoID)
	if repo == nil {
		t.Errorf("failed to get repo : %s.\n", repoID)
		t.FailNow()
	}

	if repo.ID != repoID {
		t.Errorf("failed to get repo : %s.\n", repoID)
	}
}
