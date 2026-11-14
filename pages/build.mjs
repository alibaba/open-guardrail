import { readFile, writeFile, mkdir, rm, cp } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { renderMarkdown } from './src/markdown.mjs';
import { copy } from './src/copy.mjs';
import { release } from './src/release.mjs';
import { docs } from './src/docs.mjs';

const root = path.dirname(fileURLToPath(import.meta.url));
const repo = path.dirname(root);
const dist = path.join(root, 'dist');
const base = `/${(process.env.SITE_BASE_PATH || '').replace(/^\/+|\/+$/g, '')}/`.replace('//', '/');
const origin = process.env.SITE_ORIGIN?.replace(/\/$/, '');
const github = 'https://github.com/alibaba/open-guardrail';
const esc = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const asset = value => `${base}assets/${value}`;
const home = locale => `${base}${locale === 'zh' ? 'zh/' : ''}`;
const doc = (locale, slug = '') => `${home(locale)}docs/${slug ? `${slug}/` : ''}`;
const shield = '<svg viewBox="0 0 32 36" fill="none" aria-hidden="true"><path d="m16 2 13 5v11c0 8-13 15-13 15S3 26 3 18V7L16 2Z" stroke="currentColor" stroke-width="2"/><path d="m10 17 4 4 8-9" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const arrow = '<span aria-hidden="true">↗</span>';
const gh = '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .8a11.4 11.4 0 0 0-3.6 22.2c.6.1.8-.2.8-.5v-2.1c-3.2.7-3.9-1.4-3.9-1.4-.5-1.3-1.3-1.7-1.3-1.7-1-.7.1-.7.1-.7 1.1.1 1.7 1.2 1.7 1.2 1 1.7 2.6 1.2 3.3.9.1-.7.4-1.2.7-1.5-2.6-.3-5.3-1.3-5.3-5.6 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.1 1.2A10.6 10.6 0 0 1 12 6c1 0 1.9.1 2.8.4 2.2-1.5 3.1-1.2 3.1-1.2.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.3-5.3 5.6.4.4.8 1.1.8 2.1v3.3c0 .3.2.6.8.5A11.4 11.4 0 0 0 12 .8Z"/></svg>';

function header(locale, current = '') {
  const t = copy[locale];
  const other = locale === 'en' ? 'zh' : 'en';
  const languageUrl = current.startsWith('docs') ? doc(other, current.split('/')[1]) : home(other);
  return `<a class="skip" href="#main">${t.skip}</a><header class="header"><div class="nav-shell"><a class="brand" href="${home(locale)}" aria-label="${t.brandHome}">${shield}<span>Open<span class="brand-light">Guardrail</span></span></a><button class="menu-button" aria-expanded="false" aria-controls="navigation" aria-label="${t.menu}"><span></span><span></span></button><nav id="navigation" aria-label="${t.mainNavigation}"><a href="${home(locale)}#how-it-works">${t.nav[0]}</a><a href="${doc(locale)}" ${current.startsWith('docs') ? 'aria-current="page"' : ''}>${t.nav[1]}</a><span class="nav-divider"></span><a class="language" lang="${other === 'zh' ? 'zh-CN' : 'en'}" href="${languageUrl}"><span aria-hidden="true">◎</span> ${t.other}</a><a class="github-icon" href="${github}" aria-label="GitHub">${gh}</a><a class="button button-small" href="${home(locale)}#quickstart">${t.start} <span aria-hidden="true">↗</span></a></nav></div></header>`;
}

function footer(locale) {
  const t = copy[locale];
  return `<footer class="footer container"><div><a class="brand" href="${home(locale)}">${shield}<span>OpenGuardrail</span></a><p>${t.footer}</p></div><nav aria-label="${t.footerNavigation}">${[doc(locale), `${github}/blob/main/CONTRIBUTING.md`, `${github}/blob/main/SECURITY.md`, `${github}/blob/main/LICENSE`].map((url,i)=>`<a href="${url}">${t.footerLinks[i]}</a>`).join('')}</nav><div class="footer-bottom"><span>© ${new Date().getUTCFullYear()} OpenGuardrail ${t.contributors}</span><span>${t.builtOpen} ${gh}<a href="${github}">alibaba/open-guardrail ${arrow}</a></span></div></footer>`;
}

