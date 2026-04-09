package keycache

import (
	"testing"
	"time"
)

func TestSetAndGetKey(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte("abcdef0123456789")

	SetKey("repo-1", "user@test.com", key, iv, 2)

	dk := GetKey("repo-1", "user@test.com")
	if dk == nil {
		t.Fatal("expected key to be found")
	}
	if string(dk.Key) != string(key) {
		t.Errorf("key mismatch: got %x", dk.Key)
	}
	if string(dk.IV) != string(iv) {
		t.Errorf("iv mismatch: got %x", dk.IV)
	}
	if dk.Version != 2 {
		t.Errorf("version mismatch: got %d", dk.Version)
	}
}

func TestGetKeyNotFound(t *testing.T) {
	dk := GetKey("nonexistent-repo", "nobody@test.com")
	if dk != nil {
		t.Fatal("expected nil for nonexistent key")
	}
}

func TestGetKeyExpired(t *testing.T) {
	SetKey("repo-exp", "user@test.com", []byte("key"), []byte("iv"), 1)

	// Manually expire
	val, _ := keys.Load(cacheKey("repo-exp", "user@test.com"))
	val.(*DecryptKey).ExpireTime = time.Now().Unix() - 1

	dk := GetKey("repo-exp", "user@test.com")
	if dk != nil {
		t.Fatal("expected nil for expired key")
	}

	// Should be cleaned up
	if _, ok := keys.Load(cacheKey("repo-exp", "user@test.com")); ok {
		t.Fatal("expired key should have been deleted from map")
	}
}

func TestDeleteKey(t *testing.T) {
	SetKey("repo-del", "user@test.com", []byte("key"), []byte("iv"), 1)

	DeleteKey("repo-del", "user@test.com")

	dk := GetKey("repo-del", "user@test.com")
	if dk != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestDifferentUsersIsolated(t *testing.T) {
	SetKey("repo-1", "alice@test.com", []byte("alice-key"), []byte("alice-iv"), 2)
	SetKey("repo-1", "bob@test.com", []byte("bob-key"), []byte("bob-iv"), 2)

	alice := GetKey("repo-1", "alice@test.com")
	bob := GetKey("repo-1", "bob@test.com")

	if alice == nil || bob == nil {
		t.Fatal("expected both keys to exist")
	}
	if string(alice.Key) == string(bob.Key) {
		t.Fatal("keys should be different for different users")
	}
}
