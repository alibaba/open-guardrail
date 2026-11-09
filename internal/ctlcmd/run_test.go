package ctlcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunListRejectsInvalidLocalFilters(t *testing.T) {
	for _, args := range [][]string{
		{"run", "list", "--key-file", "unused", "--limit", "0"},
		{"run", "list", "--key-file", "unused", "--limit", "101"},
		{"run", "list", "--key-file", "unused", "--status", "DONE"},
	} {
		if err := run(args, &bytes.Buffer{}); err == nil || commandExitCode(err) != exitUsage {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

func TestRunReadCommandsUseGeneratedContract(t *testing.T) {
	tests := []struct {
		name, command, path, response string
		extra                         []string
		query                         map[string]string
	}{
		{name: "get", command: "get", path: "/runtime/v1alpha1/runs/run_1", response: `{"id":"run_1"}`},
		{name: "revision", command: "revision", path: "/runtime/v1alpha1/runs/run_1/policy-revision", response: `{"id":"rev_1","digest":"sha256:test"}`},
		{name: "submission", command: "submission", path: "/runtime/v1alpha1/runs/run_1/submissions/sub_1",
			response: `{"id":"sub_1","runId":"run_1"}`, extra: []string{"--submission-id", "sub_1"}},
		{name: "list", command: "list", path: "/runtime/v1alpha1/runs", response: `{"items":[]}`,
			extra: []string{"--status", "OPEN", "--scene", "recovery", "--limit", "7", "--cursor", "cursor_1"},
			query: map[string]string{"status": "OPEN", "sceneKey": "recovery", "limit": "7", "cursor": "cursor_1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var method, path, authorization string
			query := map[string]string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
					_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`)
					return
				}
				method, path, authorization = r.Method, r.URL.Path, r.Header.Get("Authorization")
				for key, values := range r.URL.Query() {
					if len(values) != 0 {
						query[key] = values[0]
					}
				}
				_, _ = fmt.Fprint(w, tc.response)
			}))
			defer server.Close()

			dir := t.TempDir()
			keyPath := filepath.Join(dir, "project-key.json")
			keyData, _ := json.Marshal(credentialFile{Server: server.URL, Token: "ogk_synthetic"})
			if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"run", tc.command, "--server", server.URL, "--key-file", keyPath}
			args = append(args, tc.extra...)
			if tc.command != "list" {
				args = append(args, "run_1")
			}
			if err := run(args, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if method != http.MethodGet || path != tc.path || authorization != "Bearer ogk_synthetic" {
				t.Fatalf("request method=%q path=%q authorization=%q", method, path, authorization)
			}
			if len(query) != len(tc.query) {
				t.Fatalf("query=%v want=%v", query, tc.query)
			}
			for key, value := range tc.query {
				if query[key] != value {
					t.Fatalf("query[%q]=%q want=%q", key, query[key], value)
				}
			}
		})
	}
}

func TestRunCreatePreservesGeneratedOperationIDForUncertainResponses(t *testing.T) {
	responses := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "gateway error", status: http.StatusBadGateway, contentType: "application/json", body: `{"code":"UPSTREAM","message":"temporary"}`},
		{name: "malformed JSON", status: http.StatusCreated, contentType: "application/json", body: `{"id":`},
		{name: "unexpected success status", status: http.StatusNoContent},
		{name: "incomplete success", status: http.StatusCreated, contentType: "application/json", body: `{}`},
		{name: "oversized non JSON response", status: http.StatusOK, contentType: "text/plain", body: strings.Repeat("x", runResponseLimit+1)},
	}
	for _, tc := range responses {
		t.Run(tc.name, func(t *testing.T) {
			var requestedMethod, requestedPath, requestedAuthorization, requestedContentType, requestedOperationID string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
					_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`)
					return
				}
				requestedMethod, requestedPath = r.Method, r.URL.Path
				requestedAuthorization = r.Header.Get("Authorization")
				requestedContentType = r.Header.Get("Content-Type")
				requestedOperationID = r.Header.Get("Idempotency-Key")
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			dir := t.TempDir()
			keyPath := filepath.Join(dir, "project-key.json")
			keyData, _ := json.Marshal(credentialFile{Server: server.URL, Token: "ogk_synthetic"})
			if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
				t.Fatal(err)
			}
			changeSetPath := filepath.Join(dir, "change-set.json")
			if err := os.WriteFile(changeSetPath, []byte(`{"target":"app-a"}`), 0600); err != nil {
				t.Fatal(err)
			}

			tokenPath := filepath.Join(dir, "run-token.json")
			err := run([]string{"run", "create", "--server", server.URL, "--key-file", keyPath, "--scene", "recovery",
				"--change-set", changeSetPath, "--submission-credential-file", tokenPath}, &bytes.Buffer{})
			if err == nil || requestedOperationID == "" || !strings.Contains(err.Error(), "retry with --operation-id "+requestedOperationID) {
				t.Fatalf("operation ID not preserved: requested=%q err=%v", requestedOperationID, err)
			}
			if requestedMethod != http.MethodPost || requestedPath != "/runtime/v1alpha1/runs" ||
				requestedAuthorization != "Bearer ogk_synthetic" || requestedContentType != "application/json" {
				t.Fatalf("request method=%q path=%q authorization=%q contentType=%q", requestedMethod, requestedPath,
					requestedAuthorization, requestedContentType)
			}
		})
	}
}

func TestRunMutationsUseGeneratedContract(t *testing.T) {
	t.Run("submit", func(t *testing.T) {
		var method, path, authorization, operationID, contentType string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
				_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`)
				return
			}
			method, path = r.Method, r.URL.Path
			authorization = r.Header.Get("Authorization")
			operationID = r.Header.Get("Idempotency-Key")
			contentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"id":"sub_1","runId":"run_1"}`)
		}))
		defer server.Close()

		dir := t.TempDir()
		credentialPath := filepath.Join(dir, "submission-credential.json")
		credential, _ := json.Marshal(credentialFile{Server: server.URL, RunID: "run_1", Token: "ogrt_synthetic"})
		if err := os.WriteFile(credentialPath, credential, 0600); err != nil {
			t.Fatal(err)
		}
		submissionPath := filepath.Join(dir, "submission.json")
		if err := os.WriteFile(submissionPath, []byte(`{"baseSubmissionNo":0,"checks":[],"finishRun":false,"summary":"pending","verdict":"UNKNOWN"}`), 0600); err != nil {
			t.Fatal(err)
		}
		err := run([]string{"run", "submit", "--server", server.URL, "--submission-credential-file", credentialPath,
			"--submission", submissionPath, "--operation-id", "op_test", "run_1"}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		if method != http.MethodPost || path != "/runtime/v1alpha1/runs/run_1/submissions" ||
			authorization != "Bearer ogrt_synthetic" || operationID != "op_test" || contentType != "application/json" {
			t.Fatalf("request method=%q path=%q authorization=%q operation=%q contentType=%q",
				method, path, authorization, operationID, contentType)
		}
	})

	t.Run("rotate credential", func(t *testing.T) {
		var method, path, authorization string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
				_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`)
				return
			}
			method, path, authorization = r.Method, r.URL.Path, r.Header.Get("Authorization")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"runId":"run_1","token":"ogrt_replacement","expiresAt":123}`)
		}))
		defer server.Close()

		dir := t.TempDir()
		keyPath := filepath.Join(dir, "project-key.json")
		keyData, _ := json.Marshal(credentialFile{Server: server.URL, Token: "ogk_synthetic"})
		if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
			t.Fatal(err)
		}
		credentialPath := filepath.Join(dir, "replacement.json")
		err := run([]string{"run", "rotate-credential", "--server", server.URL, "--key-file", keyPath,
			"--submission-credential-file", credentialPath, "run_1"}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		if method != http.MethodPost || path != "/runtime/v1alpha1/runs/run_1/submission-credential:rotate" ||
			authorization != "Bearer ogk_synthetic" {
			t.Fatalf("request method=%q path=%q authorization=%q", method, path, authorization)
		}
		var saved credentialFile
		data, err := os.ReadFile(credentialPath)
		if err != nil || json.Unmarshal(data, &saved) != nil || saved.Token != "ogrt_replacement" || saved.RunID != "run_1" {
			t.Fatalf("saved credential=%+v err=%v", saved, err)
		}
	})
}