function shell(locale, body, {title, description, route = '', toc = false} = {}) {
  const t = copy[locale];
  const canonical = origin ? `${origin}${home(locale)}${route}` : '';
  const shareImage = `${origin || ''}${asset(locale === 'zh' ? 'social-preview-zh.png' : 'social-preview.png')}`;
  return `<!doctype html><html lang="${t.lang}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="theme-color" content="#080c0a"><title>${esc(title || t.title)}</title><meta name="description" content="${esc(description || t.description)}"><meta property="og:type" content="website"><meta property="og:title" content="${esc(title || t.title)}"><meta property="og:description" content="${esc(description || t.description)}"><meta property="og:site_name" content="OpenGuardrail">${canonical ? `<link rel="canonical" href="${esc(canonical)}"><meta property="og:url" content="${esc(canonical)}"><link rel="alternate" hreflang="en" href="${origin}${home('en')}${route}"><link rel="alternate" hreflang="zh-CN" href="${origin}${home('zh')}${route}">` : ''}<meta property="og:locale" content="${locale === 'zh' ? 'zh_CN' : 'en_US'}"><meta property="og:image" content="${esc(shareImage)}"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="630"><meta property="og:image:alt" content="${esc(t.title)}"><meta name="twitter:card" content="summary_large_image"><meta name="twitter:image" content="${esc(shareImage)}"><link rel="icon" type="image/svg+xml" href="${asset('favicon.svg')}"><link rel="stylesheet" href="${asset('style.css')}"><noscript><link rel="stylesheet" href="${asset('noscript.css')}"></noscript><script type="module" src="${asset('site.js')}"></script></head><body data-locale="${locale}" ${toc ? 'class="docs-page"' : ''}>${header(locale, route)}${body}${footer(locale)}<div id="toast" role="status" aria-live="polite"></div></body></html>`;
}

const sourceCommands = 'git clone https://github.com/alibaba/open-guardrail.git\ncd open-guardrail\npython3 examples/cleanup-demo/demo.py';

function quickstart(locale) {
  const t = copy[locale];
  const platforms = [
    ['darwin-arm64', 'macOS · Apple Silicon'],
    ['darwin-amd64', 'macOS · Intel'],
    ['linux-amd64', 'Linux · amd64'],
    ['linux-arm64', 'Linux · arm64'],
  ].map(([platform,label]) => ({label,asset:release.assets.find(a=>a.name.endsWith(`-${platform}.tar.gz`))}));
  const checksum = release.assets.find(a=>a.name==='SHA256SUMS');
  const plugin = release.assets.find(a=>a.name.endsWith('-install.tar.gz'));
  return `<section class="section quickstart" id="quickstart"><div class="container quickstart-grid"><div class="section-heading"><p class="eyebrow">${release.tag}</p><h2>${t.quickTitle}</h2><p class="prereq">${t.prereq}</p><a class="inline-link" href="${doc(locale,'native-install')}">${t.fullGuide} ${arrow}</a></div><div class="quickstart-panel"><div class="code-tabs" role="tablist" aria-label="${t.quickNavigation}">${t.quickTabs.map((s,i)=>`<button id="quick-tab-${i}" type="button" role="tab" aria-selected="${i === 0}" aria-controls="quick-panel-${i}" ${i ? 'tabindex="-1"' : ''}>${s}</button>`).join('')}<button class="copy-button" type="button" aria-label="${t.copy}" title="${t.copy}" hidden>⧉</button></div>
    <div id="quick-panel-0" class="quick-panel download-panel" role="tabpanel" aria-labelledby="quick-tab-0"><p class="download-version">${release.tag} · Linux / macOS</p><label for="download-platform">${t.platform}</label><div class="download-controls"><select id="download-platform">${platforms.map(p=>`<option value="${p.asset.url}">${p.label}</option>`).join('')}</select><a id="download-native" class="button" href="${platforms[0].asset.url}">${t.downloadArchive} ↓</a></div><div class="download-links"><a href="${checksum.url}">SHA256SUMS ↗</a><a href="${doc(locale,'native-install')}">${t.installGuide} →</a></div><p class="quick-note">${t.quickHints[0]}</p><noscript><a class="inline-link" href="${release.url}">${t.releaseDownloads} ↗</a></noscript></div>
    <div id="quick-panel-1" class="quick-panel" role="tabpanel" aria-labelledby="quick-tab-1" hidden><pre><code>${sourceCommands.split('\n').map(s=>`<span class="command-line"><span class="syntax-comment" aria-hidden="true">$ </span>${esc(s)}</span>`).join('')}</code></pre><p>${t.quickHints[1]}</p></div>
    <div id="quick-panel-2" class="quick-panel plugin-panel" role="tabpanel" aria-labelledby="quick-tab-2" hidden><p>${t.pluginIntro}</p><a class="button" href="${doc(locale,'plugin-trial')}">${t.pluginGuide} →</a><a class="inline-link" href="${plugin.url}">${t.downloadPlugin} ↗</a><p class="quick-note">${t.quickHints[2]}</p></div>
  </div></div></section>`;
}

