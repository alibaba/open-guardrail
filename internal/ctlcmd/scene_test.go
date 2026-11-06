package ctlcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alibaba/open-guardrail/internal/application/spec"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

func TestSceneApplyInitializeAndUpdate(t *testing.T) {
	state := &sceneAPITestState{}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, "ogs_synthetic")
	file := filepath.Join(dir, "recovery.yaml")
	firstYAML := "apiVersion: guardrail.open/v1alpha1\nkind: GuardrailSpec\n"
	if err := os.WriteFile(file, []byte(firstYAML), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"scene", "apply", "--server", server.URL, "--session-file", session, "--key", "recovery", "--name", "Recovery", "--spec", file, "--publish"}
	first := runSceneApplyForTest(t, args)
	if !first.Created || first.Revision == nil || (first.Revision.RevisionNumber == nil || *first.Revision.RevisionNumber != 1) || first.Scene.DefaultPolicyId == nil {
		t.Fatalf("first=%+v", first)
	}
	second := runSceneApplyForTest(t, args)
	if second.Created || second.DraftChanged || second.Revision == nil || (second.Revision.RevisionNumber == nil || *second.Revision.RevisionNumber != 1) {
		t.Fatalf("second=%+v", second)
	}
	if err := os.WriteFile(file, []byte(firstYAML+"metadata:\n  name: recovery\n"), 0600); err != nil {
		t.Fatal(err)
	}
	third := runSceneApplyForTest(t, args)
	if third.Created || !third.DraftChanged || third.Revision == nil || (third.Revision.RevisionNumber == nil || *third.Revision.RevisionNumber != 2) {
		t.Fatalf("third=%+v", third)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.inits != 1 || state.saves != 1 || state.publishes != 1 || state.activates != 1 {
		t.Fatalf("counts: %+v", state)
	}
}

func TestSceneApplyRepeatDoesNotDownloadLargeHistory(t *testing.T) {
	state := &sceneAPITestState{}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, "ogs_synthetic")
	file := filepath.Join(dir, "recovery.yaml")
	yaml := "apiVersion: guardrail.open/v1alpha1\nkind: GuardrailSpec\n"
	if err := os.WriteFile(file, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"scene", "apply", "--server", server.URL, "--session-file", session, "--key", "recovery", "--name", "Recovery", "--spec", file, "--publish"}
	first := runSceneApplyForTest(t, args)
	if first.Revision == nil {
		t.Fatal("initialization returned no Revision")
	}
	state.mu.Lock()
	for i := 0; i < 16; i++ {
		state.revisions = append(state.revisions, spec.PolicyRevision{ID: fmt.Sprintf("rev_other_%d", i), PolicyID: "pol_1", YAML: strings.Repeat("# history\n", 6000)})
	}
	history, err := json.Marshal(state.revisions)
	state.mu.Unlock()
	if err != nil || len(history) <= sceneResponseLimit {
		t.Fatal("fixture must exceed the client history response limit")
	}
	repeated := runSceneApplyForTest(t, args)
	if repeated.DraftChanged || repeated.Revision == nil || repeated.Revision.Id != first.Revision.Id {
		t.Fatalf("repeat changed the active Revision: %+v", repeated)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.historyReads != 0 || state.publishes != 0 {
		t.Fatalf("repeat downloaded history or republished: history=%d publishes=%d", state.historyReads, state.publishes)
	}
}

func TestSceneApplyRequiresPublishForNewScene(t *testing.T) {
	state := &sceneAPITestState{}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, "ogs_synthetic")
	file := filepath.Join(dir, "recovery.yaml")
	if err := os.WriteFile(file, []byte("draft"), 0600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"scene", "apply", "--server", server.URL, "--session-file", session, "--key", "recovery", "--name", "Recovery", "--spec", file}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "require --publish") || state.inits != 0 {
		t.Fatalf("err=%v inits=%d", err, state.inits)
	}
}

