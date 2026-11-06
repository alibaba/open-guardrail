const {test} = require("node:test");
const assert = require("node:assert/strict");
const {readFileSync} = require("node:fs");
const {join} = require("node:path");
const vm = require("node:vm");
const source = readFileSync(join(__dirname, "static/specs.js"), "utf8");
const html = readFileSync(join(__dirname, "static/index.html"), "utf8");
const settle = () => new Promise(resolve => setImmediate(resolve));

function page() {
  function element(id = "") {
    return {id, value: "", hidden: false, disabled: false, dataset: {}, handlers: {}, textContent: "", children: [],
      addEventListener(event, fn) { this.handlers[event] = fn; },
      removeAttribute(name) { if (name === "data-i18n") delete this.dataset.i18n; },
      replaceChildren(...items) { this.children = items; this.value = items[0]?.value || ""; },
      reset() { this.values = {}; },
    };
  }
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map(m => [m[1], element(m[1])]));
  const scene = {key: "recovery", name: "Recovery", defaultPolicyId: "pol_1", runTtlSeconds: 900};
  const policy = {id: "pol_1", sceneKey: "recovery", name: "default", lifecycleState: "ACTIVE", activeRevisionId: null};
  const state = {scene, policy, draft: {id: "rev_draft", policyId: policy.id, status: "DRAFT", yaml: "saved yaml", updatedBy: "admin", updatedAt: 1}, history: [], conflict: false, pending: null};
  const calls = [];
  let ctx;
  const context = {
    document: {
      documentElement: {lang: "en"},
      getElementById(id) { assert.ok(elements.has(id), id); return elements.get(id); },
      createElement: () => element(),
    },
    window: {confirm: () => true, addEventListener() {}},
    setText(el, key) { el.textContent = key; el.dataset.i18n = key; }, t: key => key,
    message: elements.get("message"),
    show(name) { if (name === "login") ctx.window.SpecUI.setSignedIn(false); },
    FormData: class { constructor(form) { return Object.entries(form.values || {}); } },
    AbortSignal, TextEncoder, Date, crypto: {randomUUID: () => "operation-1"},
    fetch: async (url, opts) => {
      calls.push({url, ...opts});
      if (state.pending) return state.pending;
      let status = 200, data;
      if (url === "/api/v1alpha1/scenes") data = [scene];
      else if (url === "/api/v1alpha1/scenes/recovery") data = scene;
      else if (url === "/api/v1alpha1/policies/pol_1") data = policy;
      else if (url.includes("/revisions?")) {
        const before = Number(url.match(/before=(\d+)/)?.[1] || 0);
        const eligible = state.history.filter(item => !before || item.revisionNumber < before);
        const items = eligible.slice(0, 20).map(({yaml, ...summary}) => summary);
        data = {items, ...(eligible.length > 20 ? {nextBefore: items.at(-1).revisionNumber} : {})};
      }
      else if (url.includes("/policy-revisions/")) data = state.history.find(item => url.endsWith("/" + item.id));
      else if (url.endsWith("/draft") && opts.method === "GET") {
        if (!state.draft) {status = 404; data = {message: "Draft not found"};} else data = state.draft;
      }
      else if (url.endsWith("/draft")) {
        if (state.conflict) { status = 409; data = {message: "conflict"}; }
        else { const input = JSON.parse(opts.body); state.draft ||= {id: "rev_draft_" + (state.history.length + 1), policyId: policy.id, status: "DRAFT", updatedBy: "admin", updatedAt: 1}; state.draft.yaml = input.yaml; data = state.draft; }
      } else if (url.endsWith("/draft:publish")) {
        if (JSON.parse(opts.body).yaml !== state.draft.yaml) { status = 409; data = {message: "draft changed"}; }
        else {
          const revision = {id: state.draft.id, policyId: policy.id, revisionNumber: state.history.length + 1, yaml: state.draft.yaml, digest: "abc", publishedAt: 1, publishedBy: "admin"};
          state.history.unshift(revision); state.draft = null; data = revision;
        }
      } else if (url.endsWith("/active-revision")) {
        policy.activeRevisionId = JSON.parse(opts.body).policyRevisionId; data = policy;
      } else data = scene;
      // Match HTTP deserialization: callers cannot mutate server-side fixtures.
      return {ok: status < 300, status, json: async () => JSON.parse(JSON.stringify(data))};
    },
  };
  ctx = vm.createContext(context);
  vm.runInContext(source, ctx);
  return {ctx, elements, state, calls};
}
test("draft edits disable publication; save and publish preserve history", async () => {
  const p = page(); p.ctx.window.SpecUI.setSignedIn(true); await settle();
  const input = p.elements.get("spec-yaml");
  assert.equal(input.value, "saved yaml");
  input.value = "edited yaml"; input.handlers.input();
  assert.equal(p.elements.get("publish-spec").disabled, true);
  await p.elements.get("save-spec").handlers.click();
  assert.equal(p.elements.get("publish-spec").disabled, false);
  assert.equal(p.state.policy.activeRevisionId, null);
  await p.elements.get("publish-spec").handlers.click();
  assert.equal(p.elements.get("spec-active").textContent, "v1");
  assert.equal(p.elements.get("revision-yaml").textContent, "edited yaml");
  const request = p.calls.find(c => c.url.endsWith("/active-revision"));
  assert.equal(JSON.parse(request.body).policyRevisionId, "rev_draft");
  input.value = "next edit"; input.handlers.input();
  assert.equal(p.elements.get("revision-yaml").textContent, "edited yaml");
});
test("conflicts retain the editor content and require an explicit reload", async () => {
  const p = page(); p.ctx.window.SpecUI.setSignedIn(true); await settle();
  p.state.conflict = true;
  const input = p.elements.get("spec-yaml"); input.value = "my changes"; input.handlers.input();
  await p.elements.get("save-spec").handlers.click();
  assert.equal(input.value, "my changes");
  assert.equal(p.elements.get("spec-message").textContent, "specConflict");
  assert.equal(p.elements.get("spec-controls").disabled, false);
});
test("publication cannot activate a Draft replaced by another session", async () => {
  const p = page(); p.ctx.window.SpecUI.setSignedIn(true); await settle();
  p.state.draft.yaml = "another session's YAML";
  await p.elements.get("publish-spec").handlers.click();
  const publication = p.calls.find(c => c.url.endsWith("/draft:publish"));
  assert.equal(JSON.parse(publication.body).yaml, "saved yaml");
  assert.equal(p.elements.get("spec-message").textContent, "specConflict");
  assert.equal(p.elements.get("spec-yaml").value, "saved yaml");
  assert.equal(p.state.history.length, 0);
  assert.equal(p.calls.some(c => c.url.endsWith("/active-revision")), false);
});
test("late requests cannot restore Spec content after logout", async () => {
  const p = page();
  let resolve;
  p.state.pending = new Promise(done => {resolve = done;});
  p.ctx.window.SpecUI.setSignedIn(true);
  p.ctx.window.SpecUI.setSignedIn(false);
  resolve({ok: true, status: 200, json: async () => [p.state.scene]});
  await settle();
  assert.equal(p.elements.get("spec-yaml").value, "");
  assert.equal(p.elements.get("scene-select").children.length, 0);
  assert.equal(p.elements.get("spec-editor").hidden, true);
});
test("oversized drafts do not send a request", async () => {
  const p = page(); p.ctx.window.SpecUI.setSignedIn(true); await settle();
  p.elements.get("spec-yaml").value = "中".repeat(30000);
  await p.elements.get("save-spec").handlers.click();
  assert.equal(p.calls.some(c => c.url.endsWith("/draft") && c.method === "PUT"), false);
  assert.equal(p.elements.get("spec-message").textContent, "specTooLarge");
});
test("failed scene selection keeps the selector consistent with the editor", async () => {
  const p = page(); p.ctx.window.SpecUI.setSignedIn(true); await settle();
  p.state.pending = Promise.resolve({ok: false, status: 503, json: async () => ({message: "unavailable"})});
  const selector = p.elements.get("scene-select");
  selector.value = "other";
  await selector.handlers.change();
  assert.equal(selector.value, "recovery");
  assert.equal(p.elements.get("spec-yaml").value, "saved yaml");
  assert.equal(p.elements.get("spec-message").textContent, "specRequestFailed");
});

