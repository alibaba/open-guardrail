#!/usr/bin/env node
// Deterministic installation self-check. Real-model trials use the host guide.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { WorkerBridge } from './bridge.mjs';
import { byId } from './scenarios.mjs';

if (process.argv.length !== 2) {
  console.error('Usage: node trial.mjs');
  process.exit(2);
}

const bridge = new WorkerBridge(byId('file-cleanup'));
const checks = verdict => ['temporary-only', 'batch-limit'].map(checkId => ({
  checkId, verdict, summary: verdict === 'PASS' ? 'Observed temporary files within the frozen limit' : 'Protected files and too many targets',
}));
try {
  const initial = await bridge.request('status');
  const broad = await bridge.request('prepare', { requestId: 'trial-broad', targets: initial.files.map(file => file.name) });
  const denied = await bridge.request('submit', { runId: broad.runId, checks: checks('BLOCKED') });
  assert.equal(denied.run.decision, 'DENY');
  await assert.rejects(bridge.request('execute', { runId: broad.runId, targets: broad.targets }));
  assert.deepEqual((await bridge.request('status')).files, initial.files);
  const narrow = await bridge.request('prepare', { requestId: 'trial-narrow', targets: ['cache.tmp', 'download.tmp'] });
  const allowed = await bridge.request('submit', { runId: narrow.runId, checks: checks('PASS') });
  assert.equal(allowed.run.decision, 'ALLOW');
  const executed = await bridge.request('execute', { runId: narrow.runId, targets: narrow.targets });
  assert.deepEqual([...executed.removed].sort(), ['cache.tmp', 'download.tmp']);
  assert.deepEqual(executed.files, initial.files.filter(file => !file.name.endsWith('.tmp')));
  const decisions = [];
  for (const run of [broad, narrow]) {
    const status = await bridge.request('status', { runId: run.runId });
    const stored = status.runs[0];
    assert.equal(stored.run.status, 'FINISHED');
    assert.equal(stored.run.policyRevisionId, stored.revision.id);
    assert.equal(stored.run.specDigest, stored.revision.digest);
    assert.equal(stored.acceptedSubmission.id, stored.run.finalSubmissionId);
    assert.equal(stored.acceptedSubmission.runId, stored.run.id);
    assert.equal(stored.acceptedSubmission.finishRun, true);
    assert.equal(stored.acceptedSubmission.snapshot.checks.length, 2);
    for (const check of stored.acceptedSubmission.snapshot.checks) {
      assert.equal(check.evidence.length, 1);
      assert.equal(createHash('sha256').update(check.evidence[0].resultSummary).digest('hex'),
        stored.run.changeSet.observationDigest);
    }
    decisions.push({ runId: run.runId, decision: stored.run.decision, finalSubmissionId: stored.run.finalSubmissionId });
  }
  console.log(JSON.stringify({ mode: 'deterministic-installation-trial', passed: true,
    decisions, removed: executed.removed, protectedFilesUnchanged: true }, null, 2));
} finally {
  await bridge.close();
}
