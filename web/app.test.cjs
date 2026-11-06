const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const vm = require("node:vm");
const html = readFileSync(join(__dirname, "static/index.html"), "utf8");
const source = readFileSync(join(__dirname, "static/app.js"), "utf8");

function page(requiresToken = false) {
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map(match => [match[1], {
    id: match[1], dataset: {}, attributes: {}, hidden: true, value: "", handlers: {}, button: {}, values: {}, secrets: [{value: "secret"}],
    addEventListener(event, fn) { this.handlers[event] = fn; },
    setAttribute(name, value) { this.attributes[name] = value; },
    querySelector() { return this.button; },
    querySelectorAll() { return this.secrets; },
  }]));
  const calls = [];
  const state = { initialized: false, loggedIn: false };
  const context = vm.createContext({
    document: { querySelector(selector) {
      const element = elements.get(selector.slice(1));
      if (!element) throw Error("Missing HTML element: " + selector);
      return element;
    } },
    window: { confirm: () => true, addEventListener() {} },
    FormData: class { constructor(form) { return Object.entries(form.values); } },
    TextEncoder, AbortSignal,
    t: key => key,
    setText: (element, key) => { element.textContent = key; },
    fetch: async (path, options) => {
      calls.push({path, ...options});
      let status = 200, data = {};
      if (path.endsWith("/bootstrap")) {
        if (options.method === "POST") { state.initialized = true; state.loggedIn = JSON.parse(options.body).signIn === true; status = 201; data = {user: {id: "admin"}}; }
        else data = {initialized: state.initialized, requiresToken};
      } else if (path.endsWith("/me")) {
        status = state.loggedIn ? 200 : 401;
        data = {user: {username: "admin"}, projectId: "default"};
      } else if (path.endsWith("/session")) {
        state.loggedIn = options.method === "POST" && !state.rateLimited;
        status = options.method === "POST" ? (state.rateLimited ? 429 : 201) : 204;
      } else if (path.endsWith("/api-keys")) {
        status = state.loggedIn ? 201 : 401;
        data = {apiKey: "ogk_synthetic", role: "RUN_ORCHESTRATOR"};
      } else if (path === "/readyz") data = {status: "ready"};
      else data = {version: "test"};
      return {ok: status < 300, status, json: async () => data};
    },
  });
  vm.runInContext(source, context);
  return {elements, calls, state, context};
}
const settle = () => new Promise(resolve => setImmediate(resolve));

test("remote setup sends token, clears secrets and automatically shows home", async () => {
  const p = page(true);
  await settle();
  assert.equal(p.elements.get("setup").hidden, false);
  const form = p.elements.get("setup-form");
  form.values = {bootstrapToken: "ogb_synthetic", username: "admin",
    password: "synthetic-password", confirmPassword: "synthetic-password"};
  await form.handlers.submit({preventDefault() {}, currentTarget: form});
  const request = p.calls.find(call => call.method === "POST");
  assert.equal(request.path, "/api/v1alpha1/bootstrap");
  assert.equal(request.headers.Authorization, "Bearer ogb_synthetic");
  assert.deepEqual(JSON.parse(request.body), {username: "admin", password: "synthetic-password", signIn: true});
  assert.equal(p.elements.get("home").hidden, false);
  assert.equal(form.secrets[0].value, "");
  assert.equal(form.button.disabled, false);
  assert.equal(p.calls.some(call => call.path.endsWith("/api-keys")), false);
});

test("password mismatch stops before sending initialization", async () => {
  const p = page();
  await settle();
  const form = p.elements.get("setup-form");
  form.values = {password: "synthetic-password", confirmPassword: "different"};
  await form.handlers.submit({preventDefault() {}, currentTarget: form});
  assert.equal(p.calls.some(call => call.method === "POST"), false);
  assert.equal(p.elements.get("message").textContent, "passwordMismatch");
  assert.equal(form.secrets[0].value, "secret");
  assert.equal(p.elements.get("setup-confirm-error").hidden, false);
});