test("history pages stay bounded across new publications and fetch only selected YAML", async () => {
  const p = page();
  p.state.history = Array.from({length: 45}, (_, index) => ({id: "rev_" + (45-index), policyId: "pol_1", revisionNumber: 45-index, yaml: "yaml-" + (45-index), digest: "abc", publishedAt: 1, publishedBy: "admin"}));
  p.state.policy.activeRevisionId = "rev_1"; // Active content need not be on the first page.
  p.ctx.window.SpecUI.setSignedIn(true); await settle();
  assert.equal(p.elements.get("revision-select").children.length,20);
  assert.equal(p.elements.get("spec-active").textContent,"v1");
  assert.equal(p.elements.get("revision-yaml").textContent,"yaml-45");
  assert.equal(p.calls.filter(c => c.url.includes("/policy-revisions/")).length,2);
  p.state.history.unshift({id:"rev_46", policyId:"pol_1", revisionNumber:46, yaml:"new", digest:"abc",publishedAt:1,publishedBy:"admin"});
  await p.elements.get("revisions-next").handlers.click();
  assert.equal(p.elements.get("revision-select").value,"25");
  assert.equal(p.elements.get("revision-select").children.length,20);
  await p.elements.get("revisions-next").handlers.click();
  assert.equal(p.elements.get("revision-select").value,"5");
  assert.equal(p.elements.get("revisions-next").disabled,true);
  await p.elements.get("revisions-previous").handlers.click();
  assert.equal(p.elements.get("revision-select").value,"25");
  assert.equal(p.elements.get("spec-yaml").value,"saved yaml");
});
test("late history detail cannot restore YAML after logout", async () => {
  const p=page();
  p.state.history=[{id:"rev_1",policyId:"pol_1",revisionNumber:1,yaml:"private yaml",digest:"abc",publishedAt:1,publishedBy:"admin"}];
  p.ctx.window.SpecUI.setSignedIn(true);await settle();
  let resolve;
  p.state.pending=new Promise(done=>{resolve=done;});
  const selected=p.elements.get("revision-select").handlers.change();
  p.ctx.window.SpecUI.setSignedIn(false);
  resolve({ok:true,status:200,json:async()=>p.state.history[0]});await selected;
  assert.equal(p.elements.get("revision-yaml").textContent,"");
});

