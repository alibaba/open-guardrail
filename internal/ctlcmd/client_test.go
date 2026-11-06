package ctlcmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/buildinfo"
)

func TestCompatibilityWindow(t *testing.T) {
	originalVersion := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = originalVersion })

	for _, tc := range []struct {
		name        string
		client      string
		apiVersions string
		minimum     string
		maximum     string
		wantCode    int
	}{
		{"development build checks API only", "dev", `"v1alpha1"`, "0.1.0-alpha.0", "0.2.0", exitSuccess},
		{"compatible release", "0.1.0", `"v1alpha1"`, "0.1.0-alpha.0", "0.2.0", exitSuccess},
		{"unsupported API", "dev", `"v2"`, "0.1.0-alpha.0", "0.2.0", exitCompatibility},
		{"client too old", "0.0.9", `"v1alpha1"`, "0.1.0-alpha.0", "0.2.0", exitCompatibility},
		{"client too new", "0.2.0", `"v1alpha1"`, "0.1.0-alpha.0", "0.2.0", exitCompatibility},
		{"invalid server range", "0.1.0", `"v1alpha1"`, "invalid", "0.2.0", exitCompatibility},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"apiVersions":[%s],"cli":{"minimumVersion":%q,"maximumVersionExclusive":%q},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`,
					tc.apiVersions, tc.minimum, tc.maximum)
			}))
			defer server.Close()
			buildinfo.Version = tc.client
			client, err := newAPIClient(server.URL, "", time.Second, identityResponseLimit, true)
			if err != nil {
				t.Fatal(err)
			}
			err = client.ensureCompatible(context.Background())
			if got := commandExitCode(err); got != tc.wantCode {
				t.Fatalf("got exit %d want %d: %v", got, tc.wantCode, err)
			}
		})
	}
}

func TestGeneratedClientBoundsResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"version":%q}`, strings.Repeat("x", identityResponseLimit))
	}))
	defer server.Close()
	client, err := newAPIClient(server.URL, "", time.Second, identityResponseLimit, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.generated.GetVersionWithResponse(context.Background()); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestGeneratedClientPreservesServerBasePath(t *testing.T) {
	for _, basePath := range []string{"/guardrail", "/guardrail/"} {
		t.Run(basePath, func(t *testing.T) {
			var requestedPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestedPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"version":"dev"}`)
			}))
			defer server.Close()
			client, err := newAPIClient(server.URL+basePath, "", time.Second, identityResponseLimit, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.generated.GetVersionWithResponse(context.Background()); err != nil {
				t.Fatal(err)
			}
			if requestedPath != "/guardrail/api/v1alpha1/meta/version" {
				t.Fatalf("base path was not preserved: %q", requestedPath)
			}
		})
	}
}

func TestCompatibilityNegotiationDoesNotReceiveCredential(t *testing.T) {
	var capabilityAuthorization, identityAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1alpha1/meta/capabilities":
			capabilityAuthorization = r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`)
		case "/api/v1alpha1/auth/me":
			identityAuthorization = r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{"kind":"USER","projectId":"project","role":"ADMIN","workspaceId":"workspace"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := newIdentityClient(server.URL, "ogs_synthetic")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.generated.GetCurrentAdministratorWithResponse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = validateResponse(result, identityResponseLimit, "identity request", false); err != nil {
		t.Fatal(err)
	}
	if capabilityAuthorization != "" || identityAuthorization != "Bearer ogs_synthetic" {
		t.Fatalf("credential scope mismatch: capabilities=%q identity=%q", capabilityAuthorization, identityAuthorization)
	}
}

func TestResponseErrorsExposeOnlyCodeAndTrace(t *testing.T) {
	result := &contract.GetCapabilitiesResponse{
		Body: []byte(`{"code":"UNAUTHENTICATED","message":"sensitive detail","traceId":"trace-123"}`),
		HTTPResponse: &http.Response{
			StatusCode: http.StatusUnauthorized,
		},
	}
	err := validateResponse(result, identityResponseLimit, "capabilities request", false)
	if commandExitCode(err) != exitAuthentication || strings.Contains(err.Error(), "sensitive detail") ||
		!strings.Contains(err.Error(), "UNAUTHENTICATED") || !strings.Contains(err.Error(), "trace-123") {
		t.Fatalf("unsafe or incorrectly classified error: %v", err)
	}
}

func TestStableExitCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{nil, exitSuccess},
		{usageError("usage"), exitUsage},
		{&commandError{kind: exitAuthentication, message: "auth"}, exitAuthentication},
		{&commandError{kind: exitConnection, message: "connection"}, exitConnection},
		{&commandError{kind: exitServerRejected, message: "rejected"}, exitServerRejected},
		{compatibilityError("compatibility"), exitCompatibility},
	} {
		if got := commandExitCode(tc.err); got != tc.want {
			t.Fatalf("got %d want %d for %v", got, tc.want, tc.err)
		}
	}
}
