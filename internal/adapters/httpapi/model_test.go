package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

type pausingSubmissionRepository struct {
	runapp.Repository
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (r *pausingSubmissionRepository) SubmitSnapshot(ctx context.Context, scope runapp.Scope, runID string, input runapp.SnapshotSubmitInput) (runapp.SnapshotSubmission, bool, error) {
	if input.IdempotencyKey == "rotation-race" {
		r.once.Do(func() {
			close(r.entered)
			<-r.resume
		})
	}
	return r.Repository.SubmitSnapshot(ctx, scope, runID, input)
}

func TestModelHTTPFlow(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs := &pausingSubmissionRepository{Repository: store.Runs(), entered: make(chan struct{}), resume: make(chan struct{})}
	handler := New(store, Options{ModelSpecs: spec.New(store.Specs()), ModelRuns: runapp.New(runs), Origin: "https://guardrail.example"})
	call := func(method, path, token, idem string, body any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "https://guardrail.example"+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}
		if method != http.MethodGet {
			req.Header.Set("X-Guardrail-CSRF", "1")
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	expect := func(out *httptest.ResponseRecorder, status int) {
		t.Helper()
		if out.Code != status {
			t.Fatalf("got %d want %d: %s", out.Code, status, out.Body.String())
		}
	}
	init := call("POST", "/api/v1alpha1/scenes/recovery:initialize", "", "init-1", map[string]any{"name": "Recovery", "defaultPolicyName": "default", "yaml": sampleHTTPSpec})
	expect(init, 201)
	scenePath := "/api/v1alpha1/scenes/plugin-scene"
	configured := map[string]any{"changeSet": map[string]string{"url": "https://plugin.example.com/custom/enrich"}}
	sceneInput := map[string]any{"name": "Plugin scene", "defaultPolicyName": "default", "yaml": sampleHTTPSpec, "plugins": configured}
	expect(call("POST", scenePath+":initialize", "", "plugin-init", sceneInput), 201)
	expect(call("POST", scenePath+":initialize", "", "plugin-init", sceneInput), 200)
	sceneInput["plugins"] = map[string]any{}
	expect(call("POST", scenePath+":initialize", "", "plugin-init", sceneInput), 409)
	expect(call("PATCH", scenePath, "", "", map[string]any{"runTtlSeconds": 3600}), 200)
	expect(call("PATCH", scenePath, "", "", map[string]any{"plugins": map[string]any{"changeSet": map[string]string{"url": "http://plugin.example.com"}}}), 400)
	read := call("GET", scenePath, "", "", nil)
	expect(read, 200)
	var configuredScene spec.SceneModel
	if err := json.Unmarshal(read.Body.Bytes(), &configuredScene); err != nil || configuredScene.RunTTLSeconds != 3600 || configuredScene.Plugins == nil || configuredScene.Plugins.ChangeSet == nil || configuredScene.Plugins.ChangeSet.URL != "https://plugin.example.com/custom/enrich" {
		t.Fatalf("stored scene configuration: %s", read.Body.String())
	}
	expect(call("PATCH", scenePath, "", "", map[string]any{"plugins": map[string]any{}}), 200)
	read = call("GET", scenePath, "", "", nil)
	expect(read, 200)
	if strings.Contains(read.Body.String(), `"plugins"`) {
		t.Fatalf("plugins still configured: %s", read.Body.String())
	}
	policyCreated := call("POST", "/api/v1alpha1/scenes/recovery/policies", "", "p-1", map[string]any{"name": "automation-publisher"})
	expect(policyCreated, 201)
	var policy struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(policyCreated.Body.Bytes(), &policy); err != nil || policy.ID == "" {
		t.Fatalf("invalid policy: %s: %v", policyCreated.Body.String(), err)
	}
	expect(call("POST", "/api/v1alpha1/scenes/recovery/policies", "", "p-1", map[string]any{"name": "automation-publisher"}), 200)
	path := "/api/v1alpha1/policies/" + policy.ID
	expect(call("GET", path, "", "", nil), 200)
	draftYAML := strings.Replace(sampleHTTPSpec, "Check recovery readiness", "Check a manual draft", 1)
	expect(call("PUT", path+"/draft", "", "", map[string]any{"yaml": draftYAML}), 200)
	revisionCreated := call("POST", path+"/revisions", "", "r-1", map[string]any{"yaml": sampleHTTPSpec})
	expect(revisionCreated, 201)
	history := call("GET", path+"/revisions?pageSize=1", "", "", nil)
	expect(history, 200)
	var historyPage struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &historyPage); err != nil || len(historyPage.Items) != 1 {
		t.Fatalf("invalid history page: %s", history.Body.String())
	}
	if _, exists := historyPage.Items[0]["yaml"]; exists {
		t.Fatal("history page returned full YAML")
	}
	for _, query := range []string{"pageSize=0", "pageSize=101", "before=0", "before=oops", "pageSize=1&pageSize=2"} {
		expect(call("GET", path+"/revisions?"+query, "", "", nil), 400)
	}
	expect(call("GET", path+"/revisions", "", "", nil), 200)
	var publishedRevision struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(revisionCreated.Body.Bytes(), &publishedRevision); err != nil || publishedRevision.ID == "" {
		t.Fatalf("invalid Revision response: %s: %v", revisionCreated.Body.String(), err)
	}
	expect(call("POST", path+"/revisions", "", "r-1", map[string]any{"yaml": sampleHTTPSpec}), 200)
	explicitBody := map[string]any{"sceneKey": "recovery", "changeSet": map[string]any{"target": "app"}, "policyRevisionRef": map[string]any{"policyRevisionId": publishedRevision.ID}}
	expect(call("POST", "/runtime/v1alpha1/runs", "", "explicit-allowed", explicitBody), 201)
	policyRead := call("GET", path, "", "", nil)
	expect(policyRead, 200)
	var afterPublish map[string]any
	if err := json.Unmarshal(policyRead.Body.Bytes(), &afterPublish); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"draftYaml", "draftUpdatedBy", "draftUpdatedAt", "draftRevisionId"} {
		if _, exists := afterPublish[field]; exists {
			t.Fatalf("obsolete Policy field %s remains", field)
		}
	}
	if afterPublish["activeRevisionId"] != nil {
		t.Fatal("machine publication activated revision")
	}
	draftRead := call("GET", path+"/draft", "", "", nil)
	expect(draftRead, 200)
	var currentDraft struct{ ID, PolicyID, Status, YAML string }
	if err := json.Unmarshal(draftRead.Body.Bytes(), &currentDraft); err != nil || currentDraft.PolicyID == "" || currentDraft.Status != "DRAFT" || currentDraft.YAML != draftYAML {
		t.Fatalf("invalid draft: %s %v", draftRead.Body.String(), err)
	}
	for _, invalid := range []any{nil, map[string]any{}, map[string]any{"yaml": nil}} {
		expect(call("POST", path+"/draft:publish", "", "missing-reviewed-content", invalid), 400)
	}
	expect(call("POST", path+"/draft:publish", "", "reviewed-draft", map[string]any{"yaml": sampleHTTPSpec}), 409)
	publishedDraft := call("POST", path+"/draft:publish", "", "reviewed-draft", map[string]any{"yaml": draftYAML})
	expect(publishedDraft, 201)
	var promoted struct{ ID, Status string }
	if err := json.Unmarshal(publishedDraft.Body.Bytes(), &promoted); err != nil || promoted.ID != currentDraft.ID || promoted.Status != "PUBLISHED" {
		t.Fatalf("draft identity changed: %s %v", publishedDraft.Body.String(), err)
	}
	expect(call("GET", path+"/draft", "", "", nil), 404)
	expect(call("PUT", path+"/draft", "", "", map[string]any{"yaml": sampleHTTPSpec}), 200)
	// A successful exact retry returns its old Revision even after another edit.
	expect(call("POST", path+"/draft:publish", "", "reviewed-draft", map[string]any{"yaml": draftYAML}), 200)
	expect(call("POST", path+"/draft:publish", "", "reviewed-draft", map[string]any{"yaml": sampleHTTPSpec}), 409)
	created := call("POST", "/runtime/v1alpha1/runs", "", "create-1", map[string]any{"sceneKey": "recovery", "changeSet": map[string]any{"target": "app"}})
	expect(created, 201)
	var creation struct {
		Run                  runapp.PolicyRun `json:"run"`
		SubmissionCredential struct {
			Token string `json:"token"`
		} `json:"submissionCredential"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil {
		t.Fatal(err)
	}
	if creation.Run.PolicyRevisionID == "" || creation.SubmissionCredential.Token == "" {
		t.Fatal("missing revision or credential")
	}
	for name, invalid := range map[string]string{
		"leading-space":  " release-1001",
		"trailing-space": "release-1001 ",
		"control":        "release-\x01-1001",
		"too-long":       strings.Repeat("x", 129),
	} {
		response := call("POST", "/runtime/v1alpha1/runs", "", "business-invalid-"+name, map[string]any{"sceneKey": "recovery", "businessId": invalid, "changeSet": map[string]any{"target": "business"}})
		expect(response, 400)
	}
	businessBody := map[string]any{"sceneKey": "recovery", "businessId": "release-1001", "changeSet": map[string]any{"target": "business"}}
	businessCreated := call("POST", "/runtime/v1alpha1/runs", "", "business-create", businessBody)
	expect(businessCreated, 201)
	var businessCreation struct {
		Run runapp.PolicyRun `json:"run"`
	}
	if err := json.Unmarshal(businessCreated.Body.Bytes(), &businessCreation); err != nil || businessCreation.Run.BusinessID == nil || *businessCreation.Run.BusinessID != "release-1001" {
		t.Fatalf("invalid business Run: %s: %v", businessCreated.Body.String(), err)
	}
	businessReplay := call("POST", "/runtime/v1alpha1/runs", "", "business-replay", businessBody)
	expect(businessReplay, 200)
	var replayedBusinessRun runapp.PolicyRun
	if err := json.Unmarshal(businessReplay.Body.Bytes(), &replayedBusinessRun); err != nil || replayedBusinessRun.ID != businessCreation.Run.ID {
		t.Fatalf("invalid business replay: %s: %v", businessReplay.Body.String(), err)
	}
	businessConflict := call("POST", "/runtime/v1alpha1/runs", "", "business-conflict", map[string]any{"sceneKey": "recovery", "businessId": "release-1001", "changeSet": map[string]any{"target": "different"}})
	expect(businessConflict, 409)
	var businessProblem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(businessConflict.Body.Bytes(), &businessProblem); err != nil || businessProblem.Code != "BUSINESS_ID_CONFLICT" {
		t.Fatalf("unexpected business conflict: %s: %v", businessConflict.Body.String(), err)
	}
	businessList := call("GET", "/runtime/v1alpha1/runs?sceneKey=recovery&businessId=release-1001", "", "", nil)
	expect(businessList, 200)
	var businessPage struct {
		Items []runapp.PolicyRunSummary `json:"items"`
	}
	if err := json.Unmarshal(businessList.Body.Bytes(), &businessPage); err != nil || len(businessPage.Items) != 1 || businessPage.Items[0].ID != businessCreation.Run.ID {
		t.Fatalf("invalid business list: %s: %v", businessList.Body.String(), err)
	}
	snapshot := map[string]any{}
	if err := json.Unmarshal([]byte(`{"baseSubmissionNo":0,"finishRun":false,"verdict":"PASS","summary":"healthy","checks":[{"checkId":"readiness","verdict":"PASS","summary":"healthy","evidence":[{"requirementId":"health","observedAt":"2026-10-26T00:00:00Z","querySummary":"queried health","resultSummary":"healthy"}]}]}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	evidence := snapshot["checks"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	evidence["resultSummary"] = `{"dbPassword":"must-not-persist"}`
	rejected := call("POST", "/runtime/v1alpha1/runs/"+creation.Run.ID+"/submissions", creation.SubmissionCredential.Token, "sensitive", snapshot)
	expect(rejected, 422)
	if !strings.Contains(rejected.Body.String(), "SENSITIVE_EVIDENCE_DETECTED") || strings.Contains(rejected.Body.String(), "must-not-persist") {
		t.Fatal("sensitive evidence response leaked content or used the wrong classification")
	}
	current, err := store.Runs().GetPolicyRun(ctx, runapp.Scope{WorkspaceID: "default", ProjectID: "default"}, creation.Run.ID)
	if err != nil || current.LatestSubmissionNo != 0 || current.Status != "OPEN" {
		t.Fatalf("rejected evidence mutated Run: %+v %v", current, err)
	}
	evidence["resultSummary"] = "healthy"
	delete(snapshot, "summary")
	expect(call("POST", "/runtime/v1alpha1/runs/"+creation.Run.ID+"/submissions", creation.SubmissionCredential.Token, "missing-summary", snapshot), 400)
	snapshot["summary"] = nil
	expect(call("POST", "/runtime/v1alpha1/runs/"+creation.Run.ID+"/submissions", creation.SubmissionCredential.Token, "null-summary", snapshot), 400)
	snapshot["summary"] = ""
	first := call("POST", "/runtime/v1alpha1/runs/"+creation.Run.ID+"/submissions", creation.SubmissionCredential.Token, "sub-1", snapshot)
	expect(first, 201)
	snapshot["baseSubmissionNo"] = 1
	snapshot["finishRun"] = true
	final := call("POST", "/runtime/v1alpha1/runs/"+creation.Run.ID+"/submissions", creation.SubmissionCredential.Token, "sub-2", snapshot)
	expect(final, 201)
	repeated := call("POST", "/runtime/v1alpha1/runs/"+creation.Run.ID+"/submissions", creation.SubmissionCredential.Token, "sub-2", snapshot)
	expect(repeated, 200)
	got := call("GET", "/runtime/v1alpha1/runs/"+creation.Run.ID, "", "", nil)
	expect(got, 200)
	var run runapp.PolicyRun
	if err := json.Unmarshal(got.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "FINISHED" || run.Decision == nil || *run.Decision != "ALLOW" || run.LatestSubmissionNo != 2 {
		t.Fatalf("run=%+v", run)
	}
	readBase := "/runtime/v1alpha1/runs/" + run.ID
	revision := call("GET", readBase+"/policy-revision", "", "", nil)
	expect(revision, 200)
	var revisionBody struct {
		ID     string `json:"id"`
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(revision.Body.Bytes(), &revisionBody); err != nil || revisionBody.ID != run.PolicyRevisionID || revisionBody.Digest != run.SpecDigest {
		t.Fatal("Revision differs from frozen Run")
	}
	expect(call("GET", readBase+"/policy-revision", creation.SubmissionCredential.Token, "", nil), 401)
	expect(call("GET", readBase+"/policy-revision", "", "", nil), 200)
	expect(call("GET", "/runtime/v1alpha1/runs/missing/policy-revision", "", "", nil), 404)
	stored := call("GET", readBase+"/submissions/"+*run.FinalSubmissionID, "", "", nil)
	expect(stored, 200)
	var record runapp.StoredSubmission
	if err := json.Unmarshal(stored.Body.Bytes(), &record); err != nil || record.RunID != run.ID || record.SubmissionNo != 2 || record.Snapshot.Verdict != "PASS" || !record.Snapshot.FinishRun {
		t.Fatalf("stored snapshot=%s", stored.Body.String())
	}
	if record.Summary != "" || record.Snapshot.Summary != "" || !bytes.Contains(stored.Body.Bytes(), []byte(`"summary":""`)) {
		t.Fatalf("empty summary was not preserved: %s", stored.Body.String())
	}
	expect(call("GET", readBase+"/submissions/"+*run.FinalSubmissionID, creation.SubmissionCredential.Token, "", nil), 401)
	expect(call("GET", readBase+"/submissions/missing", "", "", nil), 404)
	managedSubmission := "/api/v1alpha1/runs/" + run.ID + "/submissions/" + *run.FinalSubmissionID
	historyPath := "/api/v1alpha1/runs/" + run.ID + "/submissions"
	submissionHistoryResponse := call("GET", historyPath+"?limit=1", "", "", nil)
	expect(submissionHistoryResponse, 200)
	var submissionPage runapp.SubmissionPage
	if err := json.Unmarshal(submissionHistoryResponse.Body.Bytes(), &submissionPage); err != nil || len(submissionPage.Items) != 1 || submissionPage.Items[0].SubmissionNo != 2 || submissionPage.NextBefore == nil || *submissionPage.NextBefore != 2 {
		t.Fatalf("unexpected submission history: %s: %v", submissionHistoryResponse.Body.String(), err)
	}
	if bytes.Contains(submissionHistoryResponse.Body.Bytes(), []byte(`"snapshot"`)) || bytes.Contains(submissionHistoryResponse.Body.Bytes(), []byte(`"checks"`)) {
		t.Fatal("history page must not include evidence payloads")
	}
	older := call("GET", historyPath+"?limit=1&before=2", "", "", nil)
	expect(older, 200)
	var olderPage runapp.SubmissionPage
	if err := json.Unmarshal(older.Body.Bytes(), &olderPage); err != nil || len(olderPage.Items) != 1 || olderPage.Items[0].SubmissionNo != 1 || olderPage.Items[0].FinishRun || olderPage.NextBefore != nil {
		t.Fatalf("unexpected older history: %s", older.Body.String())
	}
	for _, query := range []string{"?limit=0", "?limit=51", "?before=0", "?before=-1"} {
		expect(call("GET", historyPath+query, "", "", nil), 400)
	}
	expect(call("GET", "/api/v1alpha1/runs/missing/submissions", "", "", nil), 404)
	expect(call("GET", managedSubmission, creation.SubmissionCredential.Token, "", nil), 401)
	otherCreated := call("POST", "/runtime/v1alpha1/runs", "", "other-managed-run", map[string]any{"sceneKey": "recovery", "changeSet": map[string]any{"target": "other"}})
	expect(otherCreated, 201)
	var other struct {
		Run runapp.PolicyRun `json:"run"`
	}
	if err := json.Unmarshal(otherCreated.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	expect(call("GET", "/api/v1alpha1/runs/"+other.Run.ID+"/submissions/"+*run.FinalSubmissionID, "", "", nil), 404)
	expect(call("GET", "/api/v1alpha1/runs/"+run.ID+"/submissions/missing", "", "", nil), 404)
	expect(call("GET", "/api/v1alpha1/runs/invalid$id/submissions/"+*run.FinalSubmissionID, "", "", nil), 404)
	for _, field := range []string{"baseSubmissionNo", "finishRun", "checks"} {
		missing := map[string]any{}
		for k, v := range snapshot {
			if k != field {
				missing[k] = v
			}
		}
		expect(call("POST", readBase+"/submissions", creation.SubmissionCredential.Token, "missing-"+field, missing), 400)
	}
	page := call("GET", "/runtime/v1alpha1/runs?status=FINISHED&limit=20", "", "", nil)
	expect(page, 200)
	var listed struct {
		Items []runapp.PolicyRunSummary `json:"items"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].ID != run.ID {
		t.Fatalf("run list=%s error=%v", page.Body.String(), err)
	}
	if bytes.Contains(page.Body.Bytes(), []byte(`"changeSet"`)) || listed.Items[0].ChangeSetDigest != run.ChangeSetDigest {
		t.Fatalf("Run list must expose only the changeSet digest: %s", page.Body.String())
	}

	raceCreated := call("POST", "/runtime/v1alpha1/runs", "", "rotation-run", map[string]any{"sceneKey": "recovery", "changeSet": map[string]any{"target": "rotation-race"}})
	expect(raceCreated, 201)
	var raceCreation struct {
		Run                  runapp.PolicyRun `json:"run"`
		SubmissionCredential struct {
			Token string `json:"token"`
		} `json:"submissionCredential"`
	}
	if err := json.Unmarshal(raceCreated.Body.Bytes(), &raceCreation); err != nil {
		t.Fatal(err)
	}
	raceSnapshot := map[string]any{}
	if err := json.Unmarshal([]byte(`{"baseSubmissionNo":0,"finishRun":false,"verdict":"PASS","summary":"healthy","checks":[{"checkId":"readiness","verdict":"PASS","summary":"healthy","evidence":[{"requirementId":"health","observedAt":"2026-10-26T00:00:00Z","querySummary":"queried health","resultSummary":"healthy"}]}]}`), &raceSnapshot); err != nil {
		t.Fatal(err)
	}
	staleResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		staleResponse <- call("POST", "/runtime/v1alpha1/runs/"+raceCreation.Run.ID+"/submissions", raceCreation.SubmissionCredential.Token, "rotation-race", raceSnapshot)
	}()
	select {
	case <-runs.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old credential request did not reach the authenticated submission boundary")
	}
	rotated := call("POST", "/runtime/v1alpha1/runs/"+raceCreation.Run.ID+"/submission-credential:rotate", "", "", nil)
	expect(rotated, 201)
	var replacement struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rotated.Body.Bytes(), &replacement); err != nil || replacement.Token == "" {
		t.Fatalf("invalid rotated credential: %s: %v", rotated.Body.String(), err)
	}
	close(runs.resume)
	select {
	case response := <-staleResponse:
		expect(response, 403)
	case <-time.After(5 * time.Second):
		t.Fatal("old credential request did not finish after rotation")
	}
	expect(call("POST", "/runtime/v1alpha1/runs/"+raceCreation.Run.ID+"/submissions", replacement.Token, "rotation-race", raceSnapshot), 201)
}

const sampleHTTPSpec = `apiVersion: guardrail.open/v1alpha1
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
