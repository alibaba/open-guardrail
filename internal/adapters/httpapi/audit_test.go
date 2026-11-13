package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/alibaba/open-guardrail/internal/application/identity"
)

type auditSessionRepository struct {
	identity.Repository
	principal identity.Principal
}

func (r auditSessionRepository) Authenticate(context.Context, string, string, func() int64) (identity.Principal, error) {
	return r.principal, nil
}

func TestAuditSessionAuthorizationErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal identity.Principal
		status    int
		code      string
	}{
		{"authenticated non administrator", identity.Principal{Role: "MEMBER", User: &identity.User{ID: "member"}}, http.StatusForbidden, "PERMISSION_DENIED"},
		{"missing session user", identity.Principal{Role: "ADMIN"}, http.StatusUnauthorized, "UNAUTHENTICATED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ids := identity.New(auditSessionRepository{principal: test.principal}, nil)
			handler := New(checker{}, Options{Identity: ids, Audits: audit.New(nil), Origin: "https://guardrail.example"})
			request := httptest.NewRequest(http.MethodGet, "https://guardrail.example/api/v1alpha1/audit-events", nil)
			request.Header.Set("Authorization", "Bearer ogs_synthetic_session")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
