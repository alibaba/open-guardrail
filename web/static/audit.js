// Project administrator audit reader. Exports are fetched from the redacted API.
(() => {
  const el = id => document.getElementById(id);
  const fields = ["eventType", "actorId", "resourceType", "resourceId", "from", "to"];
  let signedIn = false, generation = 0, busy = false, loaded = false;
  let filters = new URLSearchParams(), cursors = [""], next = "";
  function message(key) { setText(el("audit-message"), key); }
  function buttons() {
    el("audit-surface").setAttribute("aria-busy", String(busy));
    for (const id of ["apply-audit", "export-audit", "previous-audit", "next-audit"]) el(id).disabled = busy;
    el("previous-audit").disabled = busy || cursors.length < 2;
    el("next-audit").disabled = busy || !next;
    el("export-audit").disabled = busy || !loaded;
  }
  function reset() {
    generation++; busy = false; loaded = false; next = ""; cursors = [""];
    el("audit-rows").replaceChildren(); el("audit-empty").hidden = true;
    el("audit-message").textContent = ""; el("audit-message").removeAttribute("data-i18n"); buttons();
  }
  async function request(params) {
    const response = await fetch("/api/v1alpha1/audit-events?" + params, {
      credentials: "same-origin", redirect: "error", signal: AbortSignal.timeout(15000),
      headers: {"X-Guardrail-CSRF": "1"},
    });
    const page = await response.json();
    if (!response.ok) throw {status: response.status};
    if (!Array.isArray(page.items)) throw {};
    return page;
  }
  function validTimestamp(value) {
    if (!/^\d+$/.test(value)) return false;
    const seconds = Number(value), date = new Date(seconds * 1000);
    return Number.isSafeInteger(seconds) && Number.isFinite(date.valueOf()) && date.getFullYear() <= 9999;
  }
  function validTimeRange(params) {
    for (const name of ["from", "to"]) {
      if (params.has(name) && !validTimestamp(params.get(name))) return false;
    }
    return !params.has("from") || !params.has("to") || Number(params.get("from")) < Number(params.get("to"));
  }
  function parameters(cursor, redacted = false) {
    const query = new URLSearchParams(filters); query.set("limit", "20"); query.set("redacted", String(redacted));
    if (cursor) query.set("cursor", cursor);
    return query;
  }
  function render(items) {
    el("audit-empty").hidden = items.length !== 0;
    el("audit-rows").replaceChildren(...items.map(item => {
      const row = document.createElement("tr");
      for (const value of [new Date(item.occurredAt * 1000).toLocaleString(), item.eventType, item.actorId, item.resourceType, item.resourceId]) {
        const cell = document.createElement("td"); cell.textContent = value || "—"; row.append(cell);
      }
      const cell = document.createElement("td");
      if (item.details) {
        const details = document.createElement("details"), summary = document.createElement("summary"), content = document.createElement("pre");
        setText(summary, "auditDetails"); content.textContent = JSON.stringify(item.details, null, 2);
        details.append(summary, content); cell.append(details);
      } else if (item.detailsUnavailable) setText(cell, "auditMissing");
      else cell.textContent = "—";
      row.append(cell); return row;
    }));
  }
  async function load(stack = [""]) {
    if (!signedIn || busy) return;
    const current = generation;
    busy = true; message("auditLoading"); buttons();
    try {
      const page = await request(parameters(stack.at(-1)));
      if (current !== generation) return;
      if (page.redacted !== false) throw {};
      render(page.items); next = page.nextCursor || ""; cursors = stack; loaded = true;
      el("audit-message").textContent = ""; el("audit-message").removeAttribute("data-i18n");
    } catch (error) {
      if (current !== generation) return;
      if (error.status === 401) show("login"); else message(error.status === 403 ? "auditForbidden" : "auditFailed");
    } finally { if (current === generation) { busy = false; buttons(); } }
  }
  el("audit-filters").addEventListener("submit", event => {
    event.preventDefault();
    if (busy) return;
    const params = new URLSearchParams();
    for (const name of fields) {
      const value = el("audit-" + name).value.trim();
      if (!value) continue;
      if (name === "from" || name === "to") {
        const seconds = Math.floor(Date.parse(value) / 1000);
        params.set(name, String(seconds));
      } else params.set(name, value);
    }
    if (!validTimeRange(params)) { message("auditInvalidTime"); return; }
    if (window.ConsoleUI) window.ConsoleUI.go("audit?" + params);
    else { reset(); filters = params; void load(); }
  });
  el("previous-audit").addEventListener("click", () => cursors.length > 1 && load(cursors.slice(0, -1)));
  el("next-audit").addEventListener("click", () => next && load([...cursors, next]));
  el("export-audit").addEventListener("click", async () => {
    if (!signedIn || busy || !loaded) return;
    const current = generation; busy = true; buttons();
    try {
      const page = await request(parameters(cursors.at(-1), true));
      if (current !== generation) return;
      if (page.redacted !== true || page.items.some(item => item.actorId != null || item.resourceId != null || item.details != null)) throw {};
      window.downloadGuardrailJSON(page, "guardrail-audit.json"); message("exportReady");
    } catch (error) {
      if (current !== generation) return;
      if (error.status === 401) show("login"); else message(error.status === 403 ? "auditForbidden" : "exportFailed");
    } finally { if (current === generation) { busy = false; buttons(); } }
  });
  window.AuditUI = {
    setSignedIn(value) { if (signedIn !== value) { signedIn = value; filters = new URLSearchParams(); reset(); fields.forEach(name => { el("audit-" + name).value = ""; }); } },
    leave() { reset(); },
    route(params) {
      const query = new URLSearchParams();
      for (const name of fields) if (params.has(name)) query.set(name, params.get(name));
      if (query.toString() !== filters.toString()) { reset(); filters = query; }
      for (const name of fields) {
        let value = filters.get(name) || "";
        if ((name === "from" || name === "to") && value) {
          const date = new Date(Number(value) * 1000);
          value = validTimestamp(value) ? new Date(date.valueOf() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 19) : "";
        }
        el("audit-" + name).value = value;
      }
      if (!validTimeRange(query)) {
        reset(); message("auditInvalidTime"); return;
      }
      if (!loaded) void load();
    },
  };
})();
