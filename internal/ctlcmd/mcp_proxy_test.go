package ctlcmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/adapters/httpapi"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	specapp "github.com/alibaba/open-guardrail/internal/application/spec"
	"github.com/alibaba/open-guardrail/internal/config"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

func TestMCPStrictReadOnlySQL(t *testing.T) {
	for _, sql := range []string{
		"SELECT * FROM users WHERE id = 1", "SELECT 1;", "WITH cte AS (SELECT id FROM t) SELECT * FROM cte",
		"EXPLAIN SELECT 1", "SHOW TABLES", "DESCRIBE users", "DESC users", "VALUES (1), (2)",
		"SELECT 'DELETE FROM x' AS note", "SELECT 'it''s safe; DELETE'", "SELECT $$DELETE;$$", "SELECT $tag$DROP;$tag$",
		"/* note */ SELECT \"UPDATE\", `DELETE` FROM users; -- trailing comment\n", "SELECT (1 + 2)",
		"SELECT * FROM (SELECT id FROM users) AS u", "SELECT * FROM a JOIN b USING (id)",
		"EXPLAIN (FORMAT JSON) SELECT * FROM users", "SHOW CREATE TABLE users",
		"SELECT COUNT(*) FROM users", "SELECT SUM(amount), AVG(amount), MIN(id), MAX(id) FROM orders",
		"SELECT COALESCE(name, 'anon'), NULLIF(status, '') FROM users", "SELECT sum(id) OVER () FROM users",
	} {
		t.Run(sql, func(t *testing.T) {
			if !isStrictReadOnlySQL(sql) {
				t.Fatal("read-only SQL was gated")
			}
		})
	}
	for _, sql := range []string{
		"", "-- comment only", "DELETE FROM users", "UPDATE users SET a=1", "SELECT * INTO backup FROM users",
		"WITH d AS (DELETE FROM users RETURNING *) SELECT * FROM d", "SELECT 1; DROP TABLE users",
		"SELECT 1;;", "EXPLAIN ANALYZE DELETE FROM users", "EXPLAIN (ANALYZE true) DELETE FROM users",
		"SELECT /*!50000 1; DROP TABLE x */", "SELECT /*M!100100 1; DROP TABLE x */", "SELECT 1 /* unclosed",
		"SELECT 'abc", "SELECT \"abc", "SELECT `abc", "SELECT $tag$abc", "SELECT 1 FOR SHARE",
		"SELECT 1 FOR NO KEY UPDATE", "SELECT nextval('sequence')", "SELECT custom_mutating_udf()",
		"SELECT \"mutating\"()", "SELECT pg_advisory_lock(1)", "SELECT 1 # 2; DELETE FROM x",
		"SELECT 1--+2; DELETE FROM x", "SELECT 1 /* nested /* */ ; DELETE FROM x */", "SELECT 'a\\'; DELETE FROM x; -- '",
		"SELECT (1", "SELECT 1)", "SELECT @variable := 1",
		"SELECT * FROM (DELETE FROM users RETURNING *) AS u", "EXPLAIN (FORMAT JSON) DELETE FROM users",
		"SHOW CREATE TABLE users; DELETE FROM users", "WITH x AS (CREATE TABLE y (id int)) SELECT 1",
		"SELECT public.from()", "SELECT public.values()", "SELECT custom_window(id) OVER () FROM users",
		"SELECT public.count(*) FROM users", "SELECT COUNT(pg_advisory_lock(1)) FROM users",
	} {
		t.Run(sql, func(t *testing.T) {
			if isStrictReadOnlySQL(sql) {
				t.Fatal("unsafe or ambiguous SQL passed through")
			}
		})
	}
}

