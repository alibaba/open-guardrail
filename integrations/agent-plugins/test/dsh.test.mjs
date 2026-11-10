// Run against actual published DSH services, not a mock ctx or a fake policy API.
import test from 'node:test';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import * as plugin from '../dsh.mjs';

const modules = process.env.OPEN_GUARDRAIL_HOST_MODULES;
const enabled = !!modules && !!process.env.OPEN_GUARDRAIL_BINARIES;
const load = (name) => import(pathToFileURL(join(modules, '@deepseek-ai', name, 'lib/index.js')).href);

test('DSH native pipeline: guarded execution, pre-execute policy and agent isolation', { skip: !enabled }, async () => {
  const { Context } = await load('cordis');
  const { default: Prompt } = await load('dsh-system-prompt');
  const { default: Tools } = await load('dsh-tools');
  const native = await load('dsh-tool-executor-native');
  const { default: Skills } = await load('dsh-skill');
  const ctx = new Context();
  const root = await ctx.plugin(async (context) => {
    await context.plugin(Prompt, {});
    await context.plugin(Tools, {});
    await context.plugin(native);
    await context.plugin(Skills, {});
    await context.plugin(plugin);
  });
  let id = 0;
  const owner = {};
  // ToolsService documents owner-scoped calls outside model turns; no log is
  // requested here. Real Agent-loop/model acceptance is a separate opt-in run.
  const call = async (method, params = {}, agent = owner) => {
    const batch = await ctx.tools.execute({ calls: [{ type: 'tool_call', id: `call-${++id}`,
      name: `guardrail_cleanup_${method}`, arguments: JSON.stringify(params) }], agent,
      signal: new AbortController().signal });
    return batch.results[0].result;
  };
  try {
    assert.equal(ctx.tools.schemas().length, 4);
    assert((await ctx.skills.get('guardrail-cleanup')).content.includes('frozen policy'));
    const prepared = (await call('prepare', { requestId: 'one', targets: ['cache.tmp'] })).value;
    const submitted = await call('submit', { runId: prepared.runId, checks: ['temporary-only', 'batch-limit'].map(checkId => ({ checkId, verdict: 'PASS', summary: 'A single existing temporary file' })) });
    assert.equal(submitted.value.run.decision, 'ALLOW');
    // Another agent cannot use this Run even if it learns its public ID.
    assert.equal((await call('execute', { runId: prepared.runId, targets: prepared.targets }, {})).isError, true);
    const unregister = ctx.tools.guard(exec => exec.name.endsWith('_execute') ? 'Existing host policy denies execution' : undefined);
    assert.equal((await call('execute', { runId: prepared.runId, targets: prepared.targets })).isError, true);
    assert.equal((await call('status')).value.files.length, 4);
    unregister();
    const effect = await call('execute', { runId: prepared.runId, targets: prepared.targets });
    assert.equal(effect.isError, false);
    assert.deepEqual(effect.value.removed, ['cache.tmp']);
    assert.equal((await call('status')).value.files.length, 3);
    const publicValue = JSON.stringify(submitted.value);
    assert(!publicValue.includes('credential.json'));
  } finally { await root.dispose(); }
});

test('DSH still registers tools when the bundled Skill lacks a description', { skip: !modules }, async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-skill-'));
  const { Context } = await load('cordis');
  const { default: Prompt } = await load('dsh-system-prompt');
  const { default: Tools } = await load('dsh-tools');
  const { default: Skills } = await load('dsh-skill');
  let root;
  try {
    const source = await readFile(new URL('../dsh.mjs', import.meta.url), 'utf8');
    await writeFile(join(directory, 'dsh.mjs'), source
      .replace("'./bridge.mjs'", JSON.stringify(new URL('../bridge.mjs', import.meta.url).href)));
    await writeFile(join(directory, 'scenarios.mjs'),
      `import { scenarios as source } from ${JSON.stringify(new URL('../scenarios.mjs', import.meta.url).href)};
export const scenarios = source.map(scenario => ({ ...scenario, skill: { ...scenario.skill,
  url: new URL('./skills/guardrail-cleanup/SKILL.md', import.meta.url) } }));
`);
    await mkdir(join(directory, 'skills/guardrail-cleanup'), { recursive: true });
    const skill = await readFile(new URL('../skills/guardrail-cleanup/SKILL.md', import.meta.url), 'utf8');
    await writeFile(join(directory, 'skills/guardrail-cleanup/SKILL.md'), skill.replace(/^description:.*\n/m, ''));
    const altered = await import(pathToFileURL(join(directory, 'dsh.mjs')).href);
    const ctx = new Context();
    root = await ctx.plugin(async (context) => {
      await context.plugin(Prompt, {});
      await context.plugin(Tools, {});
      await context.plugin(Skills, {});
      await context.plugin(altered);
    });
    assert.equal(ctx.tools.schemas().length, 4);
    assert((await ctx.skills.get('guardrail-cleanup')).description.includes('frozen policy'));
  } finally { await root?.dispose(); await rm(directory, { recursive: true, force: true }); }
});