function landing(locale) {
  const t = copy[locale];
  return shell(locale, `<main id="main">
    <section class="hero"><div class="hero-glow" aria-hidden="true"></div><div class="container hero-grid">
      <div class="hero-content"><a class="eyebrow preview-badge" href="${release.url}"><span class="live-dot"></span>${t.badge} · ${release.tag}<span aria-hidden="true">↗</span></a><h1>${t.hero[0]}<br><span>${t.hero[1]}</span></h1><p class="hero-intro">${t.intro}</p><div class="hero-actions"><a class="button" href="#quickstart">${t.try} <span aria-hidden="true">↗</span></a><a class="button button-secondary" href="${doc(locale)}">${t.explore} <span aria-hidden="true">→</span></a></div><div class="hero-meta">${t.meta.map(s=>`<span><span aria-hidden="true">✓</span> ${s}</span>`).join('')}</div></div>
      <div class="hero-art"><div class="art-orbit orbit-one" aria-hidden="true"></div><div class="art-orbit orbit-two" aria-hidden="true"></div><div class="floating-chip chip-policy"><span aria-hidden="true">⌘</span> GuardrailSpec <span class="muted">${locale === 'zh' ? '修订 1' : 'rev. 1'}</span></div><div class="assessment-card"><div class="panel-top"><span class="panel-dots" aria-hidden="true"><i></i><i></i><i></i></span><span>${t.cardPath}</span><span class="panel-live"><i></i> ${t.cardPreview}</span></div><div class="assessment-body"><div class="assessment-heading">${shield}<div><span class="eyebrow">${t.evidenceFirst}</span><h2>${t.heroPanel}</h2></div></div><div class="record-context"><span>${t.cardScene}</span><span>${t.cardRun} <i class="live-dot"></i></span></div><div class="hero-checks">${t.heroChecks.map((s,i)=>`<div><span class="check-symbol">✓</span><span>${s}<small>${i ? t.targetLimit : 'cache.tmp · download.tmp'}</small></span><b>PASS</b></div>`).join('')}</div><div class="decision-row"><div><span class="eyebrow">${t.decision}</span><strong><i></i> ALLOW</strong></div><span class="decision-time">${t.validityLabel}<br><b>60</b></span></div><div class="panel-caption">${t.heroFooter}</div></div></div><div class="floating-chip chip-audit"><span class="check-symbol">✓</span> ${t.evidence} <span class="muted">↗</span></div></div>
    </div></section>
    <section class="section container" id="how-it-works"><div class="section-heading"><p class="eyebrow">${t.flowLabel}</p><h2>${t.flowTitle}</h2></div><div class="steps">${t.steps.map(([n,title,text])=>`<article class="step"><span class="step-num">${n}</span><h3>${title}</h3><p>${text}</p></article>`).join('')}</div></section>
    <section class="section container" id="demo"><div class="section-heading"><p class="eyebrow">${t.demoLabel}</p><h2>${t.demoTitle}</h2><p>${t.demoIntro}</p></div><div class="demo"><form id="demo-form"><div class="demo-panel-heading"><span class="eyebrow">01 / ${t.request}</span><span class="small-mono">${t.cardScene}</span></div><fieldset><legend>${t.selectFiles}</legend>${['cache.tmp','download.tmp','orders.csv','config.yaml'].map((f,i)=>`<label class="file-row"><input type="checkbox" name="file" value="${f}" checked><span class="file-icon" aria-hidden="true">${i < 2 ? '⌁' : '▤'}</span><span>${f}</span><span class="file-tag ${i < 2 ? '' : 'protected'}">${i < 2 ? t.temporary : t.protected}</span></label>`).join('')}</fieldset><label class="evidence-switch"><span>${t.evidenceToggle}</span><input id="evidence-available" type="checkbox" role="switch" checked><span class="switch-track" aria-hidden="true"></span></label><div class="demo-actions"><button class="button" type="submit">${t.evaluate}<span aria-hidden="true">→</span></button><button class="text-button" type="button" id="narrow-request">${t.narrow}</button></div></form><div class="demo-output"><div class="demo-panel-heading"><span class="eyebrow">02 / ${t.assessment}</span><span class="small-mono">${t.localIllustration}</span></div><div class="result-block" id="demo-result" aria-live="polite" aria-atomic="true"><span class="result-label">${t.ready}</span><strong class="result-decision waiting">—</strong><div class="demo-checks">${t.demoChecks.map(s=>`<div><span>${s}</span><b>—</b></div>`).join('')}</div><p class="result-reason">${t.demoHint}</p></div><p class="illustration-notice"><span aria-hidden="true">ⓘ</span> ${t.demoNotice}</p></div></div><a class="inline-link" href="${github}/tree/main/examples/cleanup-demo">${t.realDemo} ${arrow}</a></section>
    ${quickstart(locale)}
  </main>`);
}

