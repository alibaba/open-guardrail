package ctlcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/httpapi"
	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/audit"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
	domain "github.com/alibaba/open-guardrail/internal/domain/run"
)

func TestAuditAndReportCLIRealService(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ids := identity.New(store.Identity(), security.Passwords{})
	if _, err := ids.BootstrapLocal(ctx, identity.BootstrapRequest{Username: "admin", Password: "synthetic-password"}); err != nil {
		t.Fatal(err)
	}
	login, err := ids.Login(ctx, identity.LoginRequest{Username: "admin", Password: "synthetic-password", Kind: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ids.CreateAPIKey(ctx, login.Token, "cli", "RUN_ORCHESTRATOR")
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := ids.CreateAPIKey(ctx, login.Token, "cli", "POLICY_PUBLISHER")
	if err != nil {
		t.Fatal(err)
	}
	specs := spec.New(store.Specs())
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	yaml, err := os.ReadFile("../../examples/policies/recovery.yaml")
	if err != nil {
		t.Fatal(err)
	}
	initial, _, err := specs.InitializeScene(ctx, scope, "recovery", "Recovery", "default", string(yaml), "init")
	if err != nil {
		t.Fatal(err)
	}
	binding := spec.Binding{Resource: resource.Reference{Kind: "repository", ID: "private-business-resource"}, PolicyID: initial.Policy.ID}
	if _, err = specs.PutBinding(ctx, scope, "recovery", binding); err != nil {
		t.Fatal(err)
	}
	binding.PolicyRevisionID = &initial.Revision.ID
	if _, err = specs.PutBinding(ctx, scope, "recovery", binding); err != nil {
		t.Fatal(err)
	}
	if err = specs.DeleteBinding(ctx, scope, "recovery", binding.Resource); err != nil {
		t.Fatal(err)
	}
	runs := runapp.New(store.Runs())
	rscope := runapp.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	current, _, runToken, err := runs.Create(ctx, rscope, runapp.CreateInput{SceneKey: "recovery", ChangeSet: json.RawMessage(`{"private":"private-business-context"}`), IdempotencyKey: "report"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := domain.SubmissionSnapshot{FinishRun: true, Verdict: "BLOCKED", Summary: "private-business-summary", Checks: []domain.CheckSnapshot{{CheckID: "readiness", Verdict: "BLOCKED", Summary: "private-check-context", Evidence: []domain.Observation{{RequirementID: "health", ObservedAt: time.Now().UTC().Format(time.RFC3339), QuerySummary: "private-query-context", ResultSummary: "private-result-context"}}}}}
	submitScope, err := runs.Authenticate(ctx, current.ID, runToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = runs.Submit(ctx, submitScope, current.ID, "finish", snapshot); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = httpapi.New(store, httpapi.Options{Identity: ids, ModelSpecs: specs, ModelRuns: runs, Audits: audit.New(store.Audits()), Origin: "http://" + server.Listener.Addr().String()})
	server.Start()
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, login.Token)
	keyFile := filepath.Join(dir, "key.json")
	data, _ := json.Marshal(credentialFile{Server: server.URL, Token: key.APIKey})
	if err = os.WriteFile(keyFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(args, &out); err != nil {
			t.Fatalf("%v: %v", args[:2], err)
		}
		return out.String()
	}
	auditArgs := []string{"audit", "list", "--server", server.URL, "--session-file", session, "--resource-type", "policy-binding", "--limit", "2"}
	var page audit.Page
	if err = json.Unmarshal([]byte(call(auditArgs...)), &page); err != nil || len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("page=%+v %v", page, err)
	}
	if page.Items[0].EventType != "policy_binding_deleted" || page.Items[1].EventType != "policy_binding_updated" || !bytes.Contains(page.Items[0].Details, []byte("private-business-resource")) {
		t.Fatalf("binding change chain lost: %+v", page)
	}
	exportArgs := append([]string{}, auditArgs...)
	exportArgs[1] = "export"
	file := filepath.Join(dir, "audit.json")
	call(append(exportArgs, "--output", file)...)
	exported, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 || bytes.Contains(exported, []byte("private-business-resource")) || bytes.Contains(exported, []byte("actorId")) {
		t.Fatal("export permissions or redaction failed")
	}
	var out bytes.Buffer
	if err = run(append(exportArgs, "--output", file), &out); err == nil {
		t.Fatal("export overwrote existing file")
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(after, exported) {
		t.Fatal("existing export changed")
	}
	var older audit.Page
	if err = json.Unmarshal([]byte(call(append(exportArgs, "--cursor", *page.NextCursor)...)), &older); err != nil || len(older.Items) != 1 || older.NextCursor != nil {
		t.Fatalf("older=%+v %v", older, err)
	}
	reportRaw := call("run", "report", "--server", server.URL, "--key-file", keyFile, current.ID)
	var report runapp.Report
	if err = json.Unmarshal([]byte(reportRaw), &report); err != nil || report.Run.ID != current.ID || report.FinalSubmission == nil || strings.Join(report.Reasons, ",") != "CHECK_BLOCKED" || report.Revision.ID != initial.Revision.ID {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	for _, secret := range []string{"private-business-", "private-check-", "private-query-", "private-result-", login.Token, key.APIKey, runToken} {
		if strings.Contains(reportRaw, secret) {
			t.Fatal("report leaked private content")
		}
	}
	get := func(path, token string, want int) {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s status=%d want=%d body=%s", path, res.StatusCode, want, body)
		}
	}
	get("/api/v1alpha1/audit-events", key.APIKey, 401)
	get("/api/v1alpha1/audit-events", runToken, 401)
	get("/api/v1alpha1/audit-events?limit=0", login.Token, 400)
	get("/api/v1alpha1/audit-events?cursor=invalid", login.Token, 400)
	get("/api/v1alpha1/runs/"+current.ID+"/report", login.Token, 200)
	get("/runtime/v1alpha1/runs/"+current.ID+"/report", publisher.APIKey, 403)
	get("/runtime/v1alpha1/runs/"+current.ID+"/report", runToken, 401)
	get("/runtime/v1alpha1/runs/run_missing/report", key.APIKey, 404)
}
