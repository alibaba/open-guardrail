const {test} = require("node:test");
const assert = require("node:assert/strict");
const {readFileSync} = require("node:fs");
const {join} = require("node:path");
const vm = require("node:vm");
const source = readFileSync(join(__dirname, "static/runs.js"), "utf8");
const html = readFileSync(join(__dirname, "static/index.html"), "utf8");
const settle = () => new Promise(resolve => setImmediate(resolve));

function page() {
  function element(id = "") {
    return {id, value: "", hidden: false, disabled: false, textContent: "", className: "", dataset: {}, handlers: {}, children: [],
      addEventListener(event, fn) { this.handlers[event] = fn; },
      setAttribute(name, value) { this[name] = value; },
      removeAttribute(name) { if (name === "data-i18n") delete this.dataset.i18n; else delete this[name]; },
      append(...items) { this.children.push(...items); },
      replaceChildren(...items) { this.children = items; },
    };
  }
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map(m => [m[1], element(m[1])]));
  const run = {id: "run_abcdefghijklmnop", sceneKey: "recovery", policyId: "pol_1", policyRevisionId: "rev_1", specDigest: "a".repeat(64), status: "FINISHED",
    result: "PASS", decision: "ALLOW", createdAt: 10, deadlineAt: 20, finishedAt: 12, decisionExpiresAt: 30,
    changeSet: {target: "app-a"}, latestSubmissionNo: 1, finalSubmissionId: "sub_abcdefghijklmnop"};
  const calls = [];
  const submission = {id: run.finalSubmissionId, runId: run.id, submissionNo: 1, verdict: "PASS", summary: "Healthy", createdAt: 12, finishRun: true,
    snapshot: {checks: [{checkId: "readiness", verdict: "PASS", summary: "All replicas ready", evidence: [{requirementId: "health", observedAt: "2026-11-03T02:16:27Z", querySummary: "Read fixture", resultSummary: '{"readyReplicas":3}', reference: "fixture:healthy"}]}]}};
  const state = {submissions: {[submission.id]: submission}, evidenceResponse: null, historyResponse: null, pending: null, page: {items: [run], nextCursor: "next-page"}};
  let ctx;
  const context = {
    document: {
      documentElement: {lang: "en"},
      getElementById(id) { assert.ok(elements.has(id), id); return elements.get(id); },
      createElement: () => element(),
      querySelectorAll() { return []; },
    },
    window: {scrollTo() {}},
    setText(node, key) { node.textContent = key; node.dataset.i18n = key; },
    show(name) { if (name === "login") ctx.window.RunUI.setSignedIn(false); },
    AbortSignal, Date, JSON, URLSearchParams, encodeURIComponent,
    fetch: async url => {
      calls.push(url);
      if (url.endsWith("/report")) return state.reportResponse || {ok:true, json:async()=>({format:"openguardrail.run-report.v1",run:{id:run.id}})};
      if (url.includes('/submissions/') && state.evidenceResponse) return typeof state.evidenceResponse === 'function' ? state.evidenceResponse(url) : state.evidenceResponse;
      if (url.includes('/submissions?') && state.historyResponse) return state.historyResponse;
      if (state.pending) return state.pending;
      const data = url.includes('/submissions?') ? (state.history || {items: Object.values(state.submissions).sort((a,b) => b.submissionNo-a.submissionNo).map(({snapshot,...metadata})=>metadata)}) : url.includes('/submissions/') ? state.submissions[url.split('/').at(-1)] : url.includes("/policy-revisions/") ? {id: "rev_1", revisionNumber: 1, yaml: "spec yaml", digest: run.specDigest} : url.includes("/runs/") ? run : state.page;
      return {ok: true, status: 200, json: async () => JSON.parse(JSON.stringify(data))};
    },
  };
  ctx = vm.createContext(context);
  vm.runInContext(source, ctx);
  return {ctx, elements, calls, state, run, submission};
}

