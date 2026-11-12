package ctlcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/adapters/httpapi"
	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	"github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
)

func TestBindingCLIRealService(t *testing.T) {
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
	specs := spec.New(store.Specs())
	scope := spec.Scope{WorkspaceID: "default", ProjectID: "default", ActorID: "admin"}
	yamlBytes, err := os.ReadFile("../../examples/policies/recovery.yaml")
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(yamlBytes)
	initial, _, err := specs.InitializeScene(ctx, scope, "recovery", "Recovery", "default", yaml, "init")
	if err != nil {
		t.Fatal(err)
	}
	policy, _, err := specs.CreatePolicy(ctx, scope, "recovery", "bound", "policy")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := specs.PublishPolicyRevision(ctx, scope, policy.ID, yaml, "first", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := specs.ActivatePolicyRevision(ctx, scope, policy.ID, first.ID, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.New(store, httpapi.Options{Identity: ids, Origin: origin, ModelSpecs: specs, ModelRuns: runapp.New(store.Runs())})
	server.Start()
	defer server.Close()
	dir := t.TempDir()
	session := writeSceneCredential(t, dir, server.URL, login.Token)
	keyFile := filepath.Join(dir, "key.json")
	keyData, _ := json.Marshal(credentialFile{Server: server.URL, Token: key.APIKey})
	if err := os.WriteFile(keyFile, keyData, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		args = append(args, "--server", server.URL, "--session-file", session, "--scene", "recovery")
		if err := run(args, &out); err != nil {
			t.Fatalf("%v: %v", args[:2], err)
		}
		return out.String()
	}
	for _, id := range []string{".", ".."} {
		call("binding", "put", "--kind", "repository", "--resource-id", id, "--policy-id", policy.ID)
		call("binding", "delete", "--kind", "repository", "--resource-id", id)
	}
	resourceID := "team/repo %2F?#中文"
	put := []string{"binding", "put", "--kind", "repository", "--resource-id", resourceID, "--policy-id", policy.ID}
	call(put...)
	page := call("binding", "list", "--limit", "1")
	var listed contract.BindingPage
	if err := json.Unmarshal([]byte(page), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].Resource.Id != resourceID {
		t.Fatalf("page=%s err=%v", page, err)
	}
	call("binding", "put", "--kind", "repository", "--resource-id", "z-last", "--policy-id", policy.ID)
	if err := json.Unmarshal([]byte(call("binding", "list", "--limit", "1")), &listed); err != nil || listed.NextAfter == nil {
		t.Fatalf("cursor=%+v %v", listed, err)
	}
	if err := json.Unmarshal([]byte(call("binding", "list", "--limit", "1", "--after", *listed.NextAfter)), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].Resource.Id != "z-last" {
		t.Fatalf("page2=%+v %v", listed, err)
	}
	change := filepath.Join(dir, "change.json")
	if err := os.WriteFile(change, []byte(`{"changeSetRef":"binding-cli"}`), 0600); err != nil {
		t.Fatal(err)
	}
	create := func(operation string, refs any, wantFailure bool) contract.PolicyRun {
		t.Helper()
		args := []string{"run", "create", "--server", server.URL, "--key-file", keyFile, "--scene", "recovery", "--change-set", change, "--operation-id", operation, "--submission-credential-file", filepath.Join(dir, operation+"-credential.json")}
		if refs != nil {
			data, _ := json.Marshal(refs)
			file := filepath.Join(dir, "resources.json")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			args = append(args, "--resources", file)
		}
		var out bytes.Buffer
		err := run(args, &out)
		if wantFailure {
			if commandExitCode(err) != exitServerRejected {
				t.Fatalf("want rejection, got %v %s", err, out.String())
			}
			if _, statErr := os.Stat(filepath.Join(dir, operation+"-credential.json")); !os.IsNotExist(statErr) {
				t.Fatal("failed create retained output credential")
			}
			return contract.PolicyRun{}
		}
		if err != nil {
			t.Fatal(err)
		}
		var result contract.PolicyRun
		if err = json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	refs := []contract.ResourceReference{{Kind: "repository", Id: resourceID}}
	old := create("old", refs, false)
	if old.PolicyId != policy.ID || old.PolicyRevisionId != first.ID || old.Selection == nil {
		t.Fatalf("old=%+v", old)
	}
	second, _, err := specs.PublishPolicyRevision(ctx, scope, policy.ID, strings.Replace(yaml, "Recovery readiness", "Updated recovery readiness", 1), "second", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = specs.ActivatePolicyRevision(ctx, scope, policy.ID, second.ID, &first.ID); err != nil {
		t.Fatal(err)
	}
	active := create("active", refs, false)
	if second.ID == first.ID || active.PolicyRevisionId != second.ID {
		t.Fatalf("active=%+v", active)
	}
	frozenAfterActivation, err := store.Runs().GetPolicyRun(ctx, runapp.Scope{WorkspaceID: "default", ProjectID: "default"}, old.Id)
	if err != nil || frozenAfterActivation.PolicyRevisionID != first.ID {
		t.Fatal("activating a Revision changed an existing Run")
	}
	call(append(put, "--revision-id", first.ID)...)
	pinned := create("pinned", refs, false)
	if pinned.PolicyRevisionId != first.ID || pinned.Selection == nil || pinned.Selection.RevisionMode == nil || *pinned.Selection.RevisionMode != "pinned" {
		t.Fatalf("pinned=%+v", pinned)
	}
	// A replay after rebinding keeps the original identity and frozen Revision.
	saved := filepath.Join(dir, "old-credential.json")
	before, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(saved, saved+".saved"); err != nil {
		t.Fatal(err)
	}
	create("old", refs, true)
	after, err := os.ReadFile(saved + ".saved")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("original credential changed")
	}
	frozen, err := store.Runs().GetPolicyRun(ctx, runapp.Scope{WorkspaceID: "default", ProjectID: "default"}, old.Id)
	if err != nil || frozen.PolicyRevisionID != first.ID {
		t.Fatalf("frozen=%+v %v", frozen, err)
	}
	create("partial", append(refs, contract.ResourceReference{Kind: "repository", Id: "unbound"}), true)
	call("binding", "put", "--kind", "repository", "--resource-id", "other", "--policy-id", initial.Policy.ID)
	create("conflict", append(refs, contract.ResourceReference{Kind: "repository", Id: "other"}), true)
	noRefs := create("default", nil, false)
	unbound := create("unbound", []contract.ResourceReference{{Kind: "repository", Id: "unknown"}}, false)
	if noRefs.PolicyId != initial.Policy.ID || noRefs.Selection != nil || unbound.PolicyId != initial.Policy.ID || unbound.Selection != nil {
		t.Fatal("default selection changed")
	}
	call("binding", "delete", "--kind", "repository", "--resource-id", resourceID)
	call("binding", "delete", "--kind", "repository", "--resource-id", resourceID)
	afterDelete := create("after-delete", refs, false)
	if afterDelete.PolicyId != initial.Policy.ID {
		t.Fatal("delete did not remove binding")
	}
	// A Project key cannot be used as an administrator session for management.
	var out bytes.Buffer
	err = run([]string{"binding", "list", "--server", server.URL, "--session-file", keyFile, "--scene", "recovery"}, &out)
	if err == nil {
		t.Fatalf("machine management auth=%v", err)
	}
}

func TestBindingCLIRejectsInvalidFlags(t *testing.T) {
	for _, args := range [][]string{
		{"binding", "put", "--scene", "recovery", "--kind", "repository", "--resource-id", "repo"},
		{"binding", "list", "--scene", "recovery", "--limit", "101"},
		{"binding", "list", "--scene", "recovery", "--session-file", ""},
		{"binding", "put", "--scene", "recovery", "--kind", "INVALID", "--resource-id", "repo", "--policy-id", "policy-1"},
		{"binding", "list", "--scene", "recovery", "--unknown"},
	} {
		if err := run(args, &bytes.Buffer{}); commandExitCode(err) != exitUsage {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestRunResourcesValidation(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{}`, `[{"kind":"repository","id":"a"},{"kind":"repository","id":"a"}]`, `[{"kind":"repository","id":"a","extra":true}]`, `[{"kind":"Repository","id":"a"}]`, `[{"kind":"repository","id":"a"}] []`} {
		file := filepath.Join(t.TempDir(), "refs.json")
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRunResources(file); commandExitCode(err) != exitUsage {
			t.Fatalf("accepted %s: %v", data, err)
		}
	}
}

func TestRunResourcesExplicitEmptyIsRejected(t *testing.T) {
	_, err := parseRunOptions("create", []string{"--key-file", "key.json", "--scene", "recovery", "--change-set", "change.json", "--submission-credential-file", "credential.json", "--resources", ""}, &bytes.Buffer{})
	if commandExitCode(err) != exitUsage {
		t.Fatalf("empty resources accepted: %v", err)
	}
}
