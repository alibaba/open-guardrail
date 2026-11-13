package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/alibaba/open-guardrail/internal/application/capability"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func TestCapabilitiesMatchRegisteredFeatures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options Options
		want    string
	}{
		{"diagnostics", Options{}, ""},
		{"identity", Options{Identity: &identity.Service{}}, "local-administrator,project-api-key"},
		{"specs", Options{Identity: &identity.Service{}, ModelSpecs: spec.New(&storage.Specs{})}, "local-administrator,project-api-key,scene-policy-management,immutable-policy-revisions,resource-policy-bindings"},
		{"runs", Options{Identity: &identity.Service{}, ModelRuns: runapp.New(&storage.Runs{})}, "local-administrator,project-api-key,policy-revision-runs,explicit-revision-run-scope,multi-submission-decision,run-expiry-decision-validity,run-scoped-submission-token,run-diagnostic-report"},
		{"all", Options{Identity: &identity.Service{}, ModelSpecs: spec.New(&storage.Specs{}), ModelRuns: runapp.New(&storage.Runs{})}, "local-administrator,project-api-key,scene-policy-management,immutable-policy-revisions,resource-policy-bindings,policy-revision-runs,explicit-revision-run-scope,multi-submission-decision,run-expiry-decision-validity,run-scoped-submission-token,run-diagnostic-report"},
		{"audit", Options{Identity: &identity.Service{}, Audits: audit.New(&storage.AuditEvents{})}, "local-administrator,project-api-key,project-audit-query"},
		{"specs without identity", Options{ModelSpecs: spec.New(&storage.Specs{})}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := New(checker{}, tc.options)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1alpha1/meta/capabilities", nil))
			var got contract.Capabilities
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if response.Code != 200 || got.Features == nil || strings.Join(got.Features, ",") != tc.want {
				t.Fatalf("unexpected capabilities: %s", response.Body.String())
			}
			// Constructing another handler must not change this handler's features.
			New(checker{}, Options{Identity: &identity.Service{}, ModelSpecs: spec.New(&storage.Specs{})})
			again := httptest.NewRecorder()
			handler.ServeHTTP(again, httptest.NewRequest("GET", "/api/v1alpha1/meta/capabilities", nil))
			if again.Body.String() != response.Body.String() {
				t.Fatal("capabilities leaked between handlers")
			}
		})
	}
}

type checker struct {
	err     error
	storage capability.Storage
}

func (c checker) Ready(context.Context) error { return c.err }
func (c checker) StorageCapability() capability.Storage {
	if c.storage.SupportLevel != "" {
		return c.storage
	}
	return capability.Storage{SupportLevel: capability.SupportOfficialLocal, MultiInstance: false}
}
func TestHTTP(t *testing.T) {
	traceIDPattern := regexp.MustCompile(`^[a-f0-9]{32}$`)
	for _, tc := range []struct {
		path   string
		err    error
		status int
		body   string
	}{
		{"/livez", errors.New("unavailable"), 200, "alive"},
		{"/healthz", errors.New("unavailable"), 404, "NOT_FOUND"},
		{"/readyz", nil, 200, "ready"},
		{"/readyz", errors.New("secret database details"), 503, "NOT_READY"},
		{"/api/v1alpha1/meta/version", nil, 200, "version"},
		{"/api/v1alpha1/meta/capabilities", nil, 200, "v1alpha1"},
		{"/", nil, 200, "OpenGuardrail"},
		{"/runs.js", nil, 200, "Run history"},
		{"/select-arrow.svg", nil, 200, "stroke=\"#5b625e\""},
		{"/favicon.svg", nil, 200, "fill=\"#16654d\""},
		{"/openapi.yaml", nil, 200, "openapi: 3.0.3"},
		{"/not-found", nil, 404, "NOT_FOUND"},
	} {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			w := httptest.NewRecorder()
			New(checker{err: tc.err}).ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret database details") {
				t.Fatal("error leaked")
			}
			if w.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing CSP")
			}
			if traceID := w.Header().Get(traceIDHeader); !traceIDPattern.MatchString(traceID) {
				t.Fatalf("invalid trace ID: %q", traceID)
			}
		})
	}
}