test("short password shows inline feedback while typing and survives failed submission", async () => {
  const p = page();
  await settle();
  const form = p.elements.get("setup-form");
  form.values = {username: "admin", password: "123", confirmPassword: "123"};
  form.handlers.input();
  assert.equal(p.elements.get("setup-password-hint").textContent, "passwordLength");
  assert.equal(p.elements.get("setup-password").attributes["aria-invalid"], "true");
  const before = p.calls.length;
  await form.handlers.submit({preventDefault() {}, currentTarget: form});
  assert.equal(p.calls.length, before);
  assert.equal(form.secrets[0].value, "secret");
  assert.equal(form.values.password, "123");
  assert.equal(p.elements.get("setup").hidden, false);
  form.values.password = form.values.confirmPassword = "123456";
  form.handlers.input();
  assert.equal(p.elements.get("setup-password-hint").textContent, "passwordHint");
  assert.equal(p.elements.get("setup-password").attributes["aria-invalid"], "false");
  await form.handlers.submit({preventDefault() {}, currentTarget: form});
  assert.equal(p.elements.get("home").hidden, false);
  assert.equal(form.secrets[0].value, "");
});

test("password policy counts Unicode characters and enforces byte maximum", async () => {
  const p = page();
  await settle();
  const form = p.elements.get("setup-form");
  for (const [password, valid] of [["中文密", false], ["中文密码测试", true], ["😀".repeat(6), true], ["a".repeat(128), true], ["a".repeat(129), false], ["😀".repeat(33), false]]) {
    form.values = {password, confirmPassword: password};
    form.handlers.input();
    assert.equal(p.elements.get("setup-password").attributes["aria-invalid"], String(!valid));
  }
});

test("change password uses the same live hint and preserves input on validation failure", async () => {
  const p = page();
  await settle();
  const form = p.elements.get("password-form");
  form.values = {currentPassword: "synthetic-password", newPassword: "123"};
  form.handlers.input();
  assert.equal(p.elements.get("change-password-hint").textContent, "passwordLength");
  const before = p.calls.length;
  await form.handlers.submit({preventDefault() {}, currentTarget: form});
  assert.equal(p.calls.length, before);
  assert.equal(form.secrets[0].value, "secret");
});

test("local setup hides token and needs only account credentials", async () => {
  const p = page();
  await settle();
  assert.equal(p.elements.get("setup-token").hidden, true);
  assert.equal(p.elements.get("bootstrap-token").disabled, true);
  assert.equal(p.elements.get("bootstrap-token").required, false);
  assert.equal(p.elements.has("check-setup"), false);
  const form = p.elements.get("setup-form");
  form.values = {username: "admin", password: "synthetic-password", confirmPassword: "synthetic-password"};
  await form.handlers.submit({preventDefault() {}, currentTarget: form});
  const request = p.calls.find(call => call.method === "POST");
  assert.equal(request.headers.Authorization, undefined);
  assert.equal(p.elements.get("home").hidden, false);
  assert.equal(p.calls.some(call => call.path.endsWith("/session")), false);
});

test("remote setup requires a token field", async () => {
  const p = page(true);
  await settle();
  assert.equal(p.elements.get("setup-token").hidden, false);
  assert.equal(p.elements.get("bootstrap-token").disabled, false);
  assert.equal(p.elements.get("bootstrap-token").required, true);
});

test("logged-in key creation is explicit and logout clears the displayed key", async () => {
  const p = page();
  await settle();
  p.state.initialized = p.state.loggedIn = true;
  await vm.runInContext("load()", p.context);
  assert.equal(p.calls.some(call => call.path.endsWith("/api-keys")), false);
  const button = p.elements.get("create-key");
  await button.handlers.click({currentTarget: button});
  assert.equal(p.elements.get("machine-key").value, "ogk_synthetic");
  assert.equal(p.elements.get("key-result").hidden, false);
  await p.elements.get("logout").handlers.click();
  assert.equal(p.elements.get("machine-key").value, "");
  assert.equal(p.elements.get("key-result").hidden, true);
});

test("temporary login backoff gives retry guidance and clears the submitted password", async () => {
  const p=page();await settle();p.state.rateLimited=true;
  const form=p.elements.get("login-form");form.values={username:"admin",password:"synthetic-password"};
  await form.handlers.submit({preventDefault(){},currentTarget:form});
  assert.equal(p.elements.get("message").textContent,"loginRateLimited");
  assert.equal(form.secrets[0].value,"");
  assert.equal(p.state.loggedIn,false);
});