func mcpTestConfig(t *testing.T, text string) mcpProxyConfig {
	t.Helper()
	name := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadMCPProxyConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMCPPolicyAndSpecValidation(t *testing.T) {
	if _, err := loadMCPProxyConfig("../../integrations/mcp-proxy/example-policy.yaml"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../integrations/mcp-proxy/db-write-guard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = specdomain.Validate(string(data)); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{
		"version: 2", "version: 1\nkeyFile: old.json", "version: 1\n---\nversion: 1", "version: 1\ndefaultAction: allow",
		"version: 1\nrules: [{name: x, tools: ['['], scene: x}]",
		"version: 1\nrules: [{name: x, tools: [x]}]",
		"version: 1\nrules: [{name: x, tools: [x], scene: x, when: {arg: sql}}]",
		"version: 1\nrules: [{name: x, tools: [x], scene: x, when: {arg: sql, notReadOnlySql: false}}]",
		"version: 1\nrules: [{name: x, tools: [x], scene: x, when: {arg: sql, equals: '', notEquals: ''}}]",
		"version: 1\nrules: [{name: x, tools: [x], scene: x, when: {arg: sql, matchesRegex: '['}}]",
		"version: 1\ndestructiveHintPolicy: {unknown: true}",
		"version: 1\nrules: [{name: destructive-hint:x, tools: [x], scene: x}]",
		"version: 1\nrules: [{name: x, tools: [x], scene: x}, {name: x, tools: [y], scene: y}]",
	} {
		name := filepath.Join(t.TempDir(), "invalid.yaml")
		if err := os.WriteFile(name, []byte(policy), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMCPProxyConfig(name); err == nil {
			t.Fatalf("accepted invalid policy: %s", policy)
		}
	}
	for _, predicate := range []string{"equals: ''", "notEquals: safe", "matchesRegex: '^safe$'", "notMatchesRegex: '^safe$'", "notReadOnlySql: true"} {
		cfg := mcpTestConfig(t, "version: 1\nrules:\n- name: test\n  tools: [test]\n  scene: test\n  when:\n    arg: sql\n    "+predicate+"\n")
		negative := strings.HasPrefix(predicate, "not")
		for _, args := range []map[string]any{nil, {"sql": 123}, {"sql": nil}} {
			if got := cfg.Rules[0].When.matches(args); got != negative {
				t.Fatalf("%s: missing/nonstring matched=%v", predicate, got)
			}
		}
	}
}

type mcpTestAPI struct {
	mu                         sync.Mutex
	requests, lookups, creates int
	runs                       map[string]contract.PolicyRun
	status                     int
	malformed                  bool
}

func newMCPTestAPI(t *testing.T) (*mcpTestAPI, *httptest.Server) {
	t.Helper()
	api := &mcpTestAPI{runs: map[string]contract.PolicyRun{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		api.requests++
		w.Header().Set("Content-Type", "application/json")
		if api.status != 0 {
			w.WriteHeader(api.status)
			return
		}
		if api.malformed {
			_, _ = fmt.Fprint(w, `{broken`)
			return
		}
		if r.URL.Path == "/api/v1alpha1/meta/capabilities" {
			_, _ = fmt.Fprint(w, `{"apiVersions":["v1alpha1"],"cli":{"minimumVersion":"0.1.0-alpha.0","maximumVersionExclusive":"0.2.0"}}`)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("proxy sent a credential to the management API")
		}
		if r.Method == http.MethodPost && r.URL.Path == "/runtime/v1alpha1/runs" {
			var request contract.CreatePolicyRunJSONRequestBody
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if r.Header.Get("Idempotency-Key") == "" {
				t.Error("missing idempotency key")
			}
			if len(request.ChangeSet) > rundomain.MaxChangeSetBytes {
				t.Error("oversized changeSet")
			}
			api.creates++
			id := fmt.Sprintf("run_%d", api.creates)
			run := contract.PolicyRun{Id: id, SceneKey: request.SceneKey, ChangeSet: request.ChangeSet, Status: contract.PolicyRunStatusOPEN}
			api.runs[id] = run
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"run": run, "submissionCredential": map[string]any{"runId": id, "token": "ogrt_synthetic", "expiresAt": time.Now().Unix() + 900}})
			return
		}
		api.lookups++
		id := strings.TrimPrefix(r.URL.Path, "/runtime/v1alpha1/runs/")
		run, ok := api.runs[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(run)
	}))
	t.Cleanup(server.Close)
	return api, server
}

func (a *mcpTestAPI) allow(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	run := a.runs[id]
	verdict, decision, expiry := contract.PASS, contract.ALLOW, time.Now().Unix()+900
	run.Status, run.Result, run.Decision, run.DecisionExpiresAt = contract.PolicyRunStatusFINISHED, &verdict, &decision, &expiry
	a.runs[id] = run
}

func mcpTestCall(id any, tool string, args any) []byte {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	return data
}

func mcpTestBlocked(t *testing.T, p *mcpProxy, args map[string]any) string {
	t.Helper()
	forward, reply, err := p.fromClient(context.Background(), mcpTestCall(1, "delete_file", args))
	if err != nil || forward != nil || !bytes.Contains(reply, []byte(`"isError":true`)) {
		t.Fatalf("not blocked: forward=%s reply=%s err=%v", forward, reply, err)
	}
	var result struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(reply, &result); err != nil || len(result.Result.Content) != 1 {
		t.Fatalf("invalid blocked tool result: %s", reply)
	}
	return result.Result.Content[0].Text
}

func mcpDeleteProxy(t *testing.T, server string) *mcpProxy {
	t.Helper()
	return newMCPProxy(mcpProxyConfig{Server: server, Rules: []mcpProxyRule{{Name: "delete", Tools: []string{"delete_*"}, Scene: "cleanup", ReuseValidRun: true}}}, t.TempDir())
}

func TestMCPZeroHTTPPassThrough(t *testing.T) {
	api, server := newMCPTestAPI(t)
	cfg := mcpTestConfig(t, "version: 1\nrules:\n- name: db\n  tools: [execute_sql]\n  scene: db\n  when: {arg: query, notReadOnlySql: true}\n")
	cfg.Server = server.URL
	p := newMCPProxy(cfg, t.TempDir())
	for _, frame := range [][]byte{
		[]byte(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`),
		[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`),
		mcpTestCall(1, "read_file", map[string]any{"path": "a"}),
		mcpTestCall("sql", "execute_sql", map[string]any{"query": "SELECT * FROM users"}),
	} {
		forward, reply, err := p.fromClient(context.Background(), frame)
		if err != nil || reply != nil || !bytes.Equal(forward, frame) {
			t.Fatalf("pass-through failed: %s %s %v", forward, reply, err)
		}
	}
	if api.requests != 0 {
		t.Fatalf("read-only calls made %d HTTP requests", api.requests)
	}
	for _, args := range []any{map[string]any{}, map[string]any{"query": false}, nil} {
		forward, reply, err := p.fromClient(context.Background(), mcpTestCall(2, "execute_sql", args))
		if err != nil || forward != nil || reply == nil {
			t.Fatalf("missing/nonstring SQL passed: %s %s %v", forward, reply, err)
		}
	}
}

func TestMCPApprovalBindingAndCredential(t *testing.T) {
	api, server := newMCPTestAPI(t)
	p := mcpDeleteProxy(t, server.URL)
	reply := mcpTestBlocked(t, p, map[string]any{"path": "tmp/a.log"})
	for _, expected := range []string{"run_1", "guardrailctl run revision", "--submission-credential-file", "--submission <submission.json>"} {
		if !strings.Contains(reply, expected) {
			t.Fatalf("missing guidance %q: %s", expected, reply)
		}
	}
	if strings.Contains(reply, "--key-file") || strings.Contains(reply, "ogrt_synthetic") {
		t.Fatal("obsolete flag or raw credential in guidance")
	}
	files, err := os.ReadDir(p.runDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("credential files=%v err=%v", files, err)
	}
	file := filepath.Join(p.runDir, files[0].Name())
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode=%v", info.Mode())
	}
	if _, err := readSubmissionCredential(runOptions{server: server.URL, runID: "run_1", tokenFile: file}); err != nil {
		t.Fatal(err)
	}
	api.allow("run_1")
	frame := []byte(`{"jsonrpc":"2.0","id":"retry","method":"tools/call","_meta":{"trace":1},"params":{"name":"delete_file","_meta":{"progressToken":"p"},"arguments":{"path":"tmp/a.log","large":9007199254740993,"_guardrail_run_id":"run_1"}}}`)
	// Changed arguments, including a large integer, cannot borrow the approval.
	forward, blocked, err := p.fromClient(context.Background(), frame)
	if err != nil || forward != nil || blocked == nil {
		t.Fatal("changed arguments reused approval")
	}
	frame = []byte(`{"jsonrpc":"2.0","id":"retry","method":"tools/call","_meta":{"trace":1},"params":{"name":"delete_file","_meta":{"progressToken":"p"},"arguments":{"path":"tmp/a.log","_guardrail_run_id":"run_1"}}}`)
	forward, blocked, err = p.fromClient(context.Background(), frame)
	if err != nil || blocked != nil || forward == nil || bytes.Contains(forward, []byte(mcpRunIDArg)) || !bytes.Contains(forward, []byte(`"progressToken":"p"`)) || !bytes.Contains(forward, []byte(`"trace":1`)) {
		t.Fatalf("retry=%s blocked=%s err=%v", forward, blocked, err)
	}
	lookups := api.lookups
	forward, blocked, err = p.fromClient(context.Background(), mcpTestCall(3, "delete_file", map[string]any{"path": "tmp/a.log"}))
	if err != nil || blocked != nil || forward == nil || api.lookups != lookups {
		t.Fatal("identical arguments did not reuse valid ALLOW")
	}
	mcpTestBlocked(t, p, map[string]any{"path": "tmp/b.log"})
	if api.creates != 2 {
		t.Fatal("changed arguments reused cached ALLOW")
	}
	mcpTestBlocked(t, p, map[string]any{"path": "tmp/b.log", mcpRunIDArg: "run_1"})
	mcpTestBlocked(t, p, map[string]any{"path": "tmp/a.log", mcpRunIDArg: "run_external"})
	key := mcpCacheKey(p.config.Rules[0], mustMCPDigest(t, "delete_file", map[string]any{"path": "tmp/a.log"}))
	p.allows[key] = mcpAllowedRun{runID: "run_1", expiresAt: time.Now().Unix()}
	mcpTestBlocked(t, p, map[string]any{"path": "tmp/a.log"})
	if api.creates != 3 {
		t.Fatal("expired cache entry passed through")
	}
}

