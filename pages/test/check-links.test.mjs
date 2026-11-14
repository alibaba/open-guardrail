import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, mkdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { checkBuilt } from '../src/check-links.mjs';

async function fixture(t,html) {
  const root=await mkdtemp(path.join(tmpdir(),'guardrail-links-'));
  t.after(()=>rm(root,{recursive:true,force:true}));
  await writeFile(path.join(root,'index.html'),html);
  return root;
}
test('malformed paths and hashes report source and URL, and continue checking',async t=>{
  const root=await fixture(t,'<a href="#%ZZ">bad hash</a><a href="/open-guardrail/%E0%A4%A">bad path</a><a href="#missing">missing</a>');
  const {failures}=await checkBuilt(root,'/open-guardrail/');
  assert.equal(failures.length,3);
  assert.ok(failures.some(f=>f.includes('index.html: invalid URL encoding: #%ZZ')));
  assert.ok(failures.some(f=>f.includes('invalid URL encoding: /open-guardrail/%E0%A4%A')));
  assert.ok(failures.some(f=>f.includes('missing anchor #missing')));
});
test('valid encoded fragments and repository subpaths resolve',async t=>{
  const root=await fixture(t,'<a href="/open-guardrail/docs/#%E4%B8%AD%E6%96%87">guide</a>');
  await mkdir(path.join(root,'docs'));
  await writeFile(path.join(root,'docs/index.html'),'<h1 id="中文">Guide</h1>');
  assert.deepEqual(await checkBuilt(root,'/open-guardrail/'),{failures:[],links:1,anchors:1,htmlFiles:2});
});
test('links cannot leave the deployment base or output directory',async t=>{
  const root=await fixture(t,'<a href="/outside/">outside</a><a href="/open-guardrail/%2e%2e/secret">escape</a>');
  const {failures}=await checkBuilt(root,'/open-guardrail/');
  assert.equal(failures.length,2);
  assert.match(failures[0],/outside deployment base/);
  assert.match(failures[1],/outside output directory/);
});
