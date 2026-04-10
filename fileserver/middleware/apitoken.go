package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/haiwen/seafile-server/fileserver/apitokenstore"
)

// RequireAPIToken validates a Seahub/DRF-style "Authorization: Token <token>"
// header against the in-memory API token store and injects the user email
// into the request context.
func RequireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "token") {
			http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}

		email := apitokenstore.Lookup(parts[1])
		if email == "" {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
