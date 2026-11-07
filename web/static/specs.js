// Scene management uses the same administrator cookie and origin as the login UI.
(() => {
  const el = id => document.getElementById(id);
  let signedIn = false, generation = 0, scene = null, policy = null, draft = null, revisions = [], busy = false;
  let editing = false, activeRevision = null, previewFailed = false;
  let revisionBefore = 0, revisionNext = null, revisionPrevious = [];
  const formatTime = value => new Intl.DateTimeFormat(
    document.documentElement.lang || undefined,
    {dateStyle: "medium", timeStyle: "medium"},
  ).format(new Date(value * 1000));
  const dirty = () => !!policy && editing && (!draft || el("spec-yaml").value !== draft.yaml);
  const notice = key => {
    setText(el("spec-message"), key);
    window.ConsoleUI?.notice(key);
    window.YamlEditor?.error("");
  };
  const path = suffix => "/api/v1alpha1/policies/" + encodeURIComponent(policy.id) + suffix;

  function draftView() {
    el("spec-draft-notice").hidden = editing;
    el("spec-editor-surface").hidden = !editing && !activeRevision;
    el("spec-editor-actions").hidden = !editing;
    el("spec-example").hidden = !editing;
    el("spec-yaml").readOnly = !editing;
    setText(el("spec-editor-label"), editing ? "specDraft" : "activeSpecReadOnly");
    setText(el("spec-preview-hint"), previewFailed ? "activeSpecLoadFailed" :
      policy?.activeRevisionId ? "noDraftActiveHint" : "noDraftNoActiveHint");
    setText(el("start-spec-draft"), policy?.activeRevisionId ? "editActiveSpec" : "createSpecDraft");
    el("retry-spec-preview").hidden = !previewFailed;
    el("spec-preview-retry").hidden = !previewFailed;
    el("spec-preview-warning").hidden = !editing;
    window.YamlEditor?.refresh();
  }
  function buttons() {
    el("spec-controls").disabled = busy;
    el("revisions-next").disabled = busy || revisionNext === null;
    el("revisions-previous").disabled = busy || revisionPrevious.length === 0;
    window.ConsoleUI?.setBusy(busy);
    el("save-spec").disabled = !policy || busy || !dirty();
    el("publish-spec").disabled = !policy || !editing || !draft || busy || !!dirty() || !draft.yaml.trim();
    el("start-spec-draft").disabled = !policy || busy || (!!policy.activeRevisionId && !activeRevision);
    el("retry-spec-preview").disabled = busy;
    const selected = revisions.find(item => String(item.revisionNumber) === el("revision-select").value);
    el("activate-revision").disabled = !policy || busy || !selected || selected.id === policy.activeRevisionId;
  }
  async function request(url, method = "GET", body, operationID = "") {
    const response = await fetch(url, {
      method, credentials: "same-origin", redirect: "error",
      headers: {"Content-Type": "application/json", "X-Guardrail-CSRF": "1", ...(operationID ? {"Idempotency-Key": operationID} : {})},
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(15000),
    });
    const data = await response.json();
    if (!response.ok) throw {status: response.status, detail: data.message};
    return data;
  }
  async function action(work) {
    if (busy || !signedIn) return;
    const current = generation;
    busy = true; buttons();
    try {
      await work(current);
    } catch (error) {
      if (current !== generation) return;
      el("scene-select").value = scene?.key || "";
      if (error.status === 401) { show("login"); setText(message, "loginPrompt"); return; }
      notice(error.status === 409 ? "specConflict" : error.status === 422 ? "specInvalid" : "specRequestFailed");
      // Validation messages contain field guidance, never raw YAML or database errors.
      if (error.status === 422) {
        el("spec-message").removeAttribute("data-i18n");
        el("spec-message").textContent = t("specInvalid") + " " + error.detail;
        window.ConsoleUI?.notice("specInvalid", error.detail);
        window.YamlEditor?.error(t("specInvalid") + " " + error.detail);
      }
    } finally {
      if (current === generation) { busy = false; buttons(); }
    }
  }
  function option(value, text, key) {
    const item = document.createElement("option");
    item.value = value;
    if (key) setText(item, key); else item.textContent = text;
    return item;
  }
  async function renderRevision(current = generation, loadedContent = null) {
    const revision = revisions.find(item => String(item.revisionNumber) === el("revision-select").value);
    el("revision-yaml").textContent = "";
    el("revision-meta").textContent = "";
    el("revision-meta").removeAttribute("data-i18n");
    if (!revision) { buttons(); return; }
    const policyID = policy.id;
    const isCurrent = () => current === generation && policy?.id === policyID && el("revision-select").value === String(revision.revisionNumber);
    setText(el("revision-meta"), "revisionLoading");
    try {
      const content = loadedContent?.id === revision.id ? loadedContent :
        await request("/api/v1alpha1/policy-revisions/" + encodeURIComponent(revision.id));
      if (!isCurrent()) return;
      el("revision-yaml").textContent = content.yaml;
      el("revision-meta").removeAttribute("data-i18n");
      el("revision-meta").textContent = "v" + revision.revisionNumber + " · " + formatTime(revision.publishedAt) + " · " + revision.publishedBy + " · SHA-256: " + revision.digest;
      buttons();
    } catch (error) {
      if (isCurrent()) setText(el("revision-meta"), "revisionLoadFailed");
      throw error;
    }
  }
  function revisionOptions() {
    el("revision-select").replaceChildren(...(revisions.length
      ? revisions.map(item => option(String(item.revisionNumber), "v" + item.revisionNumber))
      : [option("", "", "noRevisions")]));
  }
  async function historyPage(before, current) {
    const page = await request(path("/revisions?pageSize=20" + (before ? "&before=" + before : "")));
    if (current !== generation) return false;
    revisions = page.items; revisionBefore = before; revisionNext = page.nextBefore ?? null;
    revisionOptions();
    return true;
  }
  async function loadActiveRevision(selected) {
    const content = await request("/api/v1alpha1/policy-revisions/" + encodeURIComponent(selected.activeRevisionId));
    if (content.id !== selected.activeRevisionId || content.policyId !== selected.id || typeof content.yaml !== "string") {
      throw new Error("Active revision identity mismatch");
    }
    return content;
  }
  async function openScene(key, current) {
    const base = "/api/v1alpha1/scenes/" + encodeURIComponent(key);
    const loaded = await request(base);
    if (current !== generation) return;
    if (!loaded.defaultPolicyId) {
      scene = loaded; policy = null; draft = null; revisions = [];
      editing = false; activeRevision = null; previewFailed = false;
      el("spec-yaml").value = ""; draftView();
      revisionBefore = 0; revisionNext = null; revisionPrevious = [];
      window.ConsoleUI?.updateScene(scene);
      el("scene-select").value = scene.key;
      el("spec-editor").hidden = true;
      el("spec-active").textContent = "—";
      notice("noActiveSpec");
      return;
    }
    const policyBase = "/api/v1alpha1/policies/" + encodeURIComponent(loaded.defaultPolicyId);
    const [selected, history, currentDraft] = await Promise.all([
      request(policyBase), request(policyBase + "/revisions?pageSize=20"),
      request(policyBase + "/draft").catch(error => { if (error.status === 404) return null; throw error; }),
    ]);
    if (current !== generation) return;
    let active = history.items.find(item => item.id === selected.activeRevisionId) || null;
    let activeContent = null, failedPreview = false;
    if (selected.activeRevisionId && (!currentDraft || !active)) {
      try {
        const content = await loadActiveRevision(selected);
        active = content; activeContent = content;
      } catch (error) {
        if (error.status === 401) throw error;
        failedPreview = true;
      }
    }
    if (current !== generation) return;
    scene = loaded; policy = selected; draft = currentDraft; revisions = history.items;
    editing = !!draft; activeRevision = activeContent; previewFailed = failedPreview;
    revisionBefore = 0; revisionNext = history.nextBefore ?? null; revisionPrevious = [];
    window.ConsoleUI?.updateScene(scene);
    el("scene-select").value = scene.key;
    el("spec-yaml").value = draft?.yaml ?? activeRevision?.yaml ?? "";
    draftView();
    if (active) {
      el("spec-active").removeAttribute("data-i18n");
      el("spec-active").textContent = "v" + active.revisionNumber;
    } else setText(el("spec-active"), policy.activeRevisionId ? "activeSpecUnavailable" : "noActiveSpec");
    revisionOptions();
    try {
      await renderRevision(current, activeContent);
    } catch (error) {
      // History content is independent of the draft editor. Authentication
      // failures still end the session; other failures stay in the history view.
      if (error.status === 401) throw error;
    }
    if (current !== generation) return;
    el("spec-editor").hidden = false;
  }
  async function reload(current, selected = scene?.key) {
    const list = await request("/api/v1alpha1/scenes");
    if (current !== generation) return;
    window.ConsoleUI?.setScenes(list);
    el("scene-select").replaceChildren(...(list.length
      ? list.map(item => option(item.key, item.name + " (" + item.key + ")"))
      : [option("", "", "noScenes")]));
    if (!list.length) {
      scene = null; policy = null; draft = null; revisions = []; el("spec-editor").hidden = true; notice("noScenes"); return;
    }
    const key = list.some(item => item.key === selected) ? selected : list[0].key;
    el("scene-select").value = key;
    await openScene(key, current);
  }
  function mayDiscard() { return !dirty() || window.confirm(t("discardSpecChanges")); }

  window.SpecUI = {
    open(key) {
      if (scene?.key === key) { window.ConsoleUI?.showDetail(); return; }
      if (!mayDiscard()) return;
      return action(async current => {
        await openScene(key, current);
        if (current === generation) { window.ConsoleUI?.showDetail(); notice("specLoaded"); }
      });
    },
    setSignedIn(value) {
      if (signedIn === value) return;
      signedIn = value; generation++; busy = false;
      revisionBefore = 0; revisionNext = null; revisionPrevious = [];
      scene = null; policy = null; draft = null; revisions = [];
      editing = false; activeRevision = null; previewFailed = false;
      el("spec-yaml").value = ""; el("revision-yaml").textContent = ""; el("revision-meta").textContent = "";
      el("revision-meta").removeAttribute("data-i18n");
      draftView();
      el("scene-select").replaceChildren(); el("revision-select").replaceChildren();
      el("spec-editor").hidden = true; el("spec-message").textContent = "";
      el("spec-message").removeAttribute("data-i18n");
      buttons();
      if (value) void action(current => reload(current)).then(() => {
        if (signedIn) window.ConsoleUI?.route();
      });
    },
  };
  el("scene-create-form").addEventListener("submit", event => {
    event.preventDefault();
    if (!mayDiscard()) return;
    const data = Object.fromEntries(new FormData(event.currentTarget));
    return action(async current => {
      const created = await request("/api/v1alpha1/scenes/" + encodeURIComponent(data.key) + ":initialize", "POST", {name: data.name, defaultPolicyName: data.defaultPolicyName, yaml: data.yaml}, crypto.randomUUID());
      if (current !== generation) return;
      el("scene-create-form").reset();
      await reload(current, created.scene.key);
      if (current === generation) { window.ConsoleUI?.created(created.scene.key); notice("sceneCreated"); }
    });
  });
  el("scene-select").addEventListener("change", () => {
    if (!mayDiscard()) { el("scene-select").value = scene.key; return; }
    const key = el("scene-select").value;
    return action(async current => {
      await openScene(key, current);
      if (current === generation) notice("specLoaded");
    });
  });
  el("reload-scenes").addEventListener("click", () => {
    if (mayDiscard()) return action(async current => {
      await reload(current);
      if (current === generation && scene) notice("specLoaded");
    });
  });
  el("spec-yaml").addEventListener("input", () => { buttons(); notice("specUnsaved"); });
  el("start-spec-draft").addEventListener("click", () => {
    if (!signedIn || busy || editing || !policy || (policy.activeRevisionId && !activeRevision)) return;
    editing = true;
    el("spec-yaml").value = activeRevision?.yaml ?? "";
    draftView(); buttons(); notice("specUnsaved");
    el("spec-yaml").focus();
  });
  el("retry-spec-preview").addEventListener("click", () => action(async current => {
    if (!scene || !policy?.activeRevisionId) return;
    const selected = policy;
    try {
      const content = await loadActiveRevision(selected);
      if (current !== generation || policy?.id !== selected.id || policy.activeRevisionId !== content.id) return;
      activeRevision = content; previewFailed = false;
      if (!editing) el("spec-yaml").value = content.yaml;
      el("spec-active").removeAttribute("data-i18n");
      el("spec-active").textContent = "v" + content.revisionNumber;
      draftView();
      notice(dirty() ? "specUnsaved" : "specLoaded");
    } catch (error) {
      if (current !== generation) return;
      if (error.status === 401) throw error;
      previewFailed = true; draftView();
    }
  }));
  el("spec-example").addEventListener("click", () => {
    if (!scene || !editing || !mayDiscard()) return;
    el("spec-yaml").value = "apiVersion: guardrail.open/v1alpha1\nkind: GuardrailSpec\nmetadata:\n  name: recovery-gate\nspec:\n  description: Check whether recovery can safely continue\n  instructions: Inspect the current state and submit a verdict with evidence\n  validForSeconds: 900\n  checks:\n    - id: recovery-readiness\n      displayName: Recovery readiness\n      description: Verify that the cause of the interruption is resolved\n      instructions: Inspect current logs and metrics and provide reproducible evidence\n      criteria:\n        pass: The interruption cause is resolved and health is stable\n        block: A confirmed failure makes recovery unsafe\n        unknown: Current evidence is insufficient to decide\n      evidenceRequirements:\n        - id: recovery-evidence\n          description: Record the observation source and current health\n          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]\n";
    window.YamlEditor?.refresh();
    buttons(); notice("specUnsaved");
  });
  el("save-spec").addEventListener("click", () => action(async current => {
    if (!editing) return;
    const yaml = el("spec-yaml").value;
    if (new TextEncoder().encode(yaml).length > 65536) { notice("specTooLarge"); return; }
    const saved = await request(path("/draft"), "PUT", {yaml});
    if (current !== generation) return;
    draft = saved;
    notice("specSaved");
  }));
  el("publish-spec").addEventListener("click", () => {
    if (!editing || !draft) return;
    if (dirty()) { notice("saveSpecFirst"); return; }
    if (!window.confirm(t("confirmSpecPublish"))) return;
    return action(async current => {
      const key = scene.key, currentPolicy = policy, reviewedYaml = draft.yaml;
      const revision = await request(path("/draft:publish"), "POST", {yaml: reviewedYaml}, crypto.randomUUID());
      await request(path("/active-revision"), "PUT", {policyRevisionId: revision.id, expectedActiveRevisionId: currentPolicy.activeRevisionId});
      if (current !== generation) return;
      await openScene(key, current);
      if (current === generation) notice("specPublished");
    });
  });
  el("revision-select").addEventListener("change", () => action(current => renderRevision(current)));
  el("revisions-next").addEventListener("click", () => action(async current => {
    if (revisionNext === null) return;
    const previous = revisionBefore;
    if (await historyPage(revisionNext, current)) { revisionPrevious.push(previous); await renderRevision(current); }
  }));
  el("revisions-previous").addEventListener("click", () => action(async current => {
    if (!revisionPrevious.length) return;
    if (await historyPage(revisionPrevious[revisionPrevious.length - 1], current)) { revisionPrevious.pop(); await renderRevision(current); }
  }));
  el("activate-revision").addEventListener("click", () => action(async current => {
    const revision = revisions.find(item => String(item.revisionNumber) === el("revision-select").value);
    if (!revision || !policy || revision.id === policy.activeRevisionId) return;
    const key = scene.key;
    await request(path("/active-revision"), "PUT", {policyRevisionId: revision.id, expectedActiveRevisionId: policy.activeRevisionId});
    if (current !== generation) return;
    await openScene(key, current);
    if (current === generation) notice("specPublished");
  }));
  window.addEventListener("beforeunload", event => {
    if (signedIn && dirty()) { event.preventDefault(); event.returnValue = ""; }
  });
})();