func mustMCPDigest(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	_, digest, err := canonicalMCPArgs(tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestMCPCanonicalArgumentsAndChangeSetLimit(t *testing.T) {
	if mustMCPDigest(t, "x", nil) != mustMCPDigest(t, "x", map[string]any{}) {
		t.Fatal("nil arguments must normalize to {}")
	}
	a := map[string]any{"nested": map[string]any{"b": 2, "a": 1}, "x": true}
	b := map[string]any{"x": true, "nested": map[string]any{"a": 1, "b": 2}, mcpRunIDArg: "run_1"}
	if mustMCPDigest(t, "x", a) != mustMCPDigest(t, "x", b) || mustMCPDigest(t, "y", a) == mustMCPDigest(t, "x", a) {
		t.Fatal("unstable or tool-independent digest")
	}
	api, server := newMCPTestAPI(t)
	p := mcpDeleteProxy(t, server.URL)
	args := map[string]any{"path": strings.Repeat("x", rundomain.MaxChangeSetBytes)}
	mcpTestBlocked(t, p, args)
	var change mcpRunBinding
	if err := json.Unmarshal(api.runs["run_1"].ChangeSet, &change); err != nil {
		t.Fatal(err)
	}
	if !change.ArgumentsTruncated || change.Arguments != nil || change.ArgumentsDigest != mustMCPDigest(t, "delete_file", args) {
		t.Fatal("truncation lost binding")
	}
}

func TestMCPFailClosedRunValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*contract.PolicyRun)
	}{
		{"id", func(r *contract.PolicyRun) { r.Id = "other" }},
		{"scene", func(r *contract.PolicyRun) { r.SceneKey = "other" }},
		{"tool", func(r *contract.PolicyRun) {
			r.ChangeSet = []byte(`{"source":"mcp-proxy","rule":"delete","tool":"other","argumentsDigest":"sha256:no"}`)
		}},
		{"digest", func(r *contract.PolicyRun) {
			var b mcpRunBinding
			_ = json.Unmarshal(r.ChangeSet, &b)
			b.ArgumentsDigest = "sha256:other"
			r.ChangeSet, _ = json.Marshal(b)
		}},
		{"malformed changeset", func(r *contract.PolicyRun) { r.ChangeSet = []byte(`null`) }},
		{"open", func(r *contract.PolicyRun) { r.Status = contract.PolicyRunStatusOPEN }},
		{"expired status", func(r *contract.PolicyRun) { r.Status = contract.PolicyRunStatusEXPIRED }},
		{"unknown", func(r *contract.PolicyRun) { v := contract.RunResult("UNKNOWN"); r.Result = &v }},
		{"blocked", func(r *contract.PolicyRun) { v := contract.RunResult("BLOCKED"); r.Result = &v }},
		{"missing result", func(r *contract.PolicyRun) { r.Result = nil }},
		{"deny", func(r *contract.PolicyRun) { v := contract.Decision("DENY"); r.Decision = &v }},
		{"missing decision", func(r *contract.PolicyRun) { r.Decision = nil }},
		{"missing expiry", func(r *contract.PolicyRun) { r.DecisionExpiresAt = nil }},
		{"expired decision", func(r *contract.PolicyRun) { v := time.Now().Unix() - 1; r.DecisionExpiresAt = &v }},
		{"expiry equals now", func(r *contract.PolicyRun) { v := time.Now().Unix(); r.DecisionExpiresAt = &v }},
	} {
		t.Run(test.name, func(t *testing.T) {
			api, server := newMCPTestAPI(t)
			p := mcpDeleteProxy(t, server.URL)
			mcpTestBlocked(t, p, map[string]any{"path": "a"})
			api.allow("run_1")
			api.mu.Lock()
			r := api.runs["run_1"]
			test.mutate(&r)
			api.runs["run_1"] = r
			api.mu.Unlock()
			mcpTestBlocked(t, p, map[string]any{"path": "a", mcpRunIDArg: "run_1"})
		})
	}
	for _, mode := range []string{"unreachable", "503", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			api, server := newMCPTestAPI(t)
			p := mcpDeleteProxy(t, server.URL)
			mcpTestBlocked(t, p, map[string]any{"path": "a"})
			api.allow("run_1")
			api.mu.Lock()
			api.status = 503
			if mode == "malformed" {
				api.status = 0
				api.malformed = true
			}
			api.mu.Unlock()
			if mode == "unreachable" {
				server.Close()
			}
			mcpTestBlocked(t, p, map[string]any{"path": "a", mcpRunIDArg: "run_1"})
		})
	}
}

