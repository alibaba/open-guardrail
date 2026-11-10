import test from 'node:test';
import assert from 'node:assert/strict';
import { copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

test('DSH keeps one worker per agent and scenario and closes only owned workers', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-dsh-lifecycle-'));
  try {
    await copyFile(new URL('../dsh.mjs', import.meta.url), join(directory, 'dsh.mjs'));
    await writeFile(join(directory, 'scenarios.mjs'), `
export const scenarios = ['first', 'second'].map(id => ({ id, tools: [{
  name: 'tool_' + id, method: 'status', description: id, schema: { type: 'object' }
}] }));
`);
    await writeFile(join(directory, 'bridge.mjs'), `
export const instances = [];
export class WorkerBridge {
  constructor(scenario) { this.id = scenario.id; this.closed = 0; instances.push(this); }
  async request() { if (this.closed) throw new Error('closed'); return this.id; }
  async close() { this.closed++; }
}
`);
    const { apply } = await import(pathToFileURL(join(directory, 'dsh.mjs')).href);
    const { instances } = await import(pathToFileURL(join(directory, 'bridge.mjs')).href);
    const tools = new Map();
    const events = new Map();
    let dispose;
    apply({
      inject() {},
      tools: { register(tool) { tools.set(tool.name, tool); } },
      on(name, handler) { events.set(name, handler); },
      effect(register) { dispose = register(); },
    });
    const first = {};
    const second = {};
    for (const agent of [first, second]) {
      for (const name of ['tool_first', 'tool_second']) {
        assert.equal(await tools.get(name).execute({}, { agent }), name.slice(5));
      }
    }
    assert.equal(await tools.get('tool_first').execute({}, { agent: first }), 'first');
    assert.equal(instances.length, 4);
    assert.deepEqual(instances.map(item => item.id), ['first', 'second', 'first', 'second']);
    await events.get('agent/disposed')({ agent: first });
    assert.deepEqual(instances.map(item => item.closed), [1, 1, 0, 0]);
    assert.equal(await tools.get('tool_first').execute({}, { agent: second }), 'first');
    await dispose();
    assert.deepEqual(instances.map(item => item.closed), [1, 1, 1, 1]);
  } finally { await rm(directory, { recursive: true, force: true }); }
});