func TestSceneApplyReportsDraftWhenPublishFails(t *testing.T) {
	state := &sceneAPITestState{publishCode: "RESOURCE_STATE_CONFLICT"}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, "ogs_synthetic")
	file := filepath.Join(dir, "recovery.yaml")
	if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"scene", "apply", "--server", server.URL, "--session-file", session, "--key", "recovery", "--name", "Recovery", "--spec", file, "--publish"}
	runSceneApplyForTest(t, args)
	if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	err := run(args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "draft was saved") || !strings.Contains(err.Error(), "RESOURCE_STATE_CONFLICT") {
		t.Fatalf("err=%v", err)
	}
	if state.draft.YAML != "changed" || state.saves != 1 {
		t.Fatalf("policy=%+v", state.policy)
	}
}

func TestSceneApplyRejectsProjectKeyAsSession(t *testing.T) {
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, "http://127.0.0.1:8080", "ogk_synthetic")
	file := filepath.Join(dir, "recovery.yaml")
	_ = os.WriteFile(file, []byte("draft"), 0600)
	err := run([]string{"scene", "apply", "--session-file", session, "--key", "recovery", "--name", "Recovery", "--spec", file}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "session file does not match") {
		t.Fatalf("err=%v", err)
	}
}

func TestSceneApplyDoesNotActivateReplacedDraft(t *testing.T) {
	state := &sceneAPITestState{}
	server := httptest.NewServer(http.HandlerFunc(state.serveHTTP))
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, "ogs_synthetic")
	file := filepath.Join(dir, "recovery.yaml")
	if err := os.WriteFile(file, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"scene", "apply", "--server", server.URL, "--session-file", session, "--key", "recovery", "--name", "Recovery", "--spec", file, "--publish"}
	initial := runSceneApplyForTest(t, args)
	if err := os.WriteFile(file, []byte("reviewed by this caller"), 0600); err != nil {
		t.Fatal(err)
	}
	state.replaceDraftOnPublish = "saved by another session"
	err := run(args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "RESOURCE_STATE_CONFLICT") {
		t.Fatalf("replaced Draft publication: %v", err)
	}
	if state.activates != 0 || len(state.revisions) != 1 || *state.policy.ActiveRevisionID != initial.Revision.Id {
		t.Fatal("conflicted publication changed active Revision or history")
	}
}

func TestReadSpecFileErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := readSpecFile(filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err=%v", err)
	}
	if _, err := readSpecFile(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err=%v", err)
	}
	large := filepath.Join(dir, "large")
	_ = os.WriteFile(large, bytes.Repeat([]byte("a"), specdomain.MaxYAMLBytes+1), 0600)
	if _, err := readSpecFile(large); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("err=%v", err)
	}
	invalid := filepath.Join(dir, "invalid")
	_ = os.WriteFile(invalid, []byte{0xff}, 0600)
	if _, err := readSpecFile(invalid); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("err=%v", err)
	}
}

