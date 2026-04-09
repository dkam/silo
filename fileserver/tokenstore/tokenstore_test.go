package tokenstore

import (
	"testing"
	"time"
)

func TestCreateAndQueryToken(t *testing.T) {
	token := CreateToken("repo-1", "obj-1", "download", "user@test.com", false)
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	info := QueryToken(token)
	if info == nil {
		t.Fatal("expected token to be found")
	}
	if info.RepoID != "repo-1" || info.ObjID != "obj-1" || info.Op != "download" || info.User != "user@test.com" {
		t.Errorf("unexpected token info: %+v", info)
	}
}

func TestQueryTokenReusable(t *testing.T) {
	token := CreateToken("repo-1", "obj-1", "download", "user@test.com", false)

	// Non-one-time tokens should survive multiple queries
	for i := 0; i < 3; i++ {
		info := QueryToken(token)
		if info == nil {
			t.Fatalf("query %d: expected token to still exist", i+1)
		}
	}
}

func TestQueryTokenOneTime(t *testing.T) {
	token := CreateToken("repo-1", "obj-1", "download", "user@test.com", true)

	info := QueryToken(token)
	if info == nil {
		t.Fatal("first query should return token")
	}

	info = QueryToken(token)
	if info != nil {
		t.Fatal("second query should return nil for one-time token")
	}
}

func TestQueryTokenNotFound(t *testing.T) {
	info := QueryToken("nonexistent-token")
	if info != nil {
		t.Fatal("expected nil for nonexistent token")
	}
}

func TestQueryTokenExpired(t *testing.T) {
	token := CreateToken("repo-1", "obj-1", "download", "user@test.com", false)

	// Manually expire the token
	val, _ := tokens.Load(token)
	val.(*AccessInfo).ExpireTime = time.Now().Unix() - 1

	info := QueryToken(token)
	if info != nil {
		t.Fatal("expected nil for expired token")
	}

	// Should also be cleaned up from the map
	if _, ok := tokens.Load(token); ok {
		t.Fatal("expired token should have been deleted from map")
	}
}

func TestDeleteToken(t *testing.T) {
	token := CreateToken("repo-1", "obj-1", "download", "user@test.com", false)

	DeleteToken(token)

	info := QueryToken(token)
	if info != nil {
		t.Fatal("expected nil after delete")
	}
}
