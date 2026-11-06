package httpapi

import (
	"net/http"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
)

// These adapters implement the generated OpenAPI interface. Routing and required
// parameter validation are owned by the generated router.
func (s *server) GetPolicy(w http.ResponseWriter, r *http.Request, policyId string) {
	s.modelPolicyRoute(w, r)
}
func (s *server) ActivatePolicyRevision(w http.ResponseWriter, r *http.Request, policyId string, params contract.ActivatePolicyRevisionParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) GetPolicyDraft(w http.ResponseWriter, r *http.Request, policyId string) {
	s.modelPolicyRoute(w, r)
}
func (s *server) SavePolicyDraft(w http.ResponseWriter, r *http.Request, policyId string, params contract.SavePolicyDraftParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) PublishPolicyDraft(w http.ResponseWriter, r *http.Request, policyId string, params contract.PublishPolicyDraftParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) ListPolicyRevisions(w http.ResponseWriter, r *http.Request, policyId string, params contract.ListPolicyRevisionsParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) PublishPolicyRevision(w http.ResponseWriter, r *http.Request, policyId string, params contract.PublishPolicyRevisionParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) ArchivePolicy(w http.ResponseWriter, r *http.Request, policyId string, params contract.ArchivePolicyParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) UnarchivePolicy(w http.ResponseWriter, r *http.Request, policyId string, params contract.UnarchivePolicyParams) {
	s.modelPolicyRoute(w, r)
}
func (s *server) GetPolicyRevision(w http.ResponseWriter, r *http.Request, revisionId string) {
	s.modelRevisionRoute(w, r, revisionId)
}
func (s *server) ListManagedPolicyRuns(w http.ResponseWriter, r *http.Request, params contract.ListManagedPolicyRunsParams) {
	s.modelManagedRunRoute(w, r)
}
func (s *server) GetManagedPolicyRun(w http.ResponseWriter, r *http.Request, runId string) {
	s.modelManagedRunRoute(w, r)
}
func (s *server) GetManagedRunSubmission(w http.ResponseWriter, r *http.Request, runId, submissionId string) {
	scope, ok := s.managedRunScope(w, r)
	if !ok {
		return
	}
	if !modelID.MatchString(runId) || !modelID.MatchString(submissionId) {
		s.modelRunError(w, r, runapp.ErrNotFound)
		return
	}
	submission, err := s.options.ModelRuns.GetRunSubmission(r.Context(), scope, runId, submissionId)
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, submission)
}
func (s *server) ListManagedRunSubmissions(w http.ResponseWriter, r *http.Request, runId string, params contract.ListManagedRunSubmissionsParams) {
	scope, ok := s.managedRunScope(w, r)
	if !ok {
		return
	}
	if !modelID.MatchString(runId) {
		s.modelRunError(w, r, runapp.ErrNotFound)
		return
	}
	limit, before := 20, int64(0)
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Before != nil {
		before = *params.Before
		if before < 1 {
			s.modelRunError(w, r, runapp.ErrInvalid)
			return
		}
	}
	page, err := s.options.ModelRuns.ListRunSubmissions(r.Context(), scope, runId, limit, before)
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *server) ListSceneModels(w http.ResponseWriter, r *http.Request) {
	s.modelSceneRoute(w, r)
}
func (s *server) GetSceneModel(w http.ResponseWriter, r *http.Request, key string) {
	s.modelSceneRoute(w, r)
}
func (s *server) SetSceneTTL(w http.ResponseWriter, r *http.Request, key string, params contract.SetSceneTTLParams) {
	s.modelSceneRoute(w, r)
}
func (s *server) SetDefaultPolicy(w http.ResponseWriter, r *http.Request, key string, params contract.SetDefaultPolicyParams) {
	s.modelSceneRoute(w, r)
}
func (s *server) ListPolicies(w http.ResponseWriter, r *http.Request, key string) {
	s.modelSceneRoute(w, r)
}
func (s *server) CreatePolicy(w http.ResponseWriter, r *http.Request, key string, params contract.CreatePolicyParams) {
	s.modelSceneRoute(w, r)
}
func (s *server) InitializeScene(w http.ResponseWriter, r *http.Request, key string, params contract.InitializeSceneParams) {
	s.modelSceneRoute(w, r)
}
func (s *server) ListPolicyRuns(w http.ResponseWriter, r *http.Request, params contract.ListPolicyRunsParams) {
	s.modelRunRoute(w, r)
}
func (s *server) CreatePolicyRun(w http.ResponseWriter, r *http.Request, params contract.CreatePolicyRunParams) {
	s.modelRunRoute(w, r)
}
func (s *server) GetPolicyRun(w http.ResponseWriter, r *http.Request, runId string) {
	s.modelRunRoute(w, r)
}
func (s *server) RotatePolicySubmissionCredential(w http.ResponseWriter, r *http.Request, runId string) {
	s.modelRunRoute(w, r)
}
func (s *server) SubmitSnapshot(w http.ResponseWriter, r *http.Request, runId string, params contract.SubmitSnapshotParams) {
	s.modelRunRoute(w, r)
}

func (s *server) GetRunPolicyRevision(w http.ResponseWriter, r *http.Request, runId string) {
	scope, ok := s.runScope(w, r, identity.ScopeRunsRead)
	if !ok {
		return
	}
	if !modelID.MatchString(runId) {
		s.modelRunError(w, r, runapp.ErrNotFound)
		return
	}
	revision, err := s.options.ModelRuns.GetRunPolicyRevision(r.Context(), scope, runId)
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, revision)
}
func (s *server) GetRunSubmission(w http.ResponseWriter, r *http.Request, runId, submissionId string) {
	scope, ok := s.runScope(w, r, identity.ScopeRunsRead)
	if !ok {
		return
	}
	if !modelID.MatchString(runId) || !modelID.MatchString(submissionId) {
		s.modelRunError(w, r, runapp.ErrNotFound)
		return
	}
	submission, err := s.options.ModelRuns.GetRunSubmission(r.Context(), scope, runId, submissionId)
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, submission)
}
