import { marked } from 'marked';
import { headingId } from './docs.mjs';

const esc = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

export function headingTokens(markdown) {
  const headings = [];
  marked.walkTokens(marked.lexer(markdown), token => {
    if (token.type === 'heading') headings.push(token);
  });
  return headings;
}

export function documentDescription(markdown) {
  const paragraph = marked.lexer(markdown).find(token => token.type === 'paragraph');
  return paragraph ? marked.parseInline(paragraph.text).replace(/<[^>]*>/g, '')
    .replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&')
    .replace(/\s+/g, ' ').trim().slice(0, 200) : '';
}

export function renderMarkdown(markdown, original, {source, locale, rewriteLink}) {
  const originals = headingTokens(original);
  const translated = headingTokens(markdown);
  if (originals.length !== translated.length || originals.some((h,i) => h.depth !== translated[i].depth)) {
    throw new Error(`Translation section structure differs from the source: ${source}`);
  }
  let index = 0;
  const headings = [], used = new Map();
  const renderer = new marked.Renderer();
  renderer.link = function({href, title, tokens}) {
    return `<a href="${esc(rewriteLink(href))}"${title ? ` title="${esc(title)}"` : ''}>${this.parser.parseInline(tokens)}</a>`;
  };
  renderer.image = ({href, text}) => `<img src="${esc(rewriteLink(href, true))}" alt="${esc(text)}" loading="lazy">`;
  renderer.html = ({text}) => esc(text);
  renderer.heading = function({tokens, depth}) {
    const html = this.parser.parseInline(tokens);
    const text = html.replace(/<[^>]+>/g, '');
    const slug = headingId(marked.parseInline(originals[index++].text));
    const count = used.get(slug) || 0;
    used.set(slug, count + 1);
    const id = `${slug}${count ? `-${count}` : ''}`;
    if (depth === 2) headings.push([id, text]);
    const localizedId = headingId(html);
    const alias = localizedId && localizedId !== id ? `<span id="${esc(localizedId)}"></span>` : '';
    return `<h${depth} id="${esc(id)}">${alias}${html}<a class="heading-anchor" href="#${esc(id)}" aria-label="${locale === 'zh' ? '链接到' : 'Link to'} ${esc(text)}">#</a></h${depth}>`;
  };
  return {html:marked.parse(markdown, {renderer, gfm:true}), headings, description:documentDescription(markdown)};
}
