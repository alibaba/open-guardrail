package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

var modelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
var modelSceneKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

// Only action suffixes embedded in wildcard segments need a routing shim.
// All ordinary paths go through the generated OpenAPI router and its validation.
// Record a path-only template for unsupported methods; they reach the shim too,
// but must not be logged as POST or include a caller-supplied path.
func (s *server) handleModelAction(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/v1alpha1/scenes/") && strings.HasSuffix(path, ":initialize") {
		r.Pattern = "/api/v1alpha1/scenes/{key}:initialize"
		if r.Method == http.MethodPost {
			r.Pattern = "POST " + r.Pattern
		}
		s.modelSceneRoute(w, r)
		return true
	}
	if strings.HasPrefix(path, "/api/v1alpha1/policies/") && (strings.HasSuffix(path, ":archive") || strings.HasSuffix(path, ":unarchive")) {
		r.Pattern = "/api/v1alpha1/policies/{policyId}:archive"
		if strings.HasSuffix(path, ":unarchive") {
			r.Pattern = "/api/v1alpha1/policies/{policyId}:unarchive"
		}
		if r.Method == http.MethodPost {
			r.Pattern = "POST " + r.Pattern
		}
		s.modelPolicyRoute(w, r)
		return true
	}
	return false
}

func (s *server) modelRevisionRoute(w http.ResponseWriter, r *http.Request, id string) {
	scope, ok := s.specScope(w, r)
	if !ok {
		return
	}
	if !modelID.MatchString(id) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	revision, err := s.options.ModelSpecs.GetPolicyRevision(r.Context(), scope, id)
	if err != nil {
		s.modelSpecError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, revision)
}

