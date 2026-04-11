package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dkam/silo/fileserver/authmgr"
	"github.com/dkam/silo/fileserver/option"
)

func init() {
	option.JWTPrivateKey = "test-secret-key-for-unit-tests"
}

func TestRequireAuthSuccess(t *testing.T) {
	token, err := authmgr.GenerateSessionToken("alice@example.com")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	var gotEmail string
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = GetUserEmail(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/repos", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotEmail != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", gotEmail)
	}
}

func TestRequireAuthMissingHeader(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/repos", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuthInvalidFormat(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	tests := []struct {
		name   string
		header string
	}{
		{"no space", "Bearertoken"},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"empty token", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/repos", nil)
			req.Header.Set("Authorization", tt.header)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rr.Code)
			}
		})
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v1/repos", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20iLCJleHAiOjE1MDAwMDAwMDB9.invalid")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuthCaseInsensitiveBearer(t *testing.T) {
	token, err := authmgr.GenerateSessionToken("bob@example.com")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/repos", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with lowercase 'bearer', got %d", rr.Code)
	}
}

func TestGetUserEmailNoContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	email := GetUserEmail(req)
	if email != "" {
		t.Errorf("expected empty string, got %s", email)
	}
}
