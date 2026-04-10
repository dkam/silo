package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/haiwen/seafile-server/fileserver/apitokenstore"
	log "github.com/sirupsen/logrus"
)

// RequireAPIToken validates a Seahub/DRF-style "Authorization: Token <token>"
// header and injects the user email into the request context.
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

		email, err := apitokenstore.Lookup(parts[1])
		if errors.Is(err, apitokenstore.ErrNotFound) {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}
		if err != nil {
			log.Errorf("API token lookup failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(r.Context(), UserEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
