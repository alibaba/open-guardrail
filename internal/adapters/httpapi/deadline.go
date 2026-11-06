package httpapi

import (
	"context"
	"net/http"
	"time"
)

// Keep database work within the server's 15-second socket write timeout.
const requestBudget = 10 * time.Second

// Cancellation is cooperative: handlers pass this context to their repositories.
// Do not run the handler in another goroutine or race its response writer.
func withRequestDeadline(budget time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
