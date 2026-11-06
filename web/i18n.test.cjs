// Run with node --test web/i18n.test.cjs; Node is not required by the server.
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const vm = require("node:vm");
const source = readFileSync(join(__dirname, "static/i18n.js"), "utf8");
const html = readFileSync(join(__dirname, "static/index.html"), "utf8");

function page(preferred, saved, blocked = false) {
  const elements = [...html.matchAll(/data-i18n="([^"]+)"/g)]
    .map(match => ({ dataset: { i18n: match[1] }, textContent: "" }));
  const selector = { addEventListener: (_, callback) => { selector.change = callback; } };
  const document = {
    documentElement: {},
    querySelectorAll: () => elements,
    querySelector: () => selector,
  };
  const storage = { value: saved };
  const context = vm.createContext({
    document,
    navigator: { languages: [preferred] },
    localStorage: {
      getItem() { if (blocked) throw Error("disabled"); return storage.value; },
      setItem(key, value) { if (blocked) throw Error("disabled"); storage.value = value; },
    },
  });
  vm.runInContext(source, context);
  return { context, document, elements, storage, selector };
}

test("browser language, explicit preference and unsupported language fallback", () => {
  assert.equal(page("zh-TW").document.documentElement.lang, "zh-CN");
  assert.equal(page("en", "zh-CN").document.documentElement.lang, "zh-CN");
  assert.equal(page("zh-CN", "en").document.documentElement.lang, "en");
  assert.equal(page("fr", "invalid").document.documentElement.lang, "en");
});

test("translations cover the same keys and every static text marker", () => {
  const p = page("en");
  vm.runInContext(`
    if (JSON.stringify(Object.keys(translations.en).sort()) !==
        JSON.stringify(Object.keys(translations["zh-CN"]).sort())) throw Error("Key mismatch");
  `, p.context);
  for (const element of p.elements) {
    assert.notEqual(element.textContent, element.dataset.i18n);
    assert.ok(element.textContent);
  }
});

test("switching updates current status and persists only the language", () => {
  const p = page("en");
  vm.runInContext('setText(document.querySelectorAll()[0], "signedOut")', p.context);
  p.selector.change({ target: { value: "zh-CN" } });
  assert.equal(p.elements[0].textContent, "已退出登录。");
  assert.equal(p.storage.value, "zh-CN");
  assert.equal(p.document.documentElement.lang, "zh-CN");
});

test("disabled storage does not prevent rendering or switching", () => {
  const p = page("zh-CN", null, true);
  p.selector.change({ target: { value: "en" } });
  assert.equal(p.document.documentElement.lang, "en");
  assert.equal(p.elements.find(element => element.dataset.i18n === "preview").textContent,
    "OPENGUARDRAIL · DEVELOPMENT PREVIEW");
});

test("language changes notify the Run view to reformat its existing timestamps", () => {
  const p=page('en');let calls=0;
  p.context.window={RunUI:{refreshLanguage(){calls++;}}};
  p.selector.change({target:{value:'zh-CN'}});
  assert.equal(calls,1);
});
