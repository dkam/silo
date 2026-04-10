package middleware

import (
	"fmt"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// DebugLogger logs every HTTP request with method, path, status, duration, and
// remote address. 404s are logged at WARN level so they stand out.
func DebugLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		duration := time.Since(start)

		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path = path + "?" + r.URL.RawQuery
		}
		msg := fmt.Sprintf("HTTP %s %s -> %d (%s) from %s",
			r.Method, path, rec.status, duration, r.RemoteAddr)

		if rec.status == http.StatusNotFound {
			log.Warn(msg)
		} else {
			log.Info(msg)
		}
	})
}
