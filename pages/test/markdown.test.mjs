import test from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown, documentDescription } from '../src/markdown.mjs';

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
