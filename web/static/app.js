const message = document.querySelector("#message");

async function api(path, method = "GET", body) {
  const response = await fetch(path, {
    method, credentials: "omit", redirect: "error",
    headers: { "Content-Type": "application/json", "X-Guardrail-CSRF": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  if (!response.ok) {
    const error = new Error("HTTP request failed");
    error.key = "requestFailed";
    error.status = response.status;
    throw error;
  }
  return response.status === 204 ? null : response.json();
}

async function refresh() {
  try {
    const [ready, version] = await Promise.all([
      api("/readyz").catch(() => ({ status: "unavailable" })),
      api("/api/v1alpha1/meta/version"),
    ]);
    setText(document.querySelector("#status"), ready.status === "ready" ? "ready" : "unavailable");
    document.querySelector("#version").textContent = version.version;
    message.textContent = "";
    message.removeAttribute("data-i18n");
  } catch (error) {
    setText(message, error.key || "networkError");
  }
}

document.querySelector("#home").hidden = false;
window.ConsoleUI?.setAvailable(true);
window.SpecUI?.setAvailable(true);
window.RunUI?.setAvailable(true);
window.AuditUI?.setAvailable(true);
window.ConsoleUI?.route();
document.querySelector("#refresh").addEventListener("click", refresh);
refresh();
