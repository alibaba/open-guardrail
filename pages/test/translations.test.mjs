import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { marked } from 'marked';
import { docs, contributorDocs } from '../src/docs.mjs';
import { headingTokens } from '../src/markdown.mjs';

const repo = new URL('../../', import.meta.url);
// These existing guides translate comments in code blocks and table examples.
const existingTranslations = new Set(['guardrailspec-reference', 'plugin-trial', 'resource-bindings']);

for (const [slug, englishPath, , , , chinesePath] of [...docs, ...contributorDocs]) {
  test(`${slug}: complete Chinese guide with matching sections and executable examples`, async () => {
    assert.ok(chinesePath, `Missing Chinese source for ${slug}`);
    const english = await readFile(fileURLToPath(new URL(englishPath, repo)), 'utf8');
    const chinese = await readFile(fileURLToPath(new URL(chinesePath, repo)), 'utf8');
    assert.match(chinese, /\p{Script=Han}/u);
    assert.doesNotMatch(chinese, /\{\{CODE_\d+\}\}/);
    const enTokens = marked.lexer(english);
    const zhTokens = marked.lexer(chinese);
    const enHeadings = headingTokens(english);
    const zhHeadings = headingTokens(chinese);
    assert.deepEqual(zhHeadings.map(h => h.depth), enHeadings.map(h => h.depth));
    for (const heading of zhHeadings.filter(h => h.depth > 1)) assert.match(heading.text, /\p{Script=Han}/u);
    if (!existingTranslations.has(slug)) {
      assert.deepEqual(zhTokens.filter(t => t.type === 'code').map(t => [t.lang, t.text]),
        enTokens.filter(t => t.type === 'code').map(t => [t.lang, t.text]));
      assert.deepEqual(zhTokens.filter(t => t.type === 'table').map(t => t.rows.length),
        enTokens.filter(t => t.type === 'table').map(t => t.rows.length));
    }
  });
}
