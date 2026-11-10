import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, copyFile, writeFile, rm, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
const doctor = fileURLToPath(new URL('../doctor.mjs', import.meta.url));

test('doctor reports actionable missing runtime errors without exposing environment secrets', () => {
  const result = spawnSync(process.execPath, [doctor, '--live'], { encoding: 'utf8', env: {
    ...process.env, OPEN_GUARDRAIL_PYTHON: '/missing/python', OPEN_GUARDRAIL_BINARIES: '',
    OPEN_GUARDRAIL_SECRET_TEST: 'must-never-appear',
  } });
  assert.equal(result.status, 1);
  const report = JSON.parse(result.stdout);
  assert(report.checks.some(item => item.name === 'python' && !item.ok && item.fix));
  assert(report.checks.some(item => item.name === 'native-binaries' && !item.ok && item.fix));
  assert(!result.stdout.includes('must-never-appear'));
  assert(report.checks.some(item => item.name === 'isolated-service' && item.skipped === true && item.reason));
});

test('doctor rejects malformed host selection', () => {
  for (const args of [['--host'], ['pi'], ['--live', '--live'], ['--host', 'pi', '--host', 'dsh']]) {
    const result = spawnSync(process.execPath, [doctor, ...args], { encoding: 'utf8' });
    assert.equal(result.status, 2);
  }
});

test('trial rejects unexpected flags before starting a worker', () => {
  const trial = fileURLToPath(new URL('../trial.mjs', import.meta.url));
  const result = spawnSync(process.execPath, [trial, '--live'], { encoding: 'utf8', env: {
    ...process.env, OPEN_GUARDRAIL_PYTHON: '/must-not-start-python',
  } });
  assert.equal(result.status, 2);
  assert.equal(result.stdout, '');
  assert.match(result.stderr, /Usage: node trial.mjs/);
});

test('host pins follow package metadata for both adapters', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-doctor-'));
  try {
    for (const file of ['doctor.mjs', 'bridge.mjs']) {
      await copyFile(new URL('../' + file, import.meta.url), join(directory, file));
    }
    const engines = { pi: '9.8.7', dsh: '2.3.4' };
    await writeFile(join(directory, 'package.json'), JSON.stringify({ version: 'test', engines }));
    const bin = join(directory, 'bin');
    await mkdir(bin);
    for (const [name, version] of Object.entries({ ...engines, pnpm: '1.0.0' })) {
      await writeFile(join(bin, name), `#!/bin/sh\nprintf '%s\n' '${version}'\n`, { mode: 0o755 });
    }
    for (const [host, version] of Object.entries(engines)) {
      const result = spawnSync(process.execPath, [join(directory, 'doctor.mjs'), '--host', host], {
        encoding: 'utf8', env: { ...process.env, PATH: bin + ':' + process.env.PATH },
      });
      const report = JSON.parse(result.stdout);
      assert(report.checks.some(item => item.name === 'host-version' && item.ok && item.detail === version));
    }
  } finally { await rm(directory, { recursive: true, force: true }); }
});