func TestMCPDestructiveToolsAndRulePriority(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			api, server := newMCPTestAPI(t)
			cfg := mcpProxyConfig{Server: server.URL}
			if configured {
				cfg.DestructiveHintPolicy.Scene = "destructive"
			}
			p := newMCPProxy(cfg, t.TempDir())
			request := []byte(`{"jsonrpc":"2.0","id":"list","method":"tools/list"}`)
			if _, _, err := p.fromClient(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			response := []byte(`{"jsonrpc":"2.0","id":"list","result":{"tools":[{"name":"nuke_env","annotations":{"destructiveHint":true}},{"name":"ping"}]}}`)
			if err := p.observeDownstream(response); err != nil {
				t.Fatal(err)
			}
			if len(p.pendingToolsList) != 0 {
				t.Fatal("pending tools/list request not removed")
			}
			forward, reply, err := p.fromClient(context.Background(), mcpTestCall(2, "ping", nil))
			if err != nil || reply != nil || forward == nil {
				t.Fatal("ping did not pass through")
			}
			forward, reply, err = p.fromClient(context.Background(), mcpTestCall(3, "nuke_env", nil))
			if err != nil || forward != nil || reply == nil {
				t.Fatal("destructive tool passed through")
			}
			if configured && (api.creates != 1 || api.runs["run_1"].SceneKey != "destructive") {
				t.Fatal("hint did not create a gated Run")
			}
			if !configured && api.requests != 0 {
				t.Fatal("missing scene should deny without HTTP")
			}
			p.config.Rules = []mcpProxyRule{{Name: "explicit", Scene: "explicit", Tools: []string{"nuke_*"}}}
			rule, _ := p.match("nuke_env", nil)
			if rule == nil || rule.Scene != "explicit" {
				t.Fatal("explicit rule did not take precedence")
			}
		})
	}
	p := newMCPProxy(mcpProxyConfig{DefaultAction: "deny"}, t.TempDir())
	if rule, reason := p.match("unlisted", nil); rule != nil || reason == "" {
		t.Fatal("default deny failed")
	}
	// Malformed tool-list contents must not turn a valid JSON-RPC frame into a transport error.
	_, _, _ = p.fromClient(context.Background(), []byte(`{"jsonrpc":"2.0","id":0,"method":"tools/list"}`))
	if err := p.observeDownstream([]byte(`{"jsonrpc":"2.0","id":0,"result":{"tools":"invalid"}}`)); err != nil {
		t.Fatal(err)
	}
}

