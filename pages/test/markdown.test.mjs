import test from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown, documentDescription } from '../src/markdown.mjs';
import { inlineText, escapeHtml } from '../src/text.mjs';
import { headingId } from '../src/docs.mjs';
import { marked } from 'marked';

const options = {source:'nested.zh.md',locale:'zh',rewriteLink:href => href};
test('nested quote and list headings retain source anchors and document order', () => {
  const en = '# Guide\n\n> ## Quoted\n>\n> Text.\n\n- ### Nested\n\n## Repeated\n\n## Repeated';
  const zh = '# 指南\n\n> ## 引用\n>\n> 正文。\n\n- ### 列表\n\n## 重复\n\n## 再次重复';
  const result=renderMarkdown(zh,en,options);
  assert.deepEqual(result.headings.map(([id])=>id),['quoted','repeated','repeated-1']);
  for (const id of ['guide','quoted','nested','repeated','repeated-1']) assert.ok(result.html.includes(`id="${id}"`));
  assert.ok(result.html.includes('id="引用"'));
});
test('translation errors identify the source instead of indexing past headings', () => {
  assert.throws(()=>renderMarkdown('# 指南\n\n> ## 多出章节','# Guide',options),/nested.zh.md/);
});
test('document descriptions use readable first-paragraph text', () => {
  assert.equal(documentDescription('# Guide\n\nUse **policies**, `Runs` and [evidence](runs.md).'),'Use policies, Runs and evidence.');
  assert.equal(documentDescription('# Guide'),'');
});
test('raw HTML and attribute injection remain escaped', () => {
  const text='# Guide\n\n<script>alert(1)</script>\n\n![a" onerror="alert(1)](image.png)';
  const {html}=renderMarkdown(text,text,options);
  assert.ok(!html.includes('<script>'));
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(!html.includes('alt="a" onerror='));
});

test('token text preserves nested formatting, entities, image labels and literal code',()=>{
  const tokens=marked.Lexer.lexInline('**Policy *checks*** &amp; &copy; &#x4e2d; [Runs](runs.md) ![diagram](x.png) `&lt;code&gt;`');
  assert.equal(inlineText(tokens),'Policy checks & © 中 Runs diagram &lt;code&gt;');
  assert.equal(headingId(inlineText(marked.Lexer.lexInline('**Run** `API` &amp; 证据'))),'run-api-证据');
});

test('raw or entity-encoded markup is text at both description and TOC output boundaries',()=>{
  for(const input of ['<script','<img src=x onerror=alert(1)>','&lt;img src=x onerror=alert(1)&gt;','&#60;script&#62;alert(1)&#60;/script&#62;','" onmouseover="alert(1)','<scr<script>ipt>']) {
    const source=`# Guide\n\nText ${input}\n\n## Text ${input}`;
    const result=renderMarkdown(source,source,options);
    const toc=result.headings.map(([id,text])=>`<a href="#${escapeHtml(id)}">${escapeHtml(text)}</a>`).join('');
    const meta=`<meta name="description" content="${escapeHtml(result.description)}">`;
    assert.ok(!toc.includes('<script')&&!toc.includes('<img'));
    assert.ok(!meta.includes('<script')&&!meta.includes('<img'));
    assert.ok(!meta.includes('content="Text " onmouseover='));
    for(const [id] of result.headings)assert.doesNotMatch(id,/[<>"'&]/);
  }
});