function docNav(locale, active) {
  const t = copy[locale];
  const groups = t.docsGroups.map((group,i) => {
    const items = docs.filter(d => d[4] === i);
    const links = items.map(d => `<a href="${doc(locale,d[0])}" ${active===d[0]?'aria-current="page"':''}>${d[locale==='zh'?3:2]}</a>`).join('');
    return i === 0
      ? `<div class="docs-nav-group"><h2>${group}</h2>${links}</div>`
      : `<details class="docs-nav-group" ${items.some(d=>d[0]===active)?'open':''}><summary>${group}</summary>${links}</details>`;
  }).join('');
  return `<aside class="docs-sidebar"><a class="docs-home" href="${doc(locale)}">${t.docsTitle} <span aria-hidden="true">↗</span></a><nav aria-label="${t.docsNavigation}">${groups}</nav><a class="docs-github" href="${github}">${gh} GitHub ${arrow}</a></aside>`;
}

function docsIndex(locale) {
  const t = copy[locale];
  const groups = t.docsGroups.map((group,i) => {
    const cards = `<div class="doc-card-grid">${docs.filter(d=>d[4]===i).map(d=>`<a class="doc-card" href="${doc(locale,d[0])}"><span>${d[locale==='zh'?3:2]}</span>${arrow}</a>`).join('')}</div>`;
    return i === 0 ? `<section><h2>${group}</h2>${cards}</section>`
      : `<details class="doc-index-group"><summary>${group}</summary>${cards}</details>`;
  }).join('');
  return shell(locale, `<main id="main" class="container docs-layout">${docNav(locale)}<article class="docs-index"><p class="eyebrow">${t.docsEyebrow}</p><h1>${t.docsTitle}</h1><p class="docs-lead">${t.docsIntro}</p>${groups}</article></main>`, {route:'docs/', description:t.docsIntro, title:`${t.docsTitle} — OpenGuardrail`, toc:true});
}

