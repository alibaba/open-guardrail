import { assessRequest } from './demo.mjs';
import { copy } from './copy.mjs';

const t = copy[document.body.dataset.locale] || copy.en;
const menuButton = document.querySelector('.menu-button');
const navigation = document.querySelector('#navigation');
function closeMenu() {
  menuButton?.setAttribute('aria-expanded', 'false');
  navigation?.classList.remove('is-open');
}
menuButton?.addEventListener('click', () => {
  const open = menuButton.getAttribute('aria-expanded') !== 'true';
  menuButton.setAttribute('aria-expanded', String(open));
  navigation?.classList.toggle('is-open', open);
});
navigation?.addEventListener('click', event => { if (event.target.closest('a')) closeMenu(); });
document.addEventListener('keydown', event => { if (event.key === 'Escape') closeMenu(); });
document.addEventListener('click', event => {
  if (!event.target.closest('.nav-shell')) closeMenu();
});

let toastTimer;
function toast(message) {
  const node = document.querySelector('#toast');
  if (!node) return;
  node.textContent = message;
  node.classList.add('visible');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => node.classList.remove('visible'), 2400);
}

const tabs = [...document.querySelectorAll('.code-tabs [role="tab"]')];
function selectTab(index, focus = false) {
  tabs.forEach((tab,i) => {
    const active = index === i;
    tab.setAttribute('aria-selected', String(active));
    tab.tabIndex = active ? 0 : -1;
    const panel = document.getElementById(tab.getAttribute('aria-controls'));
    if (panel) panel.hidden = !active;
  });
  const panel = document.getElementById(tabs[index]?.getAttribute('aria-controls'));
  const copyButton = document.querySelector('.copy-button');
  if (copyButton) copyButton.hidden = !panel?.querySelector('code');
  if (focus) tabs[index]?.focus();
}
tabs.forEach((tab,index) => {
  tab.addEventListener('click', () => selectTab(index));
  tab.addEventListener('keydown', event => {
    const next = event.key === 'ArrowRight' ? (index + 1) % tabs.length
      : event.key === 'ArrowLeft' ? (index + tabs.length - 1) % tabs.length
      : event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : null;
    if (next !== null) { event.preventDefault(); selectTab(next, true); }
  });
});
if (tabs.length) selectTab(0);

document.querySelector('#download-platform')?.addEventListener('change', event => {
  document.querySelector('#download-native').href = event.target.value;
});

document.querySelector('.copy-button')?.addEventListener('click', async () => {
  const code = document.querySelector('.quick-panel:not([hidden]) code');
  if (!code) return;
  const text = [...code.querySelectorAll('.command-line')].map(line => line.textContent.replace(/^\$ /, '')).join('\n');
  try {
    await navigator.clipboard.writeText(text);
    toast(t.copied);
  } catch {
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(code);
    selection.removeAllRanges();
    selection.addRange(range);
    toast(t.copyFailed);
  }
});

const form = document.querySelector('#demo-form');
function evaluate() {
  const targets = [...form.querySelectorAll('input[name="file"]:checked')].map(input => input.value);
  const result = assessRequest(targets, form.querySelector('#evidence-available').checked);
  const output = document.querySelector('#demo-result');
  output.querySelector('.result-label').textContent = result.result;
  const decision = output.querySelector('.result-decision');
  decision.textContent = result.decision;
  decision.className = `result-decision ${result.decision === 'DENY' ? 'denied' : 'allowed'}`;
  output.querySelectorAll('.demo-checks b').forEach((node,index) => {
    node.textContent = result.checks[index];
    node.dataset.verdict = result.checks[index];
  });
  output.querySelector('.result-reason').textContent = t.reasons[result.reason];
}
form?.addEventListener('submit', event => { event.preventDefault(); evaluate(); });
form?.addEventListener('change', evaluate);
document.querySelector('#narrow-request')?.addEventListener('click', () => {
  form.querySelectorAll('input[name="file"]').forEach(input => { input.checked = input.value.endsWith('.tmp'); });
  evaluate();
});
if (form) evaluate();
