// Read-only Run history uses the administrator session and never handles Project or Run credentials.
(() => {
  const el = id => document.getElementById(id);
  let signedIn = false, generation = 0, busy = false, loaded = false;
  let items = [], nextCursor = "", cursors = [""];

  let detailVersion = 0, evidenceVersion = 0, historyVersion = 0, visibleRun = null;
  let historyCursors = [0], historyNext = null, currentSubmission = null, retryEvidence = null;
  const validID = value => typeof value === "string" && /^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$/.test(value);

  const formatTime = value => value == null ? "—" : new Intl.DateTimeFormat(
    document.documentElement.lang || undefined,
    {dateStyle: "medium", timeStyle: "medium"},
  ).format(new Date(value * 1000));
  const text = value => value == null || value === "" ? "—" : String(value);

  async function request(path) {
    const response = await fetch(path, {
      credentials: "same-origin", redirect: "error",
      headers: {"Content-Type": "application/json", "X-Guardrail-CSRF": "1"},
      signal: AbortSignal.timeout(15000),
    });
    const data = await response.json();
    if (!response.ok) throw {status: response.status};
    return data;
  }

  function message(key, state = "info") {
    setText(el("run-message"), key);
    el("run-message").dataset.state = state;
  }
  function clearMessage() {
    el("run-message").textContent = "";
    el("run-message").removeAttribute("data-i18n");
    el("run-message").dataset.state = "empty";
  }


  function node(tag, value, className = "") {
    const result = document.createElement(tag);
    if (value != null) result.textContent = String(value);
    result.className = className; return result;
  }
  function setTime(element, value) {
    if (value == null) delete element.dataset.runTime;
    else element.dataset.runTime = String(value);
    element.textContent = formatTime(value);
  }
  function evidenceMessage(key, error = false) {
    setText(el("run-submission-state"), key); el("run-submission-state").hidden = false;
    el("run-submission-state").dataset.state = error ? "error" : "info";
  }
  function clearEvidence() {
    evidenceVersion++; historyVersion++; currentSubmission = null; retryEvidence = null;
    historyCursors = [0]; historyNext = null;
    el("run-checks").replaceChildren(); el("run-submission-json").textContent = "";
    el("run-submission-select").replaceChildren(); el("run-submission-history").hidden = true;
    el("run-submission-content").hidden = true; el("retry-run-submission").hidden = true;
  }
  function leaveDetail() {
    detailVersion++; visibleRun = null; clearEvidence();
    el("run-detail-content").hidden = true; el("run-detail-message").textContent = "";
    el("run-detail-message").removeAttribute("data-i18n");
    el("run-detail-id").textContent = "";
  }
  function evidenceField(list, key, value, structured = false) {
    const row = node("div"), term = node("dt"), description = node("dd"); setText(term, key);
    let content = text(value);
    if (structured) { try { content = JSON.stringify(JSON.parse(value), null, 2); } catch { /* Plain text is valid evidence. */ } }
    description.append(node(structured ? "pre" : "span", content)); row.append(term, description); list.append(row);
  }
  function renderSubmission(submission) {
    currentSubmission = submission;
    el("run-detail-submission").textContent = "#" + submission.submissionNo;
    el("run-submission-verdict").textContent = submission.verdict;
    el("run-submission-verdict").dataset.verdict = submission.verdict;
    el("run-submission-summary").textContent = text(submission.summary);
    setTime(el("run-submission-created"), submission.createdAt);
    el("run-history-note").hidden = submission.id === visibleRun.finalSubmissionId;
    el("run-checks").replaceChildren(...(submission.snapshot.checks || []).map(check => {
      const card = node("article", null, "run-check"), heading = node("div", null, "run-evidence-toolbar");
      const verdict = ["PASS", "BLOCKED", "UNKNOWN"].includes(check.verdict) ? check.verdict : "UNKNOWN";
      heading.append(node("h3", check.checkId), node("span", verdict, "badge verdict-" + verdict.toLowerCase()));
      card.append(heading, node("p", text(check.summary)));
      if (!check.evidence?.length) { const empty = node("p"); setText(empty, "noCheckEvidence"); card.append(empty); }
      for (const evidence of check.evidence || []) {
        const fields = node("dl", null, "evidence-fields"), details = node("details"), summary = node("summary"), collection = node("dl");
        setText(summary, "collectionDetails"); details.append(summary);
        card.append(node("h4", evidence.requirementId));
        evidenceField(fields, "observedAt", evidence.observedAt);
        const observed = Date.parse(evidence.observedAt);
        if (Number.isFinite(observed)) setTime(fields.children[0].children[1].children[0], observed / 1000);
        evidenceField(fields, "resultSummary", evidence.resultSummary, true);
        evidenceField(collection, "querySummary", evidence.querySummary);
        if (evidence.reference) evidenceField(collection, "evidenceReference", evidence.reference);
        details.append(collection); card.append(fields, details);
      }
      return card;
    }));
    el("run-submission-json").textContent = JSON.stringify(submission, null, 2);
    el("run-submission-state").hidden = true; el("run-submission-content").hidden = false;
  }
  async function loadSubmission(id) {
    const run = visibleRun, current = detailVersion, serial = ++evidenceVersion;
    if (!run) return;
    currentSubmission = null; el("run-checks").replaceChildren(); el("run-submission-json").textContent = "";
    el("run-submission-content").hidden = true; el("retry-run-submission").hidden = true;
    evidenceMessage("evidenceLoading"); retryEvidence = () => loadSubmission(id);
    try {
      if (!validID(id)) throw {};
      const submission = await request("/api/v1alpha1/runs/" + encodeURIComponent(run.id) + "/submissions/" + encodeURIComponent(id));
      if (!signedIn || current !== detailVersion || serial !== evidenceVersion) return;
      if (submission.id !== id || submission.runId !== run.id || !submission.snapshot || submission.submissionNo < 1 || submission.submissionNo > run.latestSubmissionNo) throw {};
      renderSubmission(submission); retryEvidence = null;
    } catch (error) {
      if (!signedIn || current !== detailVersion || serial !== evidenceVersion) return;
      if (error.status === 401) { show("login"); return; }
      evidenceMessage("evidenceLoadFailed", true); el("retry-run-submission").hidden = false;
    }
  }
  async function loadHistory(before = 0, stack = [0]) {
    const run = visibleRun, current = detailVersion, serial = ++historyVersion;
    if (!run) return;
    evidenceVersion++; currentSubmission = null;
    el("run-checks").replaceChildren(); el("run-submission-json").textContent = "";
    el("run-submission-content").hidden = true; el("retry-run-submission").hidden = true;
    for (const id of ["run-submission-select", "newer-submissions", "older-submissions"]) el(id).disabled = true;
    evidenceMessage("evidenceLoading"); retryEvidence = () => loadHistory(before, stack);
    try {
      const page = await request("/api/v1alpha1/runs/" + encodeURIComponent(run.id) + "/submissions?limit=20&before=" + (before || run.latestSubmissionNo + 1));
      if (!signedIn || current !== detailVersion || serial !== historyVersion) return;
      if (!Array.isArray(page.items) || page.items.some(item => item.runId !== run.id || !validID(item.id) || item.submissionNo > run.latestSubmissionNo)) throw {};
      historyCursors = stack; historyNext = page.nextBefore || null;
      el("run-submission-select").replaceChildren(...page.items.map(item => {
        const option = node("option", "#" + item.submissionNo + " · " + item.verdict); option.value = item.id; return option;
      }));
      el("run-submission-history").hidden = page.items.length === 0;
      el("newer-submissions").hidden = stack.length === 1; el("older-submissions").hidden = !historyNext;
      if (!page.items.length) { evidenceMessage("submissionPending"); return; }
      el("run-submission-select").value = page.items[0].id;
      await loadSubmission(page.items[0].id);
    } catch (error) {
      if (!signedIn || current !== detailVersion || serial !== historyVersion) return;
      if (error.status === 401) { show("login"); return; }
      retryEvidence = () => loadHistory(before, stack);
      evidenceMessage("evidenceLoadFailed", true); el("retry-run-submission").hidden = false;
    } finally {
      if (current === detailVersion && serial === historyVersion) {
        for (const id of ["run-submission-select", "newer-submissions", "older-submissions"]) el(id).disabled = false;
      }
    }
  }

  function buttons() {
    for (const id of ["reload-runs", "apply-run-filters", "previous-runs", "next-runs"]) el(id).disabled = busy;
    el("previous-runs").disabled = busy || cursors.length === 1;
    el("next-runs").disabled = busy || !nextCursor;
    el("run-surface").setAttribute("aria-busy", String(busy));
  }

  function renderList() {
    el("run-empty").hidden = !loaded || items.length !== 0;
    el("run-table-wrap").hidden = !loaded || items.length === 0;
    el("run-pagination").hidden = !loaded || items.length === 0;
    const rows = items.map(run => {
      const row = document.createElement("tr");
      const values = [run.id, run.sceneKey, run.policyRevisionId ? run.policyRevisionId.slice(0, 18) + "…" : "—", run.status,
        text(run.result), text(run.decision), formatTime(run.createdAt)];
      values.forEach((value, index) => {
        const cell = document.createElement("td");
        const content = document.createElement(index < 2 ? "code" : "span");
        content.textContent = value;
        if (index === 3) content.className = "badge " + run.status.toLowerCase();
        cell.append(content); row.append(cell);
      });
      const action = document.createElement("td"), button = document.createElement("button");
      button.type = "button"; button.className = "text-button"; setText(button, "viewRun");
      button.addEventListener("click", () => {
        if (window.ConsoleUI) window.ConsoleUI.go(routePath(run.id));
        else void openRun(run.id);
      });
      action.append(button); row.append(action);
      return row;
    });
    el("run-rows").replaceChildren(...rows);
    buttons();
  }

  function listPath(cursor) {
    const values = ["limit=20"];
    const status = el("run-status-filter").value;
    const scene = el("run-scene-filter").value.trim();
    if (status) values.push("status=" + encodeURIComponent(status));
    if (scene) values.push("sceneKey=" + encodeURIComponent(scene));
    if (cursor) values.push("cursor=" + encodeURIComponent(cursor));
    return "/api/v1alpha1/runs?" + values.join("&");
  }
  function routePath(id = "") {
    const values = new URLSearchParams();
    const status = el("run-status-filter").value;
    const scene = el("run-scene-filter").value.trim();
    if (scene) values.set("sceneKey", scene);
    if (status) values.set("status", status);
    const query = values.toString();
    return "runs" + (id ? "/" + encodeURIComponent(id) : "") + (query ? "?" + query : "");
  }

  async function action(work) {
    if (!signedIn || busy) return false;
    const current = generation;
    busy = true; message("runsLoading", "loading"); buttons();
    try {
      await work(current);
      return current === generation;
    } catch (error) {
      if (current !== generation) return false;
      if (error.status === 401) { show("login"); return false; }
      message("runRequestFailed", "error");
      return false;
    } finally {
      if (current === generation) { busy = false; buttons(); }
    }
  }

  function load(cursor = "") {
    return action(async current => {
      const page = await request(listPath(cursor));
      if (current !== generation) return;
      items = page.items || []; nextCursor = page.nextCursor || ""; loaded = true;
      renderList();
      if (items.length) message("runsLoaded", "success");
      else clearMessage();
    });
  }

  async function openRun(id) {
    if (!signedIn) return false;
    leaveDetail(); const current = detailVersion;
    el("run-list-view").hidden = true; el("run-detail-view").hidden = false;
    setText(el("run-detail-message"), "runDetailLoading");
    el("refresh-run").onclick = () => openRun(id);
    try {
      if (!validID(id)) throw {};
      const run = await request("/api/v1alpha1/runs/" + encodeURIComponent(id));
      if (!signedIn || current !== detailVersion) return false;
      const revision = await request("/api/v1alpha1/policy-revisions/" + encodeURIComponent(run.policyRevisionId));
      if (!signedIn || current !== detailVersion) return false;
      if (run.id !== id || revision.id !== run.policyRevisionId || revision.digest !== run.specDigest) throw {};
      visibleRun = run;
      el("run-detail-id").textContent = run.id;
      el("run-detail-scene").textContent = run.sceneKey;
      el("run-detail-status").textContent = run.status;
      el("run-detail-status").className = "badge " + run.status.toLowerCase();
      el("run-detail-spec").textContent = (revision ? "v" + revision.revisionNumber : run.policyRevisionId) + " · SHA-256: " + run.specDigest;
      el("run-detail-result").textContent = text(run.result);
      el("run-detail-decision").dataset.decision = run.decision || "";
      el("run-detail-decision").removeAttribute("data-i18n");
      if (run.decision) el("run-detail-decision").textContent = run.decision;
      else setText(el("run-detail-decision"), run.status === "EXPIRED" ? "runTimedOut" : "noDecision");
      setTime(el("run-decision-expires"), run.decisionExpiresAt);
      setTime(el("run-detail-created"), run.createdAt);
      setTime(el("run-detail-deadline"), run.deadlineAt);
      setTime(el("run-detail-terminal"), run.finishedAt || run.expiredAt);
      el("run-detail-change-set").textContent = JSON.stringify(run.changeSet, null, 2);
      el("run-detail-spec-yaml").textContent = revision?.yaml || "—";
      el("run-list-view").hidden = true; el("run-detail-view").hidden = false;
      el("run-detail-content").hidden = false;
      el("run-submission-history").open = false; el("run-technical").open = false;
      el("run-detail-message").textContent = ""; el("run-detail-message").removeAttribute("data-i18n");
      window.scrollTo({top: 0});
      if (run.latestSubmissionNo) void loadHistory(); else evidenceMessage("submissionPending");
      return true;
    } catch (error) {
      if (!signedIn || current !== detailVersion) return false;
      if (error.status === 401) { show("login"); return false; }
      setText(el("run-detail-message"), "runDetailFailed"); return false;
    }
  }

  function showList() {
    leaveDetail();
    el("run-detail-view").hidden = true; el("run-list-view").hidden = false;
    window.scrollTo({top: 0});
  }

  window.RunUI = {
    setSignedIn(value) {
      if (signedIn === value) return;
      signedIn = value; generation++; busy = false; loaded = false;
      items = []; nextCursor = ""; cursors = [""];
      el("run-scene-filter").value = ""; el("run-status-filter").value = "";
      el("run-rows").replaceChildren(); clearMessage();
      showList(); renderList();
    },
    show() {
      showList();
      if (signedIn && !loaded) void load();
    },
    open(id) { return openRun(id); },
    leave: leaveDetail,
    refreshLanguage() {
      document.querySelectorAll("[data-run-time]").forEach(element => {
        element.textContent = formatTime(Number(element.dataset.runTime));
      });
    },
    route(id, params) {
      const scene = params?.get("sceneKey") || "";
      const status = params?.get("status") || "";
      const changed = scene !== el("run-scene-filter").value || status !== el("run-status-filter").value;
      el("run-scene-filter").value = scene;
      el("run-status-filter").value = status;
      if (changed) { loaded = false; items = []; nextCursor = ""; cursors = [""]; renderList(); }
      if (id) return openRun(id);
      this.show();
    },
  };

  el("retry-run-submission").addEventListener("click", () => retryEvidence?.());
  el("run-submission-select").addEventListener("change", async () => {
    await loadSubmission(el("run-submission-select").value);
    if (currentSubmission) el("run-submission-content").scrollIntoView?.({block: "start"});
  });
  el("older-submissions").addEventListener("click", () => historyNext && loadHistory(historyNext, [...historyCursors, historyNext]));
  el("newer-submissions").addEventListener("click", () => historyCursors.length > 1 && loadHistory(historyCursors[historyCursors.length - 2], historyCursors.slice(0, -1)));
  el("reload-runs").addEventListener("click", () => load(cursors[cursors.length - 1]));
  el("apply-run-filters").addEventListener("click", () => {
    cursors = [""]; nextCursor = ""; loaded = false;
    if (window.ConsoleUI) { window.ConsoleUI.go(routePath()); return; }
    return load();
  });
  el("next-runs").addEventListener("click", async () => {
    if (!nextCursor) return;
    const target = nextCursor;
    if (await load(target)) { cursors.push(target); buttons(); }
  });
  el("previous-runs").addEventListener("click", async () => {
    if (cursors.length === 1) return;
    const target = cursors[cursors.length - 2];
    if (await load(target)) { cursors.pop(); buttons(); }
  });
  el("back-runs").addEventListener("click", () => {
    if (window.ConsoleUI) window.ConsoleUI.go(routePath()); else showList();
  });
})();