test("filters and cursor navigation use management Run endpoints", async () => {
  const p = page(); p.ctx.window.RunUI.setSignedIn(true); p.ctx.window.RunUI.show(); await settle();
  assert.equal(p.elements.get("run-rows").children.length, 1);
  assert.equal(p.elements.get("next-runs").disabled, false);
  p.elements.get("run-scene-filter").value = "recovery";
  p.elements.get("run-status-filter").value = "FINISHED";
  await p.elements.get("apply-run-filters").handlers.click();
  assert.ok(p.calls.at(-1).includes("sceneKey=recovery"));
  assert.ok(p.calls.at(-1).includes("status=FINISHED"));
  await p.elements.get("next-runs").handlers.click();
  assert.ok(p.calls.at(-1).includes("cursor=next-page"));
  assert.equal(p.elements.get("previous-runs").disabled, false);
});

test("filters become a reloadable route when the console router is present", async () => {
  const p = page(), routes = [];
  p.ctx.window.ConsoleUI = {go(path) { routes.push(path); }};
  p.ctx.window.RunUI.setSignedIn(true);
  p.elements.get("run-scene-filter").value = "release.recovery";
  p.elements.get("run-status-filter").value = "FINISHED";
  p.elements.get("apply-run-filters").handlers.click();
  assert.equal(routes[0], "runs?sceneKey=release.recovery&status=FINISHED");
});

test("empty results show one empty state without pagination", async () => {
  const p = page(); p.state.page = {items: [], nextCursor: ""};
  p.ctx.window.RunUI.setSignedIn(true); p.ctx.window.RunUI.show(); await settle();
  assert.equal(p.elements.get("run-empty").hidden, false);
  assert.equal(p.elements.get("run-pagination").hidden, true);
  assert.equal(p.elements.get("run-message").textContent, "");
  assert.equal(Object.hasOwn(p.elements.get("run-message").dataset, "i18n"), false);
});

test("failed cursor navigation keeps the displayed page state", async () => {
  const p = page(); p.ctx.window.RunUI.setSignedIn(true); p.ctx.window.RunUI.show(); await settle();
  p.state.pending = Promise.resolve({ok: false, status: 503, json: async () => ({})});
  await p.elements.get("next-runs").handlers.click();
  assert.equal(p.elements.get("previous-runs").disabled, true);
  assert.equal(p.elements.get("run-rows").children.length, 1);
  assert.equal(p.elements.get("run-message").textContent, "runRequestFailed");
  p.state.pending = null;
  await p.elements.get("next-runs").handlers.click();
  assert.equal(p.elements.get("previous-runs").disabled, false);
});

test("detail renders stored JSON as text and returns to the list", async () => {
  const p = page(); p.ctx.window.RunUI.setSignedIn(true); p.ctx.window.RunUI.show(); await settle();
  const row = p.elements.get("run-rows").children[0];
  row.children.at(-1).children[0].handlers.click(); await settle();
  assert.equal(p.elements.get("run-list-view").hidden, true);
  assert.equal(p.elements.get("run-detail-id").textContent, p.run.id);
  assert.equal(p.elements.get("run-detail-change-set").textContent, JSON.stringify(p.run.changeSet, null, 2));
  assert.equal(p.elements.get("run-detail-spec-yaml").textContent, "spec yaml");
  p.elements.get("back-runs").handlers.click();
  assert.equal(p.elements.get("run-list-view").hidden, false);
});

test("late list responses cannot restore Run data after logout", async () => {
  const p = page();
  let resolve;
  p.state.pending = new Promise(done => { resolve = done; });
  p.ctx.window.RunUI.setSignedIn(true); p.ctx.window.RunUI.show();
  p.ctx.window.RunUI.setSignedIn(false);
  assert.equal(Object.hasOwn(p.elements.get("run-message").dataset, "i18n"), false);
  resolve({ok: true, status: 200, json: async () => ({items: [p.run]})});
  await settle();
  assert.equal(p.elements.get("run-rows").children.length, 0);
  assert.equal(p.elements.get("run-detail-id").textContent, "");
});

function content(node) { return [node.textContent, ...node.children.map(content)].join("\n"); }
function historyFixture(p) {
  const older = {...p.submission, id: 'sub_older', submissionNo: 1, verdict: 'UNKNOWN', summary: 'Need more samples', finishRun: false};
  p.submission.submissionNo = 2; p.run.latestSubmissionNo = 2; p.state.submissions[older.id] = older; return older;
}

