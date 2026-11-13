const {test} = require("node:test");
const assert = require("node:assert/strict");
const {readFileSync} = require("node:fs");
const vm = require("node:vm");
const html = readFileSync(__dirname + "/static/index.html", "utf8");
function page() {
  const element = () => ({value: "", hidden: false, disabled: false, textContent: "", children: [], handlers: {}, dataset: {},
    addEventListener(name, fn) {this.handlers[name] = fn;}, setAttribute() {}, removeAttribute() {},
    append(...children) {this.children.push(...children);}, replaceChildren(...children) {this.children = children;}});
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map(m => [m[1], element()]));
  const state = {page: {items: [{id: "8", eventType: "updated", actorId: "admin", occurredAt: 100, details: {before: "<script>bad</script>"}}], redacted: false, nextCursor: "older"}, response: null};
  const calls = [], downloads = [];
  const context = vm.createContext({document: {getElementById(id) {assert.ok(elements.has(id), id);return elements.get(id);}, createElement: element},
    window: {downloadGuardrailJSON(data) {downloads.push(data);}}, AbortSignal, URLSearchParams, Date,
    setText(node, key) {node.textContent = key;}, show() {context.window.AuditUI.setSignedIn(false);},
    fetch: async url => {calls.push(url);if (state.response) return state.response;
      const data = url.includes("redacted=true") ? {items: [{id: "8", actorRef: "digest"}], redacted: true} : state.page;
      return {ok: true, json: async () => data};},
  });
  vm.runInContext(readFileSync(__dirname + "/static/audit.js", "utf8"), context);
  context.window.AuditUI.setSignedIn(true);
  return {context, elements, state, calls, downloads};
}
const settle = () => new Promise(resolve => setImmediate(resolve));
test("audit paging preserves filters and renders historical content as text", async () => {
  const p = page();p.context.window.AuditUI.route(new URLSearchParams("actorId=admin"));await settle();
  assert.match(p.calls[0], /actorId=admin/);
  const content = p.elements.get("audit-rows").children[0].children[5].children[0].children[1];
  assert.match(content.textContent, /<script>bad<\/script>/);
  await p.elements.get("next-audit").handlers.click();assert.match(p.calls.at(-1), /cursor=older/);
  assert.equal(p.elements.get("previous-audit").disabled, false);
  await p.elements.get("previous-audit").handlers.click();assert.doesNotMatch(p.calls.at(-1), /cursor=/);
});
test("audit export uses server redaction and rejects an unsafe response", async () => {
  const p = page();p.context.window.AuditUI.route(new URLSearchParams());await settle();
  await p.elements.get("export-audit").handlers.click();assert.equal(p.downloads.length, 1);assert.match(p.calls.at(-1), /redacted=true/);
  p.state.response = {ok: true, json: async () => ({redacted: true, items: [{actorId: "private"}]})};
  await p.elements.get("export-audit").handlers.click();assert.equal(p.downloads.length, 1);assert.equal(p.elements.get("audit-message").textContent, "exportFailed");
});
test("audit logout and leaving the view discard delayed responses", async () => {
  for (const leave of [p => p.context.window.AuditUI.setSignedIn(false), p => p.context.window.AuditUI.leave()]) {
    const p = page();let resolve;p.state.response = new Promise(done => {resolve = done;});
    p.context.window.AuditUI.route(new URLSearchParams());leave(p);
    resolve({ok: true, json: async () => p.state.page});await settle();
    assert.equal(p.elements.get("audit-rows").children.length, 0);assert.equal(p.elements.get("export-audit").disabled, true);
  }
});
test("failed audit paging keeps the visible page and allows retry", async () => {
  const p = page();p.context.window.AuditUI.route(new URLSearchParams());await settle();
  p.state.response = {ok: false, status: 503, json: async () => ({})};await p.elements.get("next-audit").handlers.click();
  assert.equal(p.elements.get("audit-rows").children.length, 1);assert.equal(p.elements.get("previous-audit").disabled, true);
  assert.equal(p.elements.get("audit-message").textContent, "auditFailed");
});
test("invalid audit time range is explained without a network request", () => {
  const p = page();p.elements.get("audit-from").value = "2026-11-12T01:00";p.elements.get("audit-to").value = "2026-11-11T01:00";
  p.elements.get("audit-filters").handlers.submit({preventDefault() {}});
  assert.equal(p.calls.length, 0);assert.equal(p.elements.get("audit-message").textContent, "auditInvalidTime");
});

test("direct audit routes reject malformed times and retain a repairable range", async () => {
  for (const query of ["from=abc", "to=", "from=-1", "from=1.5", "from=1e3", "from=9999999999999999999", "from=2&to=1", "from=1&to=1"]) {
    const p = page();p.context.window.AuditUI.route(new URLSearchParams(query));await settle();
    assert.equal(p.calls.length, 0, query);assert.equal(p.elements.get("audit-message").textContent, "auditInvalidTime");
    assert.equal(p.elements.get("export-audit").disabled, true);
  }
  const p = page();p.context.window.AuditUI.route(new URLSearchParams("from=2&to=1"));
  assert.ok(p.elements.get("audit-from").value);assert.ok(p.elements.get("audit-to").value);
  p.elements.get("audit-from").value = "2026-11-11T00:00";p.elements.get("audit-to").value = "2026-11-12T00:00";
  p.elements.get("audit-filters").handlers.submit({preventDefault() {}});await settle();
  assert.equal(p.calls.length, 1);assert.equal(p.elements.get("audit-message").textContent, "");
});

test("audit forbidden responses keep the session available", async () => {
  const p = page();p.state.response = {ok:false,status:403,json:async()=>({})};
  p.context.window.AuditUI.route(new URLSearchParams());await settle();
  assert.equal(p.elements.get("audit-message").textContent,"auditForbidden");
  p.state.response = null;p.context.window.AuditUI.route(new URLSearchParams("from=0&to=1"));await settle();
  assert.equal(p.elements.get("audit-rows").children.length,1);
});