func TestTraceIDIsServerGeneratedAndMatchesError(t *testing.T) {
	request := httptest.NewRequest("GET", "/not-found", nil)
	request.Header.Set(traceIDHeader, "attacker-controlled\nvalue")
	response := httptest.NewRecorder()
	New(checker{}).ServeHTTP(response, request)
	var body contract.Error
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	traceID := response.Header().Get(traceIDHeader)
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(traceID) || body.TraceId != traceID {
		t.Fatalf("trace mismatch: header=%q body=%q", traceID, body.TraceId)
	}
}

func TestGeneratedClientMatchesServerContract(t *testing.T) {
	service := httptest.NewServer(New(checker{}))
	defer service.Close()
	client, err := contract.NewClientWithResponses(service.URL)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.GetCapabilitiesWithResponse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode() != 200 || response.JSON200 == nil {
		t.Fatalf("unexpected generated client response: %d %s", response.StatusCode(), response.Body)
	}
	if len(response.JSON200.ApiVersions) != 1 || response.JSON200.ApiVersions[0] != "v1alpha1" {
		t.Fatalf("unexpected capabilities: %+v", response.JSON200)
	}
	if traceID := response.HTTPResponse.Header.Get(traceIDHeader); !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(traceID) {
		t.Fatalf("invalid trace ID: %q", traceID)
	}
}

func TestPublicMetadataDoesNotExposeInternalDetails(t *testing.T) {
	handler := New(checker{})
	for _, path := range []string{"/api/v1alpha1/meta/version", "/api/v1alpha1/meta/capabilities"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		body := response.Body.String()
		for _, forbidden := range []string{"commit", "unknown", "database", "postgres", "sqlite"} {
			if strings.Contains(strings.ToLower(body), forbidden) {
				t.Fatalf("%s exposed %q: %s", path, forbidden, body)
			}
		}
	}
}

func TestCapabilitiesDescribeOperatingEnvelope(t *testing.T) {
	for _, expected := range []capability.Storage{
		{SupportLevel: capability.SupportOfficialLocal, MultiInstance: false},
		{SupportLevel: capability.SupportOfficialShared, MultiInstance: false},
	} {
		response := httptest.NewRecorder()
		New(checker{storage: expected}).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1alpha1/meta/capabilities", nil))
		var capabilities contract.Capabilities
		if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
			t.Fatal(err)
		}
		if string(capabilities.Storage.SupportLevel) != string(expected.SupportLevel) || capabilities.Storage.MultiInstance != expected.MultiInstance {
			t.Fatalf("unexpected storage capabilities: %+v", capabilities.Storage)
		}
	}
}

func TestCapabilitiesRejectUnknownStorageSupportLevel(t *testing.T) {
	response := httptest.NewRecorder()
	New(checker{storage: capability.Storage{
		SupportLevel:  capability.SupportLevel("future-level"),
		MultiInstance: true,
	}}).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1alpha1/meta/capabilities", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	var body contract.Error
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "CAPABILITY_UNAVAILABLE" || strings.Contains(response.Body.String(), "future-level") {
		t.Fatalf("unexpected error response: %s", response.Body.String())
	}
}

func TestIdentityRoutesRemainHiddenWhenIdentityIsNotConfigured(t *testing.T) {
	handler := New(checker{})
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1alpha1/bootstrap", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1alpha1/bootstrap", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1alpha1/auth/future", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodGet, "/runtime/v1alpha1/identity", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1alpha1/projects", nil),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s: got %d want %d: %s", request.Method, request.URL.Path, response.Code, http.StatusNotFound, response.Body.String())
		}
	}
}

// Check the actual HTTP routes, not just whether assets are embedded.
func TestPageAssets(t *testing.T) {
	handler := New(checker{})
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	assets := regexp.MustCompile(`(?:src|href)="(/[^"]+\.(?:js|css))"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(assets) == 0 {
		t.Fatal("no page assets found")
	}
	for _, asset := range assets {
		t.Run(asset[1], func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", asset[1], nil))
			if response.Code != 200 || response.Body.Len() == 0 {
				t.Fatalf("asset unavailable: %d %s", response.Code, response.Body.String())
			}
			contentType := response.Header().Get("Content-Type")
			if strings.HasSuffix(asset[1], ".js") && !strings.Contains(contentType, "javascript") {
				t.Fatalf("script has invalid content type: %s", contentType)
			}
			if strings.HasSuffix(asset[1], ".css") && !strings.Contains(contentType, "text/css") {
				t.Fatalf("stylesheet has invalid content type: %s", contentType)
			}
		})
	}
}