type mcpPipeHarness struct {
	clientWrite *io.PipeWriter
	clientRead  *bufio.Reader
	downRead    *bufio.Reader
	downWrite   *io.PipeWriter
}

func newMCPPipeHarness(t *testing.T, p *mcpProxy) mcpPipeHarness {
	t.Helper()
	clientIn, clientWrite := io.Pipe()
	clientOut, clientResult := io.Pipe()
	downIn, downTarget := io.Pipe()
	downOut, downWrite := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.relay(ctx, clientIn, clientResult, downTarget, downOut, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		_ = clientWrite.Close()
		_ = clientIn.Close()
		_ = clientOut.Close()
		_ = clientResult.Close()
		_ = downIn.Close()
		_ = downTarget.Close()
		_ = downOut.Close()
		_ = downWrite.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("relay did not stop")
		}
	})
	return mcpPipeHarness{clientWrite, bufio.NewReader(clientOut), bufio.NewReader(downIn), downWrite}
}

func mcpWriteFrame(t *testing.T, w io.Writer, frame []byte) {
	t.Helper()
	if _, err := w.Write(append(frame, '\n')); err != nil {
		t.Fatal(err)
	}
}

func mcpReadFrame(t *testing.T, r *bufio.Reader) []byte {
	t.Helper()
	frame, err := readMCPFrame(r)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestMCPRelayApprovalRoundTrip(t *testing.T) {
	api, server := newMCPTestAPI(t)
	p := mcpDeleteProxy(t, server.URL)
	h := newMCPPipeHarness(t, p)
	list := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	mcpWriteFrame(t, h.clientWrite, list)
	if got := mcpReadFrame(t, h.downRead); !bytes.Equal(got, list) {
		t.Fatal("tools/list changed")
	}
	listed := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"delete_file","annotations":{"destructiveHint":true}}]}}`)
	mcpWriteFrame(t, h.downWrite, listed)
	if got := mcpReadFrame(t, h.clientRead); !bytes.Equal(got, listed) {
		t.Fatal("tools/list response changed")
	}
	mcpWriteFrame(t, h.clientWrite, mcpTestCall("blocked", "delete_file", map[string]any{"path": "a"}))
	if got := mcpReadFrame(t, h.clientRead); !bytes.Contains(got, []byte("run_1")) {
		t.Fatal("missing gate response")
	}
	api.allow("run_1")
	mcpWriteFrame(t, h.clientWrite, mcpTestCall("allowed", "delete_file", map[string]any{"path": "a", mcpRunIDArg: "run_1"}))
	forward := mcpReadFrame(t, h.downRead)
	if bytes.Contains(forward, []byte(mcpRunIDArg)) {
		t.Fatal("reserved argument reached downstream")
	}
	result := []byte(`{"jsonrpc":"2.0","id":"allowed","result":{"content":[{"type":"text","text":"deleted"}]}}`)
	mcpWriteFrame(t, h.downWrite, result)
	if got := mcpReadFrame(t, h.clientRead); !bytes.Equal(got, result) {
		t.Fatal("tool response changed")
	}
	// Large results cross the relay without Scanner's 64 KiB truncation.
	large := []byte(`{"jsonrpc":"2.0","method":"notifications/message","params":{"text":"` + strings.Repeat("x", 128<<10) + `"}}`)
	go func() { _, _ = h.downWrite.Write(append(large, '\n')) }()
	if got := mcpReadFrame(t, h.clientRead); !bytes.Equal(got, large) {
		t.Fatal("large frame truncated")
	}
}

func TestMCPRelayIgnoresBlankLines(t *testing.T) {
	p := newMCPProxy(mcpProxyConfig{}, t.TempDir())
	h := newMCPPipeHarness(t, p)
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if _, err := io.WriteString(h.clientWrite, " \t\r\n\n"); err != nil {
		t.Fatal(err)
	}
	mcpWriteFrame(t, h.clientWrite, request)
	if got := mcpReadFrame(t, h.downRead); !bytes.Equal(got, request) {
		t.Fatal("blank client lines changed or terminated the request stream")
	}
	response := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	if _, err := io.WriteString(h.downWrite, "\n \t\n"); err != nil {
		t.Fatal(err)
	}
	mcpWriteFrame(t, h.downWrite, response)
	if got := mcpReadFrame(t, h.clientRead); !bytes.Equal(got, response) {
		t.Fatal("blank downstream lines changed or terminated the response stream")
	}
}

func TestMCPRunCreationFailureGuidance(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			api, server := newMCPTestAPI(t)
			p := mcpDeleteProxy(t, server.URL)
			client, err := newRunClient(server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			p.client = client
			api.mu.Lock()
			api.status = http.StatusBadGateway
			if malformed {
				api.status, api.malformed = 0, true
			}
			api.mu.Unlock()
			message := mcpTestBlocked(t, p, map[string]any{"path": "a"})
			if strings.Contains(message, "--operation-id") || !strings.Contains(message, "operation ID: op_") || !strings.Contains(message, "original tool call without _guardrail_run_id") {
				t.Fatalf("unusable proxy recovery guidance: %s", message)
			}
			if len(p.issued) != 0 {
				t.Fatal("failed Run creation was registered")
			}
		})
	}
}

func TestMCPFrameBoundsAndPrivateDirectory(t *testing.T) {
	if _, err := readMCPFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", mcpFrameLimit+1) + "\n"))); err == nil {
		t.Fatal("oversized frame accepted")
	}
	frame, err := readMCPFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", mcpFrameLimit) + "\n")))
	if err != nil || len(frame) != mcpFrameLimit {
		t.Fatal("16 MiB frame rejected")
	}
	dir, cleanup, err := prepareMCPRunDir("")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0700 {
		t.Fatal("temporary directory is not private")
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("temporary directory not removed")
	}
	public := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareMCPRunDir(public); err == nil {
		t.Fatal("public run directory accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareMCPRunDir(link); err == nil {
		t.Fatal("symlink run directory accepted")
	}
	var out, diagnostic bytes.Buffer
	if err := runMCPProxy([]string{"--help"}, strings.NewReader(""), &out, &diagnostic); err != nil || out.Len() != 0 || !strings.Contains(diagnostic.String(), "--config") {
		t.Fatal("help polluted MCP stdout")
	}
	if err := runMCPProxy([]string{"--config", "policy.yaml", "child"}, strings.NewReader(""), &out, &diagnostic); err == nil {
		t.Fatal("missing -- accepted")
	}
}

// Reuse the test executable as a dependency-free downstream MCP process.
func TestMCPProxyChild(t *testing.T) {
	mode := os.Getenv("OPEN_GUARDRAIL_MCP_TEST_CHILD")
	if mode == "" {
		return
	}
	if mode == "fail" {
		os.Exit(17)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		frame, err := readMCPFrame(reader)
		if errors.Is(err, io.EOF) {
			os.Exit(0)
		}
		if err != nil {
			os.Exit(18)
		}
		if _, err := os.Stdout.Write(append(frame, '\n')); err != nil {
			os.Exit(19)
		}
	}
}

func TestMCPProxyProcessLifecycle(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPEN_GUARDRAIL_MCP_TEST_CHILD", "echo")
	frame := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	var out, diagnostic bytes.Buffer
	if err := runMCPProxy([]string{"--config", policy, "--", executable, "-test.run=^TestMCPProxyChild$"}, bytes.NewReader(append(frame, '\n')), &out, &diagnostic); err != nil {
		t.Fatalf("%v: stderr=%s stdout=%s", err, diagnostic.Bytes(), out.Bytes())
	}
	if !bytes.Equal(bytes.TrimSpace(out.Bytes()), frame) {
		t.Fatalf("stdout=%s stderr=%s", out.Bytes(), diagnostic.Bytes())
	}
	t.Setenv("OPEN_GUARDRAIL_MCP_TEST_CHILD", "fail")
	out.Reset()
	if err := runMCPProxy([]string{"--config", policy, "--", executable, "-test.run=^TestMCPProxyChild$"}, strings.NewReader(""), &out, &diagnostic); err == nil {
		t.Fatal("child failure was ignored")
	}
	if out.Len() != 0 {
		t.Fatal("child failure polluted stdout")
	}
}

func TestMCPProxyRealService(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DataDir: t.TempDir(), Database: config.Database{Driver: "sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	specs := specapp.New(store.Specs())
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = httpapi.New(store, httpapi.Options{Origin: "http://" + server.Listener.Addr().String(), ModelSpecs: specs, ModelRuns: runapp.New(store.Runs())})
	server.Start()
	defer server.Close()
	if err := run([]string{"scene", "apply", "--server", server.URL, "--key", "db-write-guard", "--name", "db-write-guard", "--spec", "../../integrations/mcp-proxy/db-write-guard.yaml", "--publish"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadMCPProxyConfig("../../integrations/mcp-proxy/example-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server = server.URL
	p := newMCPProxy(cfg, t.TempDir())
	forward, reply, err := p.fromClient(ctx, mcpTestCall(1, "execute_sql", map[string]any{"query": "DELETE FROM sessions WHERE id = 42"}))
	if err != nil || forward != nil || reply == nil {
		t.Fatalf("gate forward=%s reply=%s err=%v", forward, reply, err)
	}
	files, err := os.ReadDir(p.runDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("credential files=%v reply=%s err=%v", files, reply, err)
	}
	credentialPath := filepath.Join(p.runDir, files[0].Name())
	data, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	var credential credentialFile
	if err := json.Unmarshal(data, &credential); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"run", "revision", "--server", server.URL, credential.RunID}, io.Discard); err != nil {
		t.Fatal(err)
	}
	snapshot := rundomain.SubmissionSnapshot{FinishRun: true, Verdict: "PASS", Summary: "Synthetic scoped mutation verified", Checks: []rundomain.CheckSnapshot{{CheckID: "sql-safety", Verdict: "PASS", Summary: "Synthetic database scope and recovery verified", Evidence: []rundomain.Observation{
		{RequirementID: "sql-scope", ObservedAt: time.Now().UTC().Format(time.RFC3339), QuerySummary: "Synthetic target and authorization", ResultSummary: "Only session 42, transaction rollback available"},
		{RequirementID: "explain-summary", ObservedAt: time.Now().UTC().Format(time.RFC3339), QuerySummary: "Synthetic read-only plan inspection", ResultSummary: "Primary-key lookup; at most one row"},
	}}}}
	submission, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	submissionPath := filepath.Join(t.TempDir(), "submission.json")
	if err := os.WriteFile(submissionPath, submission, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"run", "submit", "--server", server.URL, "--submission-credential-file", credentialPath, "--submission", submissionPath, credential.RunID}, io.Discard); err != nil {
		t.Fatal(err)
	}
	forward, reply, err = p.fromClient(ctx, mcpTestCall(2, "execute_sql", map[string]any{"query": "DELETE FROM sessions WHERE id = 42", mcpRunIDArg: credential.RunID}))
	if err != nil || reply != nil || forward == nil || bytes.Contains(forward, []byte(mcpRunIDArg)) {
		t.Fatalf("retry forward=%s reply=%s err=%v", forward, reply, err)
	}
}

func TestMCPAmbiguousJSONAndExactIntegers(t *testing.T) {
	api, server := newMCPTestAPI(t)
	p := mcpDeleteProxy(t, server.URL)
	for _, frame := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","method":"ping","params":{"name":"delete_file","arguments":{"path":"a"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"a","path":"b"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_file","name":"ping","arguments":{}}}`,
	} {
		forward, _, err := p.fromClient(context.Background(), []byte(frame))
		if err == nil || forward != nil {
			t.Fatalf("ambiguous frame passed: %s", frame)
		}
	}
	for _, args := range []any{[]string{"a"}, "a", map[string]any{mcpRunIDArg: false}, map[string]any{mcpRunIDArg: ""}} {
		forward, reply, err := p.fromClient(context.Background(), mcpTestCall(1, "delete_file", args))
		if err != nil || forward != nil || reply == nil {
			t.Fatalf("invalid arguments passed: %s %s %v", forward, reply, err)
		}
	}
	if api.requests != 0 {
		t.Fatal("invalid calls made HTTP requests")
	}
	misleading := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","Method":"ping","params":{"name":"delete_file","arguments":{"path":"a"}}}`)
	forward, reply, err := p.fromClient(context.Background(), misleading)
	if err != nil || forward != nil || reply == nil || api.creates != 1 {
		t.Fatalf("case-sensitive method bypassed gate: %s %s %v", forward, reply, err)
	}
	large := map[string]any{"path": "a", "inode": json.Number("9007199254740993")}
	mcpTestBlocked(t, p, large)
	api.allow("run_2")
	large[mcpRunIDArg] = "run_2"
	forward, reply, err = p.fromClient(context.Background(), mcpTestCall(2, "delete_file", large))
	if err != nil || reply != nil || !bytes.Contains(forward, []byte("9007199254740993")) {
		t.Fatalf("integer changed: %s %s %v", forward, reply, err)
	}
	large["inode"] = json.Number("9007199254740992")
	mcpTestBlocked(t, p, large)
}

func TestMCPNoReuseAndInitialServiceFailure(t *testing.T) {
	api, server := newMCPTestAPI(t)
	p := mcpDeleteProxy(t, server.URL)
	p.config.Rules[0].ReuseValidRun = false
	mcpTestBlocked(t, p, map[string]any{"path": "a"})
	api.allow("run_1")
	for i := 0; i < 2; i++ {
		forward, reply, err := p.fromClient(context.Background(), mcpTestCall(i, "delete_file", map[string]any{"path": "a", mcpRunIDArg: "run_1"}))
		if err != nil || reply != nil || forward == nil {
			t.Fatal("valid Run rejected")
		}
	}
	if api.lookups != 2 {
		t.Fatal("reuseValidRun false skipped Run lookup")
	}
	mcpTestBlocked(t, p, map[string]any{"path": "a"})
	if api.creates != 2 {
		t.Fatal("reuseValidRun false reused cached ALLOW")
	}
	server.Close()
	p = mcpDeleteProxy(t, server.URL)
	mcpTestBlocked(t, p, map[string]any{"path": "a"})
	files, err := os.ReadDir(p.runDir)
	if err != nil || len(files) != 0 {
		t.Fatal("failed creation left credential files")
	}
}