test("latest evidence is read through the administrator API and rendered as literal text", async () => {
  const p=page(); p.submission.snapshot.checks[0].summary='<img src=x onerror=alert(1)>';
  p.ctx.window.RunUI.setSignedIn(true); await p.ctx.window.RunUI.open(p.run.id); await settle();
  assert.ok(p.calls.includes('/api/v1alpha1/runs/'+p.run.id+'/submissions/'+p.submission.id));
  assert.equal(p.elements.get('run-submission-summary').textContent, 'Healthy');
  const checks=content(p.elements.get('run-checks'));
  for(const value of ['readiness','PASS','<img src=x onerror=alert(1)>','health','Read fixture','"readyReplicas": 3','fixture:healthy']) assert.ok(checks.includes(value),value);
  function noHTML(node){ assert.equal(Object.hasOwn(node,'innerHTML'),false); node.children.forEach(noHTML); } noHTML(p.elements.get('run-checks'));
  assert.equal(JSON.parse(p.elements.get('run-submission-json').textContent).id,p.submission.id);
});

test("history selection stays local, preserves final decision, and refresh reselects latest", async () => {
  const p=page(),older=historyFixture(p);p.ctx.window.RunUI.setSignedIn(true);
  await p.ctx.window.RunUI.open(p.run.id);await settle();
  assert.equal(p.elements.get('run-detail-submission').textContent,'#2');
  assert.equal(p.calls.some(url=>url.endsWith(older.id)),false);
  p.elements.get('run-submission-select').value=older.id;
  await p.elements.get('run-submission-select').handlers.change();
  assert.equal(p.elements.get('run-submission-summary').textContent,older.summary);
  assert.equal(p.elements.get('run-history-note').hidden,false);
  assert.equal(p.elements.get('run-detail-decision').textContent,'ALLOW');
  await p.elements.get('refresh-run').onclick();await settle();
  assert.equal(p.elements.get('run-detail-submission').textContent,'#2');
  assert.equal(p.elements.get('run-history-note').hidden,true);
});

test("history follows bounded server cursors without eagerly fetching evidence", async () => {
  const p=page(),older=historyFixture(p);p.state.history={items:[p.submission],nextBefore:2};
  p.ctx.window.RunUI.setSignedIn(true);await p.ctx.window.RunUI.open(p.run.id);await settle();
  assert.ok(p.calls.some(url=>url.includes('submissions?limit=20&before=3')));
  p.state.history={items:[older]};await p.elements.get('older-submissions').handlers.click();
  assert.ok(p.calls.some(url=>url.includes('submissions?limit=20&before=2')));
  assert.equal(p.elements.get('run-submission-summary').textContent,older.summary);
  assert.equal(p.elements.get('older-submissions').hidden,true);
  assert.equal(p.elements.get('newer-submissions').hidden,false);
});

test("late snapshot responses cannot replace a newer selection", async () => {
  const p=page(),older=historyFixture(p);let resolve;p.ctx.window.RunUI.setSignedIn(true);
  await p.ctx.window.RunUI.open(p.run.id);await settle();
  p.state.evidenceResponse=url=>url.endsWith(older.id)?new Promise(done=>{resolve=done;}):Promise.resolve({ok:true,json:async()=>p.submission});
  p.elements.get('run-submission-select').value=older.id;
  const pending=p.elements.get('run-submission-select').handlers.change();await settle();
  p.elements.get('run-submission-select').value=p.submission.id;await p.elements.get('run-submission-select').handlers.change();
  resolve({ok:true,json:async()=>older});await pending;
  assert.equal(p.elements.get('run-submission-summary').textContent,p.submission.summary);
});