test("active version on the current page reuses its summary", async () => {
  const p = page();
  p.state.history=[{id:"rev_1",policyId:"pol_1",revisionNumber:1,yaml:"yaml",digest:"abc",publishedAt:1,publishedBy:"admin"}];
  p.state.policy.activeRevisionId="rev_1";
  p.ctx.window.SpecUI.setSignedIn(true);await settle();
  assert.equal(p.elements.get("spec-active").textContent,"v1");
  assert.equal(p.elements.get("revision-yaml").textContent,"yaml");
  assert.equal(p.calls.filter(c=>c.url.includes("/policy-revisions/")).length,1);
});
test("revision details show loading, then content or an actionable failure", async () => {
  const p=page();
  const revision={id:"rev_1",policyId:"pol_1",revisionNumber:1,yaml:"yaml",digest:"abc",publishedAt:1,publishedBy:"admin"};
  p.state.history=[revision];
  p.ctx.window.SpecUI.setSignedIn(true);await settle();
  let resolve;
  p.state.pending=new Promise(done=>{resolve=done;});
  const success=p.elements.get("revision-select").handlers.change();
  assert.equal(p.elements.get("revision-meta").textContent,"revisionLoading");
  assert.equal(p.elements.get("revision-yaml").textContent,"");
  resolve({ok:true,status:200,json:async()=>revision});await success;
  assert.match(p.elements.get("revision-meta").textContent,/v1/);
  assert.equal(p.elements.get("revision-meta").dataset.i18n,undefined);
  p.state.pending=new Promise(done=>{resolve=done;});
  const failure=p.elements.get("revision-select").handlers.change();
  assert.equal(p.elements.get("revision-meta").textContent,"revisionLoading");
  resolve({ok:false,status:503,json:async()=>({message:"unavailable"})});await failure;
  assert.equal(p.elements.get("revision-meta").textContent,"revisionLoadFailed");
  assert.equal(p.elements.get("revision-yaml").textContent,"");
  p.ctx.window.SpecUI.setSignedIn(false);
  assert.equal(p.elements.get("revision-meta").textContent,"");
  assert.equal(p.elements.get("revision-meta").dataset.i18n,undefined);
});
