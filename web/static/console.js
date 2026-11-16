// Presentation shell only: feature modules own API calls and state.
(() => {
  const el = id => document.getElementById(id);
  const dialog = el("create-scene-dialog");
  let signedIn = false, busy = false, scenes = [];

  function navigate(view) {
    if (view !== "runs") window.RunUI?.leave();
    window.scrollTo({top: 0});
    if (view !== "audit") window.AuditUI?.leave();
    el("audit-view").hidden = view !== "audit";
    el("settings-view").hidden = view !== "settings";
    el("runs-view").hidden = view !== "runs";
    el("scenes").hidden = view !== "scenes";
    for (const [id, selected] of [["nav-audit", view === "audit"], ["nav-scenes", view === "scenes"], ["nav-runs", view === "runs"], ["nav-settings", view === "settings"]]) {
      if (selected) el(id).setAttribute("aria-current", "page");
      else el(id).removeAttribute("aria-current");
    }
    if (view !== "settings" && typeof clearKey === "function") clearKey();
  }
  function tab(history) {
    for (const [name, selected] of [["draft", !history], ["history", history]]) {
      el("tab-" + name).setAttribute("aria-selected", String(selected));
      el("tab-" + name).tabIndex = selected ? 0 : -1;
      el(name + "-panel").hidden = !selected;
    }
  }
  function list() {
    window.scrollTo({top: 0});
    el("scene-page-heading").hidden = false;
    el("scene-list-view").hidden = false;
    el("scene-detail-view").hidden = true;
  }
  function routeState() {
    const value = window.location.hash.replace(/^#\/?/, "");
    const [path, query = ""] = value.split("?", 2);
    const parts = (path || "scenes").split("/").map(part => {
      try { return decodeURIComponent(part); } catch { return ""; }
    });
    return {parts, params: new URLSearchParams(query)};
  }
  function route() {
    if (!signedIn) return;
    if (!window.location.hash) { go("scenes", true); return; }
    const {parts: [view, id], params} = routeState();
    if (view === "audit" && !id) { navigate("audit"); window.AuditUI?.route(params); return; }
    if (view === "settings" && !id) { navigate("settings"); return; }
    if (view === "runs") {
      navigate("runs");
      window.RunUI?.route(id, params);
      return;
    }
    if (view === "scenes" || !view) {
      navigate("scenes");
      if (id) window.SpecUI?.open(id); else list();
      return;
    }
    go("scenes", true);
  }
  function go(path, replace = false) {
    const target = "#/" + path.replace(/^\/+/, "");
    if (window.location.hash === target) { route(); return; }
    if (replace) {
      window.history.replaceState(null, "", target);
      route();
    } else window.location.hash = target.slice(1);
  }
  function openCreate() {
    if (busy) return;
    el("create-scene-message").textContent = "";
    el("create-scene-message").removeAttribute("data-i18n");
    dialog.showModal();
  }
  function closeCreate() { if (!busy) dialog.close(); }
  function renderList() {
    el("scene-empty").hidden = scenes.length !== 0;
    el("scene-table-wrap").hidden = scenes.length === 0;
    const rows = scenes.map(scene => {
      const row = document.createElement("tr");
      const values = [scene.name, scene.key, scene.defaultPolicyId ? scene.defaultPolicyId.slice(0, 18) + "…" : "—"];
      values.forEach((value, index) => {
        const cell = document.createElement("td");
        const content = document.createElement(index === 1 ? "code" : "span");
        content.textContent = value;
        if (index === 2 && scene.defaultPolicyId) content.title = scene.defaultPolicyId;
        cell.append(content); row.append(cell);
      });
      const state = document.createElement("td"), badge = document.createElement("span");
      badge.className = "badge" + (scene.defaultPolicyId ? " active" : "");
      setText(badge, scene.defaultPolicyId ? "activeState" : "draftState");
      state.append(badge); row.append(state);
      const action = document.createElement("td"), button = document.createElement("button");
      button.type = "button"; button.className = "text-button";
      setText(button, "editSpec");
      button.addEventListener("click", () => go("scenes/" + encodeURIComponent(scene.key)));
      action.append(button); row.append(action);
      return row;
    });
    el("scene-rows").replaceChildren(...rows);
  }
  window.ConsoleUI = {
    go,
    route,
    setSignedIn(value) {
      el("shell").classList.toggle("signed-in", value);
      el("sidebar").hidden = !value;
      el("auth-intro").hidden = value;
      el("auth-footer").hidden = value;
      if (signedIn === value) return;
      signedIn = value;
      if (!value) {
        scenes = []; renderList(); dialog.close();
        el("scene-create-form").reset();
        el("scene-detail-name").textContent = ""; el("scene-detail-key").textContent = "";
      }
      navigate("scenes"); list(); tab(false);
    },
    setBusy(value) {
      busy = value;
      for (const id of ["new-scene", "submit-scene", "close-create", "cancel-create"]) el(id).disabled = value;
    },
    setScenes(value) { scenes = value; renderList(); },
    updateScene(scene) {
      el("scene-detail-name").textContent = scene.name;
      el("scene-detail-key").textContent = scene.key;
      const index = scenes.findIndex(item => item.key === scene.key);
      if (index !== -1) { scenes[index] = scene; renderList(); }
    },
    showDetail() {
      navigate("scenes");
      el("scene-page-heading").hidden = true;
      el("scene-list-view").hidden = true;
      el("scene-detail-view").hidden = false;
      tab(false);
    },
    created(key) { dialog.close(); go("scenes/" + encodeURIComponent(key)); },
    notice(key, detail) {
      if (!dialog.open) return;
      if (key === "specConflict") key = "sceneExists";
      setText(el("create-scene-message"), key);
      if (detail) {
        el("create-scene-message").removeAttribute("data-i18n");
        el("create-scene-message").textContent = t(key) + " " + detail;
      }
    },
  };
  el("nav-scenes").addEventListener("click", () => go("scenes"));
  el("nav-runs").addEventListener("click", () => go("runs"));
  el("nav-audit").addEventListener("click", () => go("audit"));
  el("nav-settings").addEventListener("click", () => go("settings"));
  el("back-scenes").addEventListener("click", () => go("scenes"));
  for (const id of ["new-scene", "empty-create"]) el(id).addEventListener("click", openCreate);
  for (const id of ["close-create", "cancel-create"]) el(id).addEventListener("click", closeCreate);
  dialog.addEventListener("cancel", event => { if (busy) event.preventDefault(); });
  for (const name of ["draft", "history"]) {
    el("tab-" + name).addEventListener("click", () => tab(name === "history"));
    el("tab-" + name).addEventListener("keydown", event => {
      if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const history = event.key === "End" || (event.key !== "Home" && name === "draft");
      tab(history); el(history ? "tab-history" : "tab-draft").focus();
    });
  }
  window.addEventListener("hashchange", route);
})();
