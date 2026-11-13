const message = document.querySelector("#message");
const panels = ["setup", "login", "home"];
let keyGeneration = 0;
function clearKey() {
  keyGeneration++;
  document.querySelector("#machine-key").value = "";
  document.querySelector("#key-result").hidden = true;
}
function show(name) {
  if (name !== "home") clearKey();
  for (const panel of panels) document.querySelector("#" + panel).hidden = panel !== name;
  if (window.ConsoleUI) window.ConsoleUI.setSignedIn(name === "home");
  if (window.SpecUI) window.SpecUI.setSignedIn(name === "home");
  if (window.RunUI) window.RunUI.setSignedIn(name === "home");
  window.AuditUI?.setSignedIn(name === "home");
  if (name === "home" && window.ConsoleUI) window.ConsoleUI.route();
}
async function api(path, method = "GET", body, token) {
  const response = await fetch(path, {
    method, credentials: "same-origin", redirect: "error",
    headers: { "Content-Type": "application/json", "X-Guardrail-CSRF": "1", ...(token ? { Authorization: "Bearer " + token } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  if (!response.ok) {
    const error = new Error("HTTP request failed");
    error.key = response.status === 401 ? "unauthorized"
      : response.status === 429 ? "loginRateLimited"
      : response.status === 409 ? "alreadyInitialized" : "requestFailed";
    error.status = response.status;
    throw error;
  }
  return response.status === 204 ? null : response.json();
}
async function refresh() {
  const principal = await api("/api/v1alpha1/auth/me");
  document.querySelector("#username").textContent = principal.user.username;
  document.querySelector("#project").textContent = principal.projectId;
  const [ready, version] = await Promise.all([
    api("/readyz").catch(() => ({ status: "unavailable" })),
    api("/api/v1alpha1/meta/version"),
  ]);
  setText(document.querySelector("#status"), ready.status === "ready" ? "ready" : "unavailable");
  document.querySelector("#version").textContent = version.version;
}
async function load() {
  try {
    const state = await api("/api/v1alpha1/bootstrap");
    if (!state.initialized) {
      const requiresToken = state.requiresToken !== false;
      document.querySelector("#setup-token").hidden = !requiresToken;
      const token = document.querySelector("#bootstrap-token");
      token.disabled = !requiresToken;
      token.required = requiresToken;
      show("setup"); setText(message, "setupRequired"); return;
    }
    await refresh();
    show("home"); setText(message, "signedIn");
  } catch (error) {
    show("login"); setText(message, error.status === 401 ? "loginPrompt" : error.key || "networkError");
  }
}
function validatePasswords(form, values, submitted = false) {
  const setup = form.id === "setup-form";
  const password = (setup ? values.password : values.newPassword) || "";
  const invalidLength = Array.from(password).length < 6 || new TextEncoder().encode(password).length > 128;
  const showLengthError = invalidLength && (submitted || password.length > 0);
  const hint = document.querySelector(setup ? "#setup-password-hint" : "#change-password-hint");
  const input = document.querySelector(setup ? "#setup-password" : "#change-password");
  setText(hint, showLengthError ? "passwordLength" : "passwordHint");
  hint.dataset.error = String(showLengthError);
  input.setAttribute("aria-invalid", String(showLengthError));
  let mismatch = false;
  if (setup) {
    mismatch = password !== (values.confirmPassword || "");
    const showMismatch = mismatch && (submitted || !!values.confirmPassword);
    const error = document.querySelector("#setup-confirm-error");
    error.hidden = !showMismatch;
    setText(error, "passwordMismatch");
    document.querySelector("#setup-confirm").setAttribute("aria-invalid", String(showMismatch));
  }
  return invalidLength ? "passwordLength" : mismatch ? "passwordMismatch" : "";
}
for (const id of ["setup-form", "password-form"]) {
  const form = document.querySelector("#" + id);
  form.addEventListener("input", () => validatePasswords(form, Object.fromEntries(new FormData(form))));
}

async function submit(form, action) {
  const button = form.querySelector("button");
  button.disabled = true;
  try {
    await action(Object.fromEntries(new FormData(form)));
    form.querySelectorAll('input[type="password"]').forEach(input => { input.value = ""; });
    await load();
  }
  catch (error) {
    if (form.id === "login-form" && error.status === 429) {
      form.querySelectorAll('input[type="password"]').forEach(input => { input.value = ""; });
    }
    if (form.id === "setup-form" && (error.status === 409 || !error.status && !error.key)) {
      // Another initializer or an uncertain response may already have completed setup.
      await load();
    } else { setText(message, error.key || "networkError"); }
  }
  finally { button.disabled = false; }
}
document.querySelector("#setup-form").addEventListener("submit", event => {
  event.preventDefault();
  return submit(event.currentTarget, async values => {
    const problem = validatePasswords(event.currentTarget, values, true);
    if (problem) throw { key: problem };
    await api("/api/v1alpha1/bootstrap", "POST",
      { username: values.username, password: values.password, signIn: true }, values.bootstrapToken?.trim());
  });
});
document.querySelector("#create-key").addEventListener("click", async event => {
  if (!window.confirm(t("confirmKeyCreation"))) return;
  const button = event.currentTarget;
  button.disabled = true;
  clearKey();
  const generation = keyGeneration;
  try {
    const result = await api("/api/v1alpha1/auth/api-keys", "POST", {});
    if (generation !== keyGeneration || document.querySelector("#home").hidden) return;
    document.querySelector("#machine-key").value = result.apiKey;
    document.querySelector("#key-result").hidden = false;
    setText(message, "keyCreated");
  } catch (error) {
    if (error.status === 401) show("login");
    setText(message, error.status ? error.key : "keyUncertain");
  } finally { button.disabled = false; }
});
document.querySelector("#hide-key").addEventListener("click", clearKey);
window.addEventListener("pagehide", clearKey);
document.querySelector("#login-form").addEventListener("submit", event => {
  event.preventDefault();
  return submit(event.currentTarget, values => api("/api/v1alpha1/auth/session", "POST", { ...values, kind: "browser" }));
});
document.querySelector("#password-form").addEventListener("submit", event => {
  event.preventDefault();
  return submit(event.currentTarget, values => {
    const problem = validatePasswords(event.currentTarget, values, true);
    if (problem) throw { key: problem };
    return api("/api/v1alpha1/auth/password", "PUT", values);
  });
});
document.querySelector("#logout").addEventListener("click", async () => {
  try { await api("/api/v1alpha1/auth/session", "DELETE"); }
  catch (error) { if (error.status !== 401) { setText(message, error.key || "networkError"); return; } }
  show("login"); setText(message, "signedOut");
});
document.querySelector("#refresh").addEventListener("click", load);
load();
