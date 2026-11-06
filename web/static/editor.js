// Lightweight visual highlighting, not a YAML parser. Server validation is authoritative.
// Keep the native textarea for selection, undo, IME input and keyboard accessibility.
(() => {
  const el = id => document.getElementById(id);
  const input = el("spec-yaml"), highlight = el("yaml-highlight"), lines = el("yaml-lines");
  function position() {
    const before = input.value.slice(0, input.selectionStart);
    const line = (before.match(/\n/g)?.length ?? 0) + 1;
    const column = Array.from(before.slice(before.lastIndexOf("\n") + 1)).length + 1;
    el("editor-position").textContent = line + " : " + column;
  }
  function syncScroll() {
    highlight.scrollTop = input.scrollTop;
    highlight.scrollLeft = input.scrollLeft;
    lines.scrollTop = input.scrollTop;
  }
  function token(text, kind) {
    const span = document.createElement("span");
    span.className = "yaml-" + kind;
    span.textContent = text;
    return span;
  }
  function refresh() {
    el("editor-error").hidden = true;
    el("editor-error").textContent = "";
    input.setAttribute("aria-invalid", "false");
    const fragment = document.createDocumentFragment();
    const rows = input.value.split("\n");
    let blockIndent = null;
    rows.forEach(row => {
      const indent = row.length - row.trimStart().length;
      if (blockIndent !== null && row.trim() && indent <= blockIndent) blockIndent = null;
      if (blockIndent !== null) {
        fragment.append(document.createTextNode(row + "\n")); return;
      }
      // Deliberately limited to field names, quoted strings and comments.
      const key = row.match(/^(\s*(?:-\s+)?)([A-Za-z_][\w.-]*)(\s*:)(?=\s|$)/);
      let rest = row;
      if (key) {
        fragment.append(document.createTextNode(key[1]), token(key[2], "key"), document.createTextNode(key[3]));
        rest = row.slice(key[0].length);
      }
      const pattern = /"(?:\\.|[^"\\])*"|'(?:''|[^'])*'|(?:^|\s)#.*/g;
      let offset = 0;
      for (const match of rest.matchAll(pattern)) {
        fragment.append(document.createTextNode(rest.slice(offset, match.index)));
        fragment.append(token(match[0], match[0].trimStart().startsWith("#") ? "comment" : "string"));
        offset = match.index + match[0].length;
      }
      fragment.append(document.createTextNode(rest.slice(offset) + "\n"));
      if (key && /^\s*[|>][+-]?\s*(?:#.*)?$/.test(rest)) blockIndent = indent;
    });
    highlight.replaceChildren(fragment);
    lines.textContent = rows.map((_, index) => index + 1).join("\n") + "\n";
    position(); syncScroll();
  }
  window.YamlEditor = {
    refresh,
    error(text) {
      el("editor-error").textContent = text;
      el("editor-error").hidden = !text;
      input.setAttribute("aria-invalid", String(!!text));
    },
  };
  input.addEventListener("input", refresh);
  input.addEventListener("scroll", syncScroll);
  for (const event of ["click", "keyup", "select"]) input.addEventListener(event, position);
  refresh();
  el("yaml-editor").classList.add("highlighted");
})();
