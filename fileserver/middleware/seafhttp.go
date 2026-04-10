package middleware

import (
	"net/http"
	"strings"
)

// StripSeafhttpPrefix rewrites incoming paths that start with "/seafhttp/"
// to their bare form (e.g. "/seafhttp/repo/xxx/permission-check" ->
// "/repo/xxx/permission-check"). This lets SeaDrive and the Seafile CLI
// clients — which expect the sync endpoints under /seafhttp/ as they are
// in nginx-reverse-proxied deployments — work against the standalone
// Go fileserver without adding duplicate routes.
func StripSeafhttpPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/seafhttp/") {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/seafhttp")
		} else if r.URL.Path == "/seafhttp" {
			r.URL.Path = "/"
		}
		next.ServeHTTP(w, r)
	})
}
