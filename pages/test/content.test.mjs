import test from 'node:test';
import assert from 'node:assert/strict';
import { copy } from '../src/copy.mjs';
import { release } from '../src/release.mjs';

test('both locales cover the same content and complete plugin prerequisites',()=>{
  assert.deepEqual(Object.keys(copy.en).sort(),Object.keys(copy.zh).sort());
  for (const t of Object.values(copy)) {
    assert.equal(t.nav.length,2);
    for (const prerequisite of ['Node.js','22+','24.9+','pnpm']) assert.ok(t.quickHints[2].includes(prerequisite));
    assert.equal(t.quickTabs.length,3);
  }
});
test('release downloads cover all supported platforms and one matching plugin bundle',()=>{
  assert.equal(new Set(release.assets.map(a=>a.name)).size,6);
  const version=release.tag.replace(/^v/,'');
  assert.equal(new URL(release.url).pathname,`/alibaba/open-guardrail/releases/tag/${release.tag}`);
  const expected=['darwin-amd64','darwin-arm64','linux-amd64','linux-arm64'].map(platform=>`open-guardrail-${version}-${platform}.tar.gz`);
  expected.push(`openguardrail-agent-plugins-preview-${version}-install.tar.gz`,'SHA256SUMS');
  assert.deepEqual(release.assets.map(a=>a.name).sort(),expected.sort());
  for (const asset of release.assets) assert.equal(asset.url,`https://github.com/alibaba/open-guardrail/releases/download/${release.tag}/${asset.name}`);
});