func runSceneApplyForTest(t *testing.T, args []string) sceneApplyResult {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	var result sceneApplyResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func writeSceneCredential(t *testing.T, dir, server, token string) string {
	t.Helper()
	path := filepath.Join(dir, "session.json")
	data, _ := json.Marshal(credentialFile{Server: server, Token: token})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

type sceneAPITestState struct {
	mu                                 sync.Mutex
	scene                              *spec.SceneModel
	policy                             *spec.Policy
	draft                              *spec.PolicyRevision
	revisions                          []spec.PolicyRevision
	publishCode                        string
	replaceDraftOnPublish              string
	inits, saves, publishes, activates int
	historyReads                       int
}

func (s *sceneAPITestState) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
		_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"},"features":[],"storage":{"supportLevel":"official-local","multiInstance":false}}`)
		return
	}
	if r.Header.Get("Authorization") != "Bearer ogs_synthetic" {
		writeSceneTestError(w, 401, "UNAUTHENTICATED")
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("X-Guardrail-CSRF") != "1" {
		writeSceneTestError(w, 400, "MISSING_CSRF")
		return
	}
	if (strings.HasSuffix(r.URL.Path, ":initialize") || strings.HasSuffix(r.URL.Path, "draft:publish")) &&
		r.Header.Get("Idempotency-Key") == "" {
		writeSceneTestError(w, 400, "MISSING_IDEMPOTENCY_KEY")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1alpha1/scenes/recovery":
		if s.scene == nil {
			writeSceneTestError(w, 404, "NOT_FOUND")
			return
		}
		_ = json.NewEncoder(w).Encode(s.scene)
	case r.Method == "POST" && r.URL.Path == "/api/v1alpha1/scenes/recovery:initialize":
		if s.scene != nil {
			writeSceneTestError(w, 409, "RESOURCE_STATE_CONFLICT")
			return
		}
		var input struct{ Name, DefaultPolicyName, YAML string }
		_ = json.NewDecoder(r.Body).Decode(&input)
		policyID := "pol_1"
		revID := "rev_1"
		s.scene = &spec.SceneModel{Key: "recovery", Name: input.Name, DefaultPolicyID: &policyID, RunTTLSeconds: 900}
		s.policy = &spec.Policy{ID: policyID, SceneKey: "recovery", Name: input.DefaultPolicyName, LifecycleState: "ACTIVE", ActiveRevisionID: &revID}
		s.revisions = []spec.PolicyRevision{{ID: revID, PolicyID: policyID, RevisionNumber: sceneTestPtr(int64(1)), Status: "PUBLISHED", YAML: input.YAML}}
		s.inits++
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(spec.InitializedScene{Scene: *s.scene, Policy: *s.policy, Revision: s.revisions[0]})
	case r.Method == "GET" && r.URL.Path == "/api/v1alpha1/policies/pol_1":
		_ = json.NewEncoder(w).Encode(s.policy)
	case r.Method == "GET" && r.URL.Path == "/api/v1alpha1/policies/pol_1/revisions":
		s.historyReads++
		_ = json.NewEncoder(w).Encode(spec.RevisionPage{Items: []spec.RevisionSummary{}})
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1alpha1/policy-revisions/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1alpha1/policy-revisions/")
		for _, revision := range s.revisions {
			if revision.ID == id {
				_ = json.NewEncoder(w).Encode(revision)
				return
			}
		}
		writeSceneTestError(w, 404, "NOT_FOUND")
	case r.Method == "GET" && r.URL.Path == "/api/v1alpha1/policies/pol_1/draft":
		if s.draft == nil {
			writeSceneTestError(w, 404, "NOT_FOUND")
			return
		}
		_ = json.NewEncoder(w).Encode(s.draft)
	case r.Method == "PUT" && r.URL.Path == "/api/v1alpha1/policies/pol_1/draft":
		var input struct{ YAML string }
		_ = json.NewDecoder(r.Body).Decode(&input)
		if s.draft == nil {
			s.draft = &spec.PolicyRevision{ID: fmt.Sprintf("rev_%d", len(s.revisions)+1), PolicyID: "pol_1", Status: "DRAFT"}
		}
		s.draft.YAML = input.YAML
		s.saves++
		_ = json.NewEncoder(w).Encode(s.draft)
	case r.Method == "POST" && r.URL.Path == "/api/v1alpha1/policies/pol_1/draft:publish":
		s.publishes++
		var input struct{ YAML string }
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeSceneTestError(w, 400, "INVALID_ARGUMENT")
			return
		}
		if s.replaceDraftOnPublish != "" {
			s.draft.YAML = s.replaceDraftOnPublish
		}
		if s.draft == nil || input.YAML != s.draft.YAML {
			writeSceneTestError(w, 409, "RESOURCE_STATE_CONFLICT")
			return
		}
		if s.publishCode != "" {
			writeSceneTestError(w, 409, s.publishCode)
			return
		}
		rev := *s.draft
		rev.RevisionNumber = sceneTestPtr(int64(len(s.revisions) + 1))
		rev.Status = "PUBLISHED"
		s.draft = nil
		s.revisions = append([]spec.PolicyRevision{rev}, s.revisions...)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(rev)
	case r.Method == "PUT" && r.URL.Path == "/api/v1alpha1/policies/pol_1/active-revision":
		var input struct{ PolicyRevisionID string }
		_ = json.NewDecoder(r.Body).Decode(&input)
		s.policy.ActiveRevisionID = &input.PolicyRevisionID
		s.activates++
		_ = json.NewEncoder(w).Encode(s.policy)
	default:
		http.NotFound(w, r)
	}
}
func writeSceneTestError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": code})
}

func sceneTestPtr[T any](value T) *T { return &value }