func (s *server) decodeModel(w http.ResponseWriter, r *http.Request, dst any, limit int64, required ...string) bool {
	if !decodeJSON(w, r, dst, limit, required...) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid JSON request")
		return false
	}
	return true
}
func modelKey(r *http.Request) string { return r.Header.Get("Idempotency-Key") }
func validModelKey(key string) bool   { return modelID.MatchString(key) }
func (s *server) modelSpecError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *specdomain.ValidationError
	if errors.As(err, &validation) {
		writeError(w, r, http.StatusUnprocessableEntity, "SPEC_VALIDATION_FAILED", validation.Message)
		return
	}
	switch {
	case errors.Is(err, spec.ErrSceneAlreadyInitialized):
		writeError(w, r, http.StatusConflict, "SCENE_ALREADY_INITIALIZED", "Scene already initialized")
	case errors.Is(err, spec.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	case errors.Is(err, spec.ErrConflict):
		writeError(w, r, http.StatusConflict, "RESOURCE_STATE_CONFLICT", "Policy state changed")
	case errors.Is(err, spec.ErrInvalid):
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid Policy request")
	default:
		writeError(w, r, http.StatusServiceUnavailable, "SPEC_UNAVAILABLE", "Policy service unavailable")
	}
}
func (s *server) modelSceneRoute(w http.ResponseWriter, r *http.Request) {
	var scope spec.Scope
	var ok bool
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/policies") {
		scope, ok = s.policyPublisherScope(w, r, identity.ScopePoliciesCreate)
	} else {
		scope, ok = s.specScope(w, r)
	}
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1alpha1/scenes")
	if path == "" {
		if r.Method != http.MethodGet {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
		scenes, err := s.options.ModelSpecs.ListSceneModels(r.Context(), scope)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, scenes)
		return
	}
	if !strings.HasPrefix(path, "/") {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	path = strings.TrimPrefix(path, "/")
	if strings.HasSuffix(path, ":initialize") && r.Method == http.MethodPost {
		key := strings.TrimSuffix(path, ":initialize")
		if !modelSceneKey.MatchString(key) || !validModelKey(modelKey(r)) {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		var in contract.InitializeSceneRequest
		if !s.decodeModel(w, r, &in, 6*specdomain.MaxYAMLBytes+4096) {
			return
		}
		if !validName(in.Name) || !validName(in.DefaultPolicyName) {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		result, created, err := s.options.ModelSpecs.InitializeScene(r.Context(), scope, key, in.Name, in.DefaultPolicyName, in.Yaml, modelKey(r))
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, result)
		return
	}
	if strings.HasSuffix(path, "/policies") {
		sceneKey := strings.TrimSuffix(path, "/policies")
		if !modelSceneKey.MatchString(sceneKey) {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		switch r.Method {
		case http.MethodGet:
			policies, err := s.options.ModelSpecs.ListPolicies(r.Context(), scope, sceneKey)
			if err != nil {
				s.modelSpecError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, policies)
		case http.MethodPost:
			if !validModelKey(modelKey(r)) {
				s.modelSpecError(w, r, spec.ErrInvalid)
				return
			}
			var in contract.CreatePolicyRequest
			if !s.decodeModel(w, r, &in, 4096) {
				return
			}
			if !validName(in.Name) {
				s.modelSpecError(w, r, spec.ErrInvalid)
				return
			}
			policy, created, err := s.options.ModelSpecs.CreatePolicy(r.Context(), scope, sceneKey, in.Name, modelKey(r))
			if err != nil {
				s.modelSpecError(w, r, err)
				return
			}
			status := http.StatusOK
			if created {
				status = http.StatusCreated
			}
			writeJSON(w, status, policy)
		default:
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		}
		return
	}
	if strings.HasSuffix(path, "/default-policy") && r.Method == http.MethodPut {
		sceneKey := strings.TrimSuffix(path, "/default-policy")
		var in contract.SetDefaultPolicyRequest
		if !s.decodeModel(w, r, &in, 4096) {
			return
		}
		if !modelSceneKey.MatchString(sceneKey) || !modelID.MatchString(in.PolicyId) {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		scene, err := s.options.ModelSpecs.SetDefaultPolicy(r.Context(), scope, sceneKey, in.PolicyId, in.ExpectedDefaultPolicyId)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
		return
	}
	if !modelSceneKey.MatchString(path) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		scene, err := s.options.ModelSpecs.GetSceneModel(r.Context(), scope, path)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
	case http.MethodPatch:
		var in contract.SetSceneTTLRequest
		if !s.decodeModel(w, r, &in, 4096) {
			return
		}
		scene, err := s.options.ModelSpecs.SetSceneTTL(r.Context(), scope, path, in.RunTtlSeconds)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, scene)
	default:
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	}
}
func validName(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 100
}
func (s *server) modelPolicyRoute(w http.ResponseWriter, r *http.Request) {
	var scope spec.Scope
	var ok bool
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/revisions") {
		scope, ok = s.policyPublisherScope(w, r, identity.ScopeRevisionsPublish)
	} else {
		scope, ok = s.specScope(w, r)
	}
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1alpha1/policies/")
	if strings.HasSuffix(path, ":archive") || strings.HasSuffix(path, ":unarchive") {
		archived := strings.HasSuffix(path, ":archive")
		suffix := ":archive"
		if !archived {
			suffix = ":unarchive"
		}
		id := strings.TrimSuffix(path, suffix)
		if r.Method != http.MethodPost || !modelID.MatchString(id) {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
		p, err := s.options.ModelSpecs.SetPolicyArchived(r.Context(), scope, id, archived)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 1 || !modelID.MatchString(parts[0]) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		p, err := s.options.ModelSpecs.GetPolicy(r.Context(), scope, id)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}
	if len(parts) != 2 {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	switch parts[1] {
	case "draft":
		if r.Method == http.MethodGet {
			draft, err := s.options.ModelSpecs.GetPolicyDraft(r.Context(), scope, id)
			if err != nil {
				s.modelSpecError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, draft)
			return
		}
		if r.Method != http.MethodPut {
			break
		}
		var in contract.PolicyDraftRequest
		if !s.decodeModel(w, r, &in, 6*specdomain.MaxYAMLBytes+4096) {
			return
		}
		p, err := s.options.ModelSpecs.SavePolicyDraft(r.Context(), scope, id, in.Yaml)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	case "draft:publish", "revisions":
		if r.Method == http.MethodGet && parts[1] == "revisions" {
			pageSize, before := 20, int64(0)
			query := r.URL.Query()
			if value, ok := query["pageSize"]; ok {
				parsed, err := strconv.Atoi(query.Get("pageSize"))
				if len(value) != 1 || err != nil || parsed < 1 || parsed > 100 {
					s.modelSpecError(w, r, spec.ErrInvalid)
					return
				}
				pageSize = parsed
			}
			if value, ok := query["before"]; ok {
				parsed, err := strconv.ParseInt(query.Get("before"), 10, 64)
				if len(value) != 1 || err != nil || parsed < 1 {
					s.modelSpecError(w, r, spec.ErrInvalid)
					return
				}
				before = parsed
			}
			revs, err := s.options.ModelSpecs.ListPolicyRevisions(r.Context(), scope, id, pageSize, before)
			if err != nil {
				s.modelSpecError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, revs)
			return
		}
		if r.Method != http.MethodPost {
			break
		}
		if !validModelKey(modelKey(r)) {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		draft := parts[1] == "draft:publish"
		var in contract.PolicyDraftRequest
		if !s.decodeModel(w, r, &in, 6*specdomain.MaxYAMLBytes+4096, "yaml") {
			return
		}
		rev, created, err := s.options.ModelSpecs.PublishPolicyRevision(r.Context(), scope, id, in.Yaml, modelKey(r), draft)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, rev)
		return
	case "active-revision":
		if r.Method != http.MethodPut {
			break
		}
		var in contract.ActivatePolicyRevisionRequest
		if !s.decodeModel(w, r, &in, 4096) {
			return
		}
		if !modelID.MatchString(in.PolicyRevisionId) {
			s.modelSpecError(w, r, spec.ErrInvalid)
			return
		}
		p, err := s.options.ModelSpecs.ActivatePolicyRevision(r.Context(), scope, id, in.PolicyRevisionId, in.ExpectedActiveRevisionId)
		if err != nil {
			s.modelSpecError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
}

func (s *server) modelRunError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, runapp.ErrUnauthorized):
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Operation not authorized")
	case errors.Is(err, runapp.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Run not found")
	case errors.Is(err, runapp.ErrSpecNotPublished):
		writeError(w, r, http.StatusConflict, "SPEC_NOT_PUBLISHED", "No usable Policy Revision")
	case errors.Is(err, runapp.ErrBindingConflict):
		writeError(w, r, http.StatusConflict, "BINDING_CONFLICT", "Resources have incomplete or conflicting Policy bindings")
	case errors.Is(err, runapp.ErrIdempotencyConflict):
		writeError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency key has different content")
	case errors.Is(err, runapp.ErrBaseSubmissionMismatch):
		writeError(w, r, http.StatusConflict, "BASE_SUBMISSION_MISMATCH", "Submission baseline changed")
	case errors.Is(err, runapp.ErrCompleted):
		writeError(w, r, http.StatusConflict, "RUN_FINISHED", "Run is finished")
	case errors.Is(err, runapp.ErrExpired):
		writeError(w, r, http.StatusGone, "RUN_EXPIRED", "Run expired")
	case errors.Is(err, rundomain.ErrVerdictMismatch):
		writeError(w, r, http.StatusUnprocessableEntity, "VERDICT_MISMATCH", "Declared verdict differs from evaluated verdict")
	case errors.Is(err, rundomain.ErrInvalidSubmission):
		writeError(w, r, http.StatusUnprocessableEntity, "INVALID_SUBMISSION", "Submission does not satisfy Policy Revision")
	case errors.Is(err, rundomain.ErrSensitiveEvidence):
		writeError(w, r, http.StatusUnprocessableEntity, "SENSITIVE_EVIDENCE_DETECTED", "Submission contains sensitive content")
	case errors.Is(err, runapp.ErrInvalid):
		writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid Run request")
	default:
		writeError(w, r, http.StatusServiceUnavailable, "RUN_UNAVAILABLE", "Run service unavailable")
	}
}

func (s *server) modelRunRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/runtime/v1alpha1/runs")
	if path == "" {
		if r.Method == http.MethodGet {
			scope, ok := s.runScope(w, r, identity.ScopeRunsRead)
			if !ok {
				return
			}
			s.modelRunList(w, r, scope)
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
		scope, ok := s.runScope(w, r, identity.ScopeRunsCreate)
		if !ok {
			return
		}
		if !validModelKey(modelKey(r)) {
			s.modelRunError(w, r, runapp.ErrInvalid)
			return
		}
		var in contract.CreatePolicyRunRequest
		// Each resource needs bounded space for JSON-escaped kind/ID and keys.
		if !s.decodeModel(w, r, &in, rundomain.MaxChangeSetBytes+4096+resource.MaxResources*2048, "sceneKey", "changeSet") {
			return
		}
		if !modelSceneKey.MatchString(string(in.SceneKey)) {
			s.modelRunError(w, r, runapp.ErrInvalid)
			return
		}
		var revisionID *string
		if in.PolicyRevisionRef != nil {
			if !modelID.MatchString(in.PolicyRevisionRef.PolicyRevisionId) {
				s.modelRunError(w, r, runapp.ErrInvalid)
				return
			}
			revisionID = &in.PolicyRevisionRef.PolicyRevisionId
		}
		var resources []resource.Reference
		if in.Resources != nil {
			if len(*in.Resources) == 0 {
				s.modelRunError(w, r, runapp.ErrInvalid)
				return
			}
			for _, ref := range *in.Resources {
				resources = append(resources, resource.Reference{Kind: ref.Kind, ID: ref.Id})
			}
		}
		run, created, token, err := s.options.ModelRuns.Create(r.Context(), scope, runapp.CreateInput{Resources: resources, SceneKey: string(in.SceneKey), ChangeSet: in.ChangeSet, RevisionID: revisionID, IdempotencyKey: modelKey(r)})
		if err != nil {
			s.modelRunError(w, r, err)
			return
		}
		if !created {
			writeJSON(w, http.StatusOK, run)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			Run                  runapp.PolicyRun `json:"run"`
			SubmissionCredential struct {
				RunID     string `json:"runId"`
				Token     string `json:"token"`
				ExpiresAt int64  `json:"expiresAt"`
			} `json:"submissionCredential"`
		}{Run: run, SubmissionCredential: struct {
			RunID     string `json:"runId"`
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expiresAt"`
		}{RunID: run.ID, Token: token, ExpiresAt: run.DeadlineAt}})
		return
	}
	if !strings.HasPrefix(path, "/") {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	path = strings.TrimPrefix(path, "/")
	if strings.HasSuffix(path, "/submissions") {
		id := strings.TrimSuffix(path, "/submissions")
		if !modelID.MatchString(id) || r.Method != http.MethodPost {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
		scope, ok := s.submissionScope(w, r, id)
		if !ok {
			return
		}
		if !validModelKey(modelKey(r)) {
			s.modelRunError(w, r, runapp.ErrInvalid)
			return
		}
		var in contract.SnapshotSubmitRequest
		if !s.decodeModel(w, r, &in, rundomain.MaxSubmissionBytes, "baseSubmissionNo", "finishRun", "verdict", "summary", "checks") {
			return
		}
		sub, created, err := s.options.ModelRuns.Submit(r.Context(), scope, id, modelKey(r), snapshotFromContract(in))
		if err != nil {
			s.modelRunError(w, r, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, sub)
		return
	}
	if strings.HasSuffix(path, "/submission-credential:rotate") {
		id := strings.TrimSuffix(path, "/submission-credential:rotate")
		if !modelID.MatchString(id) || r.Method != http.MethodPost {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
		scope, ok := s.runScope(w, r, identity.ScopeSubmissionCredentialRotate)
		if !ok {
			return
		}
		run, token, err := s.options.ModelRuns.Rotate(r.Context(), scope, id)
		if err != nil {
			s.modelRunError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			RunID     string `json:"runId"`
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expiresAt"`
		}{RunID: id, Token: token, ExpiresAt: run.DeadlineAt})
		return
	}
	if modelID.MatchString(path) && r.Method == http.MethodGet {
		scope, ok := s.runScope(w, r, identity.ScopeRunsRead)
		if !ok {
			return
		}
		run, err := s.options.ModelRuns.Get(r.Context(), scope, path)
		if err != nil {
			s.modelRunError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
		return
	}
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
}

type modelRunCursor struct {
	CreatedAt int64  `json:"createdAt"`
	ID        string `json:"id"`
}

func (s *server) modelRunList(w http.ResponseWriter, r *http.Request, scope runapp.Scope) {
	query := r.URL.Query()
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			s.modelRunError(w, r, runapp.ErrInvalid)
			return
		}
		limit = n
	}
	scene, status := query.Get("sceneKey"), query.Get("status")
	if scene != "" && !modelSceneKey.MatchString(scene) {
		s.modelRunError(w, r, runapp.ErrInvalid)
		return
	}
	if status != "" && status != "OPEN" && status != "FINISHED" && status != "EXPIRED" {
		s.modelRunError(w, r, runapp.ErrInvalid)
		return
	}
	cursor := modelRunCursor{}
	if raw := query.Get("cursor"); raw != "" {
		data, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(data) > 256 || json.Unmarshal(data, &cursor) != nil || cursor.CreatedAt < 0 || !modelID.MatchString(cursor.ID) {
			s.modelRunError(w, r, runapp.ErrInvalid)
			return
		}
	}
	items, more, err := s.options.ModelRuns.List(r.Context(), scope, scene, status, limit, cursor.CreatedAt, cursor.ID)
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	response := struct {
		Items      []runapp.PolicyRunSummary `json:"items"`
		NextCursor *string                   `json:"nextCursor,omitempty"`
	}{Items: items}
	if more && len(items) > 0 {
		last := items[len(items)-1]
		encoded, _ := json.Marshal(modelRunCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next := base64.RawURLEncoding.EncodeToString(encoded)
		response.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, response)
}
func (s *server) modelManagedRunRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	scope, ok := s.managedRunScope(w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1alpha1/runs")
	if path == "" {
		s.modelRunList(w, r, scope)
		return
	}
	if !strings.HasPrefix(path, "/") || !modelID.MatchString(path[1:]) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		return
	}
	run, err := s.options.ModelRuns.Get(r.Context(), scope, path[1:])
	if err != nil {
		s.modelRunError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func snapshotFromContract(in contract.SnapshotSubmitRequest) rundomain.SubmissionSnapshot {
	out := rundomain.SubmissionSnapshot{BaseSubmissionNo: in.BaseSubmissionNo, FinishRun: in.FinishRun, Verdict: string(in.Verdict), Summary: in.Summary, Checks: make([]rundomain.CheckSnapshot, 0, len(in.Checks))}
	for _, check := range in.Checks {
		mapped := rundomain.CheckSnapshot{CheckID: check.CheckId, Verdict: string(check.Verdict), Summary: check.Summary, Evidence: make([]rundomain.Observation, 0, len(check.Evidence))}
		for _, evidence := range check.Evidence {
			mapped.Evidence = append(mapped.Evidence, rundomain.Observation{RequirementID: evidence.RequirementId, ObservedAt: evidence.ObservedAt.Format(time.RFC3339Nano), QuerySummary: evidence.QuerySummary, ResultSummary: evidence.ResultSummary, Reference: evidence.Reference})
		}
		out.Checks = append(out.Checks, mapped)
	}
	return out
}
