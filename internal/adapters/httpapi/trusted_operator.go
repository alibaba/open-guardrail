package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"

	"github.com/alibaba/open-guardrail/internal/application/spec"
)

var operatorIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`)

// This API-only mode explicitly trusts the caller's operator. No provider,
// credential verification or local user record is involved.
func (s *server) trustedOperatorScope(w http.ResponseWriter, r *http.Request) (spec.Scope, bool) {
	reject := func() (spec.Scope, bool) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "A single operator identifier is required")
		return spec.Scope{}, false
	}
	values := r.Header.Values("X-Operator")
	if len(values) > 1 || (len(values) == 1 && values[0] == "") {
		return reject()
	}
	operator := ""
	if len(values) == 1 {
		operator = values[0]
	}
	if r.Body != nil && r.ContentLength != 0 && r.Method != http.MethodGet && r.Method != http.MethodHead {
		var fields map[string]json.RawMessage
		if !decodeJSON(w, r, &fields, 1<<20) {
			writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid JSON request")
			return spec.Scope{}, false
		}
		if raw, exists := fields["operator"]; exists {
			var bodyOperator string
			if json.Unmarshal(raw, &bodyOperator) != nil || bodyOperator == "" || (operator != "" && operator != bodyOperator) {
				return reject()
			}
			operator = bodyOperator
			delete(fields, "operator")
		}
		body, err := json.Marshal(fields)
		if err != nil {
			return reject()
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	if !operatorIdentifier.MatchString(operator) {
		return reject()
	}
	return spec.Scope{WorkspaceID: "default", ProjectID: "default",
		ActorID: s.options.TrustedOperatorNamespace + ":" + operator, Operator: operator}, true
}
