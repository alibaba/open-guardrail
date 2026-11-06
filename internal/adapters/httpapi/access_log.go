package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

type accessResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *accessResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *accessResponse) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err == nil && w.status == 0 {
		w.status = http.StatusOK
	}
	return err
}
func (w *accessResponse) WriteHeader(status int) {
	if w.status == 0 && (status >= 200 || status == http.StatusSwitchingProtocols) {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *accessResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
	return n, err
}

// withAccessLog runs inside withTraceID. Only router-owned templates are logged;
// raw URLs, identifiers, queries, headers, bodies and credentials stay private.
func withAccessLog(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &accessResponse{ResponseWriter: w}
		completed := false
		defer func() {
			status := response.status
			if completed && status == 0 {
				status = http.StatusOK
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			method := r.Method
			switch method {
			case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
			default:
				method = "OTHER"
			}
			level := slog.LevelInfo
			if route == "GET /livez" || route == "GET /readyz" {
				level = slog.LevelDebug
			}
			if !completed || status >= 500 {
				level = slog.LevelWarn
			}
			logger.Log(r.Context(), level, "http request",
				"trace_id", requestTraceID(r), "method", method, "route", route,
				"status", status, "response_bytes", response.bytes,
				"duration_ms", time.Since(started).Milliseconds(), "completed", completed)
		}()
		next.ServeHTTP(response, r)
		completed = true
	})
}
