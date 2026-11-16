package httpapi

import (
	"net/http"

	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
)

func (s *server) GetManagedRunReport(w http.ResponseWriter, r *http.Request, id string) {
	scope, ok := s.managedRunScope(w, r)
	if !ok {
		return
	}
	s.writeRunReport(w, r, scope, id)
}
func (s *server) GetRunReport(w http.ResponseWriter, r *http.Request, id string) {
	scope, ok := s.runScope(w, r, identity.ScopeRunsRead)
	if !ok {
		return
	}
	s.writeRunReport(w, r, scope, id)
}
func (s *server) writeRunReport(w http.ResponseWriter, r *http.Request, scope runapp.Scope, id string) {
	if !modelID.MatchString(id) {
		s.modelRunError(w, r, runapp.ErrInvalid)
		return
	}
	report, err := s.options.ModelRuns.Report(r.Context(), scope, id)
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
