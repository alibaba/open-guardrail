import test from 'node:test';
import assert from 'node:assert/strict';
import { WorkerBridge } from '../bridge.mjs';
import { byId } from '../scenarios.mjs';

const cleanup = byId('file-cleanup');

const enabled = !!process.env.OPEN_GUARDRAIL_BINARIES;
const checks = (verdict) => ['temporary-only', 'batch-limit'].map(checkId => ({
  checkId, verdict, summary: 'Checked the actual frozen observation',
}));

test('stdio transport: denied Run, repaired scope, concurrent retries and actual files', { skip: !enabled }, async () => {
  const bridge = new WorkerBridge(cleanup);
  try {
    const denied = await bridge.request('prepare', { requestId: 'broad', targets: ['cache.tmp', 'orders.csv'] });
    const result = await bridge.request('submit', { runId: denied.runId, checks: checks('BLOCKED') });
    assert.equal(result.run.decision, 'DENY');
    await assert.rejects(bridge.request('execute', { runId: denied.runId, targets: denied.targets }), /decision_mismatch/);
    const params = { requestId: 'narrow', targets: ['cache.tmp', 'download.tmp'] };
    const [first, replay] = await Promise.all([bridge.request('prepare', params), bridge.request('prepare', params)]);
    assert.equal(first.runId, replay.runId);
    await bridge.request('submit', { runId: first.runId, checks: checks('PASS') });
    const executions = await Promise.allSettled([1, 2].map(() => bridge.request('execute', { runId: first.runId, targets: first.targets })));
    assert.equal(executions.filter(r => r.status === 'fulfilled').length, 1);
    assert.equal(executions.filter(r => r.status === 'rejected').length, 1);
    const state = await bridge.request('status');
    assert.deepEqual(state.files.map(f => f.name).sort(), ['config.yaml', 'orders.csv']);
    assert.equal(state.runs.length, 2);
    assert(!JSON.stringify(state).includes('credential.json'));
  } finally { await bridge.close(); }
});

test('pre-admission cancellation leaves the session usable; close stops future admission', { skip: !enabled }, async () => {
  const bridge = new WorkerBridge(cleanup);
  try {
    await assert.rejects(bridge.request('status', {}, AbortSignal.abort()), /cancelled before admission/);
    assert.equal((await bridge.request('status')).files.length, 4);
  } finally { await bridge.close(); }
  await assert.rejects(bridge.request('status'), /session stopped/);
});

test('in-flight cancellation drains the owned worker and prevents automatic restart', { skip: !enabled }, async () => {
  const bridge = new WorkerBridge(cleanup);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 20);
  try {
    await assert.rejects(bridge.request('status', {}, controller.signal), /cancelled/);
    await assert.rejects(bridge.request('status'), /session stopped/);
  } finally { clearTimeout(timer); await bridge.close(); }
});

test('startup budget exceeds ordinary calls; initialized calls keep the 90s limit', { skip: !enabled }, async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const bridge = new WorkerBridge(cleanup);
  try {
    const starting = bridge.request('status');
    await Promise.resolve();
    t.mock.timers.tick(90_001);
    assert.equal((await starting).files.length, 4);
    const ordinary = bridge.request('status');
    await Promise.resolve();
    t.mock.timers.tick(90_001);
    await assert.rejects(ordinary, /timed out/);
    await assert.rejects(bridge.request('status'), /session stopped/);
  } finally { await bridge.close(); }
});
