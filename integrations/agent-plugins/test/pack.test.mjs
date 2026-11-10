import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { access, mkdtemp, readFile, realpath, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

function pack(directory) {
  const cwd = fileURLToPath(new URL('../', import.meta.url));
  execFileSync(process.execPath, ['pack.mjs'], { cwd, stdio: 'pipe' });
  return JSON.parse(execFileSync('npm', ['pack', '--offline', '--ignore-scripts', '--json', '--pack-destination', directory], {
    cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, npm_config_cache: join(directory, 'npm-cache') },
  }))[0];
}

test('packed archive contains the scenario and resolves its worker and Skill', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-package-'));
  try {
    const packed = pack(directory);
    const names = packed.files.map(file => file.path);
    for (const file of ['assets/scenarios/file-cleanup/scenario.mjs', 'assets/scenarios/file-cleanup/worker.py', 'assets/scenarios/file-cleanup/runtime.py', 'assets/scenarios/file-cleanup/policy.yaml', 'assets/scenarios/file-cleanup/skills/guardrail-cleanup/SKILL.md', 'scenarios.mjs', 'LICENSE', 'NOTICE', 'verify-host-exit.py', 'doctor.mjs', 'install.py', 'trial.mjs', 'README.zh-CN.md', 'skills/guardrail-cleanup/SKILL.md']) assert(names.includes(file));
    assert(!names.includes('assets/scenarios/file-cleanup/demo.py'));
    assert(!names.some(name => name.startsWith('assets/cleanup-demo/')));
    assert(!names.some(name => /node_modules|session\.json|credential\.json|test_worker|enterprise/.test(name)));
    assert(!names.includes('pack.mjs'));
    execFileSync('tar', ['-xzf', join(directory, packed.filename), '-C', directory]);
    const installed = join(directory, 'package');
    assert.equal(await readFile(join(installed, 'assets/scenarios/file-cleanup/runtime.py'), 'utf8'), await readFile(new URL('../../../scenarios/file-cleanup/runtime.py', import.meta.url), 'utf8'));
    const { byId } = await import(pathToFileURL(join(installed, 'scenarios.mjs')).href);
    const scenario = byId('file-cleanup');
    await access(fileURLToPath(scenario.workerUrl));
    await access(fileURLToPath(scenario.skill.url));
    assert.equal(await realpath(fileURLToPath(scenario.workerUrl)), await realpath(join(installed, 'assets/scenarios/file-cleanup/worker.py')));
    assert.equal(await realpath(fileURLToPath(scenario.skill.url)), await realpath(join(installed, 'assets/scenarios/file-cleanup/skills/guardrail-cleanup/SKILL.md')));
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('packed archive runs outside checkout with original shared verifier', {
  skip: !process.env.OPEN_GUARDRAIL_BINARIES,
}, async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-package-live-'));
  let bridge;
  try {
    const packed = pack(directory);
    execFileSync('tar', ['-xzf', join(directory, packed.filename), '-C', directory]);
    const installed = join(directory, 'package');
    // Ten serial calls allow 420s startup + 9 * 90s, with cleanup/scheduling margin.
    const trial = JSON.parse(execFileSync(process.execPath, [join(installed, 'trial.mjs')], { cwd: directory, encoding: 'utf8', timeout: 1500000 }));
    assert.equal(trial.passed, true);
    assert.deepEqual(trial.decisions.map(item => item.decision), ['DENY', 'ALLOW']);
    const { WorkerBridge } = await import(pathToFileURL(join(installed, 'bridge.mjs')).href);
    const { byId } = await import(pathToFileURL(join(installed, 'scenarios.mjs')).href);
    bridge = new WorkerBridge(byId('file-cleanup'));
    const prepared = await bridge.request('prepare', { requestId: 'packed', targets: ['cache.tmp'] });
    const final = await bridge.request('submit', { runId: prepared.runId, checks: ['temporary-only', 'batch-limit'].map(checkId => ({ checkId, verdict: 'PASS', summary: 'One observed temporary file within the frozen limit' })) });
    assert.equal(final.run.decision, 'ALLOW');
    const result = await bridge.request('execute', { runId: prepared.runId, targets: prepared.targets });
    assert.deepEqual(result.removed, ['cache.tmp']);
    assert.deepEqual(result.files.map(file => file.name).sort(), ['config.yaml', 'download.tmp', 'orders.csv']);
  } finally { await bridge?.close(); await rm(directory, { recursive: true, force: true }); }
});
