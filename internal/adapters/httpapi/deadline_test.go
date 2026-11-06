package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDeadlineCancelsWorkAndPreservesEarlierDeadline(t *testing.T) {
	for _, parentEarlier := range []bool{false, true} {
		ctx := context.Background()
		budget := 10 * time.Millisecond
		if parentEarlier {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
			defer cancel()
			budget = time.Hour
		}
		handler := withRequestDeadline(budget, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
				if !errors.Is(r.Context().Err(), context.DeadlineExceeded) {
					t.Error("wrong cancellation cause")
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			case <-time.After(time.Second):
				t.Error("request work did not receive the deadline")
			}
		}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatal("timed-out work did not return")
		}
	}
}
func TestRequestDeadlineReleasesContextAfterSuccess(t *testing.T) {
	var requestContext context.Context
	handler := withRequestDeadline(time.Hour, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requestContext = r.Context(); w.WriteHeader(204) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !errors.Is(requestContext.Err(), context.Canceled) {
		t.Fatal("completed request retained its timer/context")
	}
}