const testCapabilitiesResponse = `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`

func writeTestProjectKey(t *testing.T, serverURL string) string {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "project-key.json")
	keyData, _ := json.Marshal(credentialFile{Server: serverURL, Token: "ogk_synthetic"})
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		t.Fatal(err)
	}
	return keyPath
}

func writeTestRun(w http.ResponseWriter, status string) {
	_, _ = fmt.Fprintf(w, `{"id":"run_1","status":%q,"deadlineAt":%d,"result":"PASS","decision":"ALLOW"}`,
		status, time.Now().Add(time.Hour).Unix())
}

func TestRunWaitStopsAtFinished(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		calls++
		if calls == 1 {
			writeTestRun(w, "OPEN")
			return
		}
		writeTestRun(w, "FINISHED")
	}))
	defer server.Close()

	var out bytes.Buffer
	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--interval", "1ms", "run_1"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("Run lookups=%d want 2", calls)
	}
	if !strings.Contains(out.String(), `"status":"FINISHED"`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunWaitReportsExpired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		writeTestRun(w, "EXPIRED")
	}))
	defer server.Close()

	var out bytes.Buffer
	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--interval", "1ms", "run_1"}, &out)
	if err == nil || commandExitCode(err) != exitGeneral || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out.String(), `"status":"EXPIRED"`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunWaitTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		writeTestRun(w, "OPEN")
	}))
	defer server.Close()

	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--timeout", "20ms", "--interval", "5ms", "run_1"}, &bytes.Buffer{})
	if err == nil || commandExitCode(err) != exitGeneral || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunWaitDoesNotRetryAuthenticationFailure(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"code":"UNAUTHENTICATED","traceId":"trace_1"}`)
	}))
	defer server.Close()

	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--interval", "1ms", "run_1"}, &bytes.Buffer{})
	if err == nil || commandExitCode(err) != exitAuthentication {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("Run lookups=%d want 1", calls)
	}
}

func TestRunWaitRetriesTransientServerError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, `{"code":"UNAVAILABLE","traceId":"trace_1"}`)
			return
		}
		writeTestRun(w, "FINISHED")
	}))
	defer server.Close()

	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--interval", "1ms", "run_1"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("Run lookups=%d want 2", calls)
	}
}

func TestRunWaitRejectsInvalidFlags(t *testing.T) {
	for _, args := range [][]string{
		{"run", "wait", "--key-file", "unused", "--timeout", "-1s", "run_1"},
		{"run", "wait", "--key-file", "unused", "--interval", "0s", "run_1"},
		{"run", "wait", "--key-file", "unused"},
	} {
		if err := run(args, &bytes.Buffer{}); err == nil || commandExitCode(err) != exitUsage {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

func TestRunWaitStopsAtRunDeadlineEvenWithLongerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		// A stale OPEN response whose deadline already passed and whose later
		// reads fail must not keep the client waiting for the longer --timeout.
		_, _ = fmt.Fprintf(w, `{"id":"run_1","status":"OPEN","deadlineAt":%d}`, time.Now().Add(-time.Minute).Unix())
	}))
	defer server.Close()

	started := time.Now()
	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--timeout", "10s", "--interval", "1ms", "run_1"}, &bytes.Buffer{})
	if err == nil || commandExitCode(err) != exitGeneral || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("waited %s past the Run deadline", elapsed)
	}
}

func TestRunWaitStopsAfterContactTimeout(t *testing.T) {
	previous := waitContactTimeout
	waitContactTimeout = 50 * time.Millisecond
	t.Cleanup(func() { waitContactTimeout = previous })

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"code":"UNAVAILABLE","traceId":"trace_1"}`)
	}))
	defer server.Close()

	started := time.Now()
	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--interval", "1ms", "run_1"}, &bytes.Buffer{})
	if err == nil || commandExitCode(err) != exitServerRejected {
		t.Fatalf("err=%v", err)
	}
	if calls < 2 {
		t.Fatalf("Run lookups=%d want retries before giving up", calls)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("contact retries took %s", elapsed)
	}
}

