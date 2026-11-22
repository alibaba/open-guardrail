package httpapi

import (
	"errors"
	"net/http"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/audit"
)

func (s *server) ListAuditEvents(w http.ResponseWriter, r *http.Request, params contract.ListAuditEventsParams) {
	if s.options.Audits == nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	if !s.guard(w, r) {
		return
	}
	q := audit.Query{Limit: 20}
	if params.Limit != nil {
		q.Limit = *params.Limit
	}
	if params.EventType != nil {
		q.EventType = *params.EventType
	}
	if params.ActorId != nil {
		q.ActorID = *params.ActorId
	}
	if params.ResourceType != nil {
		q.ResourceType = *params.ResourceType
	}
	if params.ResourceId != nil {
		q.ResourceID = *params.ResourceId
	}
	if params.Cursor != nil {
		q.Cursor = *params.Cursor
	}
	if params.Redacted != nil {
		q.Redacted = *params.Redacted
	}
	q.From = params.From
	q.To = params.To
	page, err := s.options.Audits.List(r.Context(), audit.Scope{WorkspaceID: "default", ProjectID: "default"}, q)
	if err != nil {
		if errors.Is(err, audit.ErrInvalid) {
			writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid audit query")
		} else {
			writeError(w, r, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "Audit query unavailable")
		}
		return
	}
	writeJSON(w, http.StatusOK, page)
}