function rewriteLink(href, source, locale, image = false) {
  if (!href || /^(https?:|mailto:|#)/.test(href)) return href || '#';
  if (/^[a-z]+:/i.test(href) || href.startsWith('//')) return '#';
  const [file, hash = ''] = href.split('#');
  const resolved = path.posix.normalize(path.posix.join(path.posix.dirname(source), file));
  if (resolved === 'docs/README.md') return doc(locale);
  const target = docs.find(d=>d[1]===resolved || d[5]===resolved);
  if (target && !image) {
    const targetLocale = target[5] === resolved ? 'zh' : source === target[5] && resolved === target[1] ? 'en' : locale;
    return `${doc(targetLocale,target[0])}${hash ? `#${hash}` : ''}`;
  }
  if (resolved.startsWith('docs/images/')) return `${asset(`images/${path.posix.basename(resolved)}`)}${hash ? `#${hash}` : ''}`;
  return `${github}/blob/main/${resolved}${hash ? `#${hash}` : ''}`;
}

async function renderDoc(locale, item) {
  const t = copy[locale];
  if (!item[5]) throw new Error(`Missing Chinese translation for ${item[0]}`);
  const source = locale === 'zh' ? item[5] : item[1];
  const markdown = await readFile(path.join(repo, source), 'utf8');
  const original = await readFile(path.join(repo, item[1]), 'utf8');
  const {html, headings, description} = renderMarkdown(markdown, original, {
    source, locale, rewriteLink:(href, image = false) => rewriteLink(href, source, locale, image),
  });
  const title = item[locale==='zh'?3:2];
  return shell(locale, `<main id="main" class="container docs-layout">${docNav(locale,item[0])}<article class="doc-article"><div class="doc-breadcrumb"><a href="${doc(locale)}">${t.docsTitle}</a><span>/</span><span>${title}</span></div><div class="markdown-body" lang="${t.lang}">${html}</div><div class="doc-end"><a href="${github}/blob/main/${source}">${t.edit} ${arrow}</a><a href="${doc(locale)}">${t.back} →</a></div></article><aside class="docs-toc"><p class="eyebrow">${t.onPage}</p><nav aria-label="${t.onPage}">${headings.map(([id,text])=>`<a href="#${esc(id)}">${text}</a>`).join('')}</nav></aside></main>`,{route:`docs/${item[0]}/`,title:`${title} — OpenGuardrail`,description,toc:true});
}

await rm(dist, {recursive:true,force:true});
await mkdir(path.join(dist,'assets'),{recursive:true});
for (const file of ['style.css','noscript.css','site.js','demo.mjs','copy.mjs','favicon.svg','social-preview.png','social-preview-zh.png']) await cp(path.join(root,'src',file),path.join(dist,'assets',file));
await cp(path.join(repo,'docs/images'),path.join(dist,'assets/images'),{recursive:true});
const routes=[];
async function save(route, content, index = true) {
  const filename=path.join(dist,route,'index.html');
  await mkdir(path.dirname(filename),{recursive:true});
  await writeFile(filename,content); if (index) routes.push(`${base}${route}`);
}
for(const locale of ['en','zh']) {
  const prefix=locale==='zh'?'zh/':'';
  await save(prefix,landing(locale));
  await save(`${prefix}docs/`,docsIndex(locale));
  for(const item of docs) await save(`${prefix}docs/${item[0]}/`,await renderDoc(locale,item));
  const destination = doc(locale, 'adoption-guide');
  const redirectBody = `<main id="main" class="container not-found"><h1>${locale === 'zh' ? '规划你的首次接入' : 'Plan your first integration'}</h1><a class="button" href="${destination}">${locale === 'zh' ? '阅读接入指引' : 'Read the integration guide'} →</a></main>`;
  const redirect = shell(locale, redirectBody, {route:'docs/adoption-guide/'})
    .replace('<head>', `<head><meta http-equiv="refresh" content="0;url=${destination}">`);
  await save(`${prefix}docs/development-roadmap/`, redirect, false);
}
for (const locale of ['en','zh']) {
  const t = copy[locale], other = locale === 'en' ? 'zh' : 'en', alternative = copy[other];
  const body = `<main id="main" class="container not-found"><p class="eyebrow">${t.notFoundEyebrow}</p><h1>${t.notFound}</h1><p>${t.notFoundText}</p><a class="button" href="${home(locale)}">${t.home} →</a><p lang="${alternative.lang}">${alternative.notFoundText} <a class="inline-link" href="${home(other)}">${alternative.home} →</a></p></main>`;
  const content = shell(locale, body, {title:`${t.notFoundEyebrow} — OpenGuardrail`,route:'404.html',description:t.notFoundText})
    .replace('<head>', '<head><meta name="robots" content="noindex">');
  await writeFile(path.join(dist,locale === 'zh' ? 'zh/404.html' : '404.html'),content);
}
await writeFile(path.join(dist,'.nojekyll'),'');
await writeFile(path.join(dist,'robots.txt'),`User-agent: *\nAllow: /\n${origin?`Sitemap: ${origin}${base}sitemap.xml\n`:''}`);
if(origin) await writeFile(path.join(dist,'sitemap.xml'),`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${routes.map(route=>`<url><loc>${esc(origin+route)}</loc></url>`).join('')}</urlset>`);
console.log(`Built ${routes.length} pages at ${dist} (base ${base}).`);
