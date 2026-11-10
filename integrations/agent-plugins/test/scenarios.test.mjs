import test from 'node:test';
import assert from 'node:assert/strict';
import { copyFile, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import openGuardrail from '../pi.mjs';
import { byId, scenarios } from '../scenarios.mjs';

test('scenario lookup rejects an unknown id before starting a worker', () => {
  assert.equal(byId('file-cleanup'), scenarios[0]);
  assert.throws(() => byId('missing'), /Unknown OpenGuardrail scenario: missing/);
});

test('installed package loads bundled scenario even inside a checkout', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-scenarios-'));
  const checkout = join(directory, 'scenarios/file-cleanup');
  const plugin = join(directory, 'integrations/agent-plugins');
  const packaged = join(plugin, 'assets/scenarios/file-cleanup');
  try {
    await mkdir(checkout, { recursive: true });
    await mkdir(packaged, { recursive: true });
    await copyFile(new URL('../scenarios.mjs', import.meta.url), join(plugin, 'scenarios.mjs'));
    await writeFile(join(checkout, 'scenario.mjs'), "export default { id: 'checkout' };\n");
    await writeFile(join(packaged, 'scenario.mjs'), "export default { id: 'packaged' };\n");
    const url = pathToFileURL(join(plugin, 'scenarios.mjs')).href;
    assert.equal((await import(url)).scenarios[0].id, 'packaged');
    await writeFile(join(plugin, 'pack.mjs'), '// source-only marker\n');
    assert.equal((await import(url + '?checkout')).scenarios[0].id, 'checkout');
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('one pi entrypoint registers the declared scenario tools', async () => {
  const registered = [];
  const events = new Map();
  openGuardrail({
    registerTool(tool) { registered.push(tool); },
    on(name, callback) { events.set(name, callback); },
  });
  assert.deepEqual(scenarios.map((scenario) => scenario.id), ['file-cleanup']);
  assert.deepEqual(registered.map((tool) => tool.name), scenarios.flatMap((scenario) => scenario.tools.map((tool) => tool.name)));
  assert.equal(new Set(registered.map((tool) => tool.name)).size, registered.length);
  assert.equal(typeof events.get('session_shutdown'), 'function');
  await events.get('session_shutdown')();
});
