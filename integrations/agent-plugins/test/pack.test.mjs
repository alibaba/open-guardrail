import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

test('packed archive runs outside checkout with legal files and original shared verifier', {
  skip: !process.env.OPEN_GUARDRAIL_BINARIES,
}, async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-package-'));
  const cwd = fileURLToPath(new URL('../', import.meta.url));
  let bridge;
  try {
    execFileSync(process.execPath, ['pack.mjs'], { cwd, stdio: 'pipe' });
    const packed = JSON.parse(execFileSync('npm', ['pack', '--offline', '--ignore-scripts', '--json', '--pack-destination', directory], { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }))[0];
    const names = packed.files.map(file => file.path);
    for (const file of ['assets/cleanup-demo/demo.py', 'assets/cleanup-demo/policy.yaml', 'LICENSE', 'NOTICE', 'verify-host-exit.py', 'doctor.mjs', 'install.py', 'trial.mjs', 'README.zh-CN.md', 'skills/guardrail-cleanup/SKILL.md']) assert(names.includes(file));
    assert(!names.some(name => /node_modules|session\.json|credential\.json|test_worker|enterprise/.test(name)));
    execFileSync('tar', ['-xzf', join(directory, packed.filename), '-C', directory]);
    const installed = join(directory, 'package');
    assert.equal(await readFile(join(installed, 'assets/cleanup-demo/demo.py'), 'utf8'), await readFile(new URL('../../../examples/cleanup-demo/demo.py', import.meta.url), 'utf8'));
    // Ten serial calls allow 420s startup + 9 * 90s, with cleanup/scheduling margin.
    const trial = JSON.parse(execFileSync(process.execPath, [join(installed, 'trial.mjs')], { cwd: directory, encoding: 'utf8', timeout: 1500000 }));
    assert.equal(trial.passed, true);
    assert.deepEqual(trial.decisions.map(item => item.decision), ['DENY', 'ALLOW']);
    const { CleanupBridge } = await import(pathToFileURL(join(installed, 'bridge.mjs')).href);
    bridge = new CleanupBridge();
    const prepared = await bridge.request('prepare', { requestId: 'packed', targets: ['cache.tmp'] });
    const final = await bridge.request('submit', { runId: prepared.runId, checks: ['temporary-only', 'batch-limit'].map(checkId => ({ checkId, verdict: 'PASS', summary: 'One observed temporary file within the frozen limit' })) });
    assert.equal(final.run.decision, 'ALLOW');
    const result = await bridge.request('execute', { runId: prepared.runId, targets: prepared.targets });
    assert.deepEqual(result.removed, ['cache.tmp']);
    assert.deepEqual(result.files.map(file => file.name).sort(), ['config.yaml', 'download.tmp', 'orders.csv']);
  } finally { await bridge?.close(); await rm(directory, { recursive: true, force: true }); }
});
