package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func TestAccessLogCorrelatesWithoutRequestSecrets(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /runs/{id}/submissions", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid request")
	})
	handler := withTraceID(withAccessLog(logger, mux))
	request := httptest.NewRequest("POST", "/runs/private-run/submissions?token=private-query", strings.NewReader(`{"password":"private-body"}`))
	request.Header.Set("Authorization", "Bearer private-credential")
	request.Header.Set("Cookie", "og_session=private-cookie")
	request.Header.Set(traceIDHeader, "private-client-trace")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["trace_id"] != response.Header().Get(traceIDHeader) || record["trace_id"] == "" || record["status"] != float64(400) || record["route"] != "POST /runs/{id}/submissions" || record["completed"] != true {
		t.Fatalf("bad access record: %v", record)
	}
	if record["response_bytes"] != float64(response.Body.Len()) {
		t.Fatalf("wrong response size: %v", record)
	}
	if strings.Contains(output.String(), "private-") {
		t.Fatal("request data leaked into logs")
	}
	var body struct {
		TraceID string `json:"traceId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.TraceID != record["trace_id"] {
		t.Fatal("CLI error cannot be correlated with server log")
	}
}

func TestStandardHandlerWiresAccessLogger(t *testing.T) {
	var output bytes.Buffer
	handler := New(checker{}, Options{Logger: slog.New(slog.NewJSONHandler(&output, nil))})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1alpha1/meta/version", nil))
	if !strings.Contains(output.String(), response.Header().Get(traceIDHeader)) || !strings.Contains(output.String(), "GET /api/v1alpha1/meta/version") {
		t.Fatal("standard handler did not log the traced route")
	}
}

func TestAccessLogPreservesResponseControllerAndPanic(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &accessResponse{ResponseWriter: recorder}
	if err := http.NewResponseController(writer).Flush(); err != nil || !recorder.Flushed || writer.status != http.StatusOK {
		t.Fatal("response controller cannot reach underlying writer")
	}
	var output bytes.Buffer
	handler := withTraceID(withAccessLog(slog.New(slog.NewJSONHandler(&output, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("test panic") })))
	func() {
		defer func() {
			if recover() != "test panic" {
				t.Fatal("middleware changed handler panic")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/private-path", nil))
	}()
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil || record["completed"] != false || record["status"] != float64(0) || record["route"] != "unmatched" {
		t.Fatalf("aborted request misreported: %v", record)
	}
}

func TestAccessLogQuietHealthSuccessButVisibleFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		var output bytes.Buffer
		mux := http.NewServeMux()
		mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		handler := withTraceID(withAccessLog(slog.New(slog.NewJSONHandler(&output, nil)), mux))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/readyz", nil))
		if status == http.StatusOK && output.Len() != 0 {
			t.Fatal("successful probes created info-level noise")
		}
		if status == http.StatusServiceUnavailable {
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil || record["status"] != float64(status) || record["level"] != "WARN" {
				t.Fatalf("health failure hidden: %v", record)
			}
		}
	}
}

func TestAccessLogActionRoutesIncludeRejectedMethods(t *testing.T) {
	for _, route := range []struct{ path, pattern string }{
		{"/api/v1alpha1/scenes/private-scene:initialize", "/api/v1alpha1/scenes/{key}:initialize"},
		{"/api/v1alpha1/policies/private-policy:archive", "/api/v1alpha1/policies/{policyId}:archive"},
		{"/api/v1alpha1/policies/private-policy:unarchive", "/api/v1alpha1/policies/{policyId}:unarchive"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
			t.Run(method+route.pattern, func(t *testing.T) {
				var output bytes.Buffer
				handler := New(checker{}, Options{
					Identity: &identity.Service{}, ModelSpecs: spec.New(&storage.Specs{}),
					Origin: "https://guardrail.example",
					Logger: slog.New(slog.NewJSONHandler(&output, nil)),
				})
				request := httptest.NewRequest(method, "https://guardrail.example"+route.path+"?token=private-query", nil)
				request.Header.Set("X-Guardrail-CSRF", "1")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				// Missing credentials must still be rejected before the action runs.
				if response.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", response.Code)
				}
				var record map[string]any
				if err := json.Unmarshal(output.Bytes(), &record); err != nil {
					t.Fatal(err)
				}
				pattern := route.pattern
				if method == http.MethodPost {
					pattern = "POST " + pattern
				}
				if record["route"] != pattern || record["method"] != method || record["status"] != float64(response.Code) {
					t.Fatalf("incorrect action record: %v", record)
				}
				if strings.Contains(output.String(), "private-") {
					t.Fatal("action path or query leaked into logs")
				}
			})
		}
	}
}