test("failed evidence reads can retry and reject snapshots from a different Run", async () => {
  const p=page();p.state.evidenceResponse=Promise.resolve({ok:false,status:503,json:async()=>({})});
  p.ctx.window.RunUI.setSignedIn(true);await p.ctx.window.RunUI.open(p.run.id);await settle();
  assert.equal(p.elements.get('retry-run-submission').hidden,false);
  assert.equal(p.elements.get('run-detail-content').hidden,false);
  p.state.evidenceResponse=null;await p.elements.get('retry-run-submission').handlers.click();
  assert.equal(p.elements.get('run-submission-content').hidden,false);
  p.submission.runId='run_other';await p.elements.get('refresh-run').onclick();await settle();
  assert.equal(p.elements.get('run-submission-content').hidden,true);
  assert.equal(p.elements.get('run-submission-json').textContent,'');
});

test("empty Runs need no history request; OPEN intermediate submissions are inspectable", async () => {
  const p=page();p.run.latestSubmissionNo=0;p.run.finalSubmissionId=null;p.run.status='OPEN';
  p.ctx.window.RunUI.setSignedIn(true);await p.ctx.window.RunUI.open(p.run.id);await settle();
  assert.equal(p.elements.get('run-submission-state').textContent,'submissionPending');
  assert.equal(p.calls.some(url=>url.includes('/submissions')),false);
  p.run.latestSubmissionNo=1;p.submission.finishRun=false;await p.ctx.window.RunUI.open(p.run.id);await settle();
  assert.equal(p.elements.get('run-submission-content').hidden,false);
  assert.equal(p.elements.get('run-history-note').hidden,false);
});

test("leaving the page or logging out prevents late evidence from restoring content", async () => {
  for(const leave of ['logout','list','other-view']){
    const p=page();let resolve;p.state.evidenceResponse=new Promise(done=>{resolve=done;});
    p.ctx.window.RunUI.setSignedIn(true);await p.ctx.window.RunUI.open(p.run.id);await settle();
    if(leave==='logout')p.ctx.window.RunUI.setSignedIn(false);else if(leave==='list')p.elements.get('back-runs').handlers.click();else p.ctx.window.RunUI.leave();
    resolve({ok:true,json:async()=>p.submission});await settle();
    assert.equal(p.elements.get('run-submission-content').hidden,true);
    assert.equal(p.elements.get('run-submission-json').textContent,'');
  }
});

test("failed or superseded detail navigation cannot display the previous decision", async () => {
  const p=page();p.ctx.window.RunUI.setSignedIn(true);await p.ctx.window.RunUI.open(p.run.id);await settle();
  p.state.pending=Promise.resolve({ok:false,status:404,json:async()=>({})});
  await p.ctx.window.RunUI.open('run_missing');assert.equal(p.elements.get('run-detail-content').hidden,true);
  assert.equal(p.elements.get('run-detail-message').textContent,'runDetailFailed');
  let resolve;p.state.pending=new Promise(done=>{resolve=done;});const older=p.ctx.window.RunUI.open(p.run.id);await settle();
  p.state.pending=null;await p.ctx.window.RunUI.open(p.run.id);await settle();resolve({ok:true,json:async()=>({...p.run,id:'run_stale'})});await older;
  assert.equal(p.elements.get('run-detail-id').textContent,p.run.id);
});


test("Run report download checks identity and leaves no download after navigation", async () => {
  const p = page(), downloads = [];
  p.ctx.window.downloadGuardrailJSON = (data, name) => downloads.push({data, name});
  p.ctx.window.RunUI.setSignedIn(true); await p.ctx.window.RunUI.open(p.run.id); await settle();
  assert.equal(p.elements.get("export-run-report").hidden, false);
  await p.elements.get("export-run-report").handlers.click();
  assert.equal(downloads.length, 1); assert.match(p.calls.at(-1), /\/report$/);
  p.state.reportResponse = {ok:true, json:async()=>({format:"openguardrail.run-report.v1",run:{id:"another"}})};
  await p.elements.get("export-run-report").handlers.click();assert.equal(downloads.length, 1);
  let resolve; p.state.reportResponse = new Promise(done => {resolve = done;});
  const pending = p.elements.get("export-run-report").handlers.click();p.ctx.window.RunUI.leave();
  resolve({ok:true,json:async()=>({format:"openguardrail.run-report.v1",run:{id:p.run.id}})});await pending;
  assert.equal(downloads.length, 1);assert.equal(p.elements.get("export-run-report").hidden, true);
});