func TestRunWaitRetriesTooManyRequests(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"code":"RATE_LIMITED","traceId":"trace_1"}`)
			return
		}
		writeTestRun(w, "FINISHED")
	}))
	defer server.Close()

	if err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"--interval", "1ms", "run_1"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("Run lookups=%d want 2", calls)
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{header: "", want: 0},
		{header: "5", want: 5 * time.Second},
		{header: " 2 ", want: 2 * time.Second},
		{header: "0", want: 0},
		{header: "-1", want: 0},
		{header: "invalid", want: 0},
		{header: "Wed, 21 Oct 2015 07:28:00 GMT", want: 0},
	}
	for _, tc := range tests {
		response := &http.Response{Header: http.Header{}}
		response.Header.Set("Retry-After", tc.header)
		if got := retryAfter(response); got != tc.want {
			t.Fatalf("Retry-After %q=%s want=%s", tc.header, got, tc.want)
		}
	}
	if got := retryAfter(nil); got != 0 {
		t.Fatalf("nil response=%s want=0", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunWaitPropagatesExpiredOutputError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, testCapabilitiesResponse)
			return
		}
		writeTestRun(w, "EXPIRED")
	}))
	defer server.Close()

	err := run([]string{"run", "wait", "--server", server.URL, "--key-file", writeTestProjectKey(t, server.URL),
		"run_1"}, failingWriter{})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("err=%v", err)
	}
}

const cliSpec = `apiVersion: guardrail.open/v1alpha1
kind: GuardrailSpec
metadata:
  name: recovery
spec:
  description: Check recovery readiness
  instructions: Inspect current state and provide evidence
  validForSeconds: 900
  checks:
    - id: readiness
      displayName: Recovery readiness
      description: Check recovery readiness
      instructions: Inspect current health and provide evidence
      criteria:
        pass: Health is good
        block: Health is bad
        unknown: Health is not verified
      evidenceRequirements:
        - id: health
          description: Record current health
          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]
`
