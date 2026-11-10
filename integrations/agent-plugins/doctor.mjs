#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { readFileSync, accessSync, constants } from 'node:fs';
import { join, isAbsolute } from 'node:path';
import { WorkerBridge } from './bridge.mjs';

const checks = [];
function check(name, run, fix) {
  try {
    const detail = run();
    checks.push({ name, ok: true, detail });
    return detail;
  } catch {
    checks.push({ name, ok: false, fix });
    return undefined;
  }
}
function command(program, args) {
  const result = spawnSync(program, args, { encoding: 'utf8', timeout: 10000, maxBuffer: 65536 });
  if (result.error || result.status !== 0) throw new Error('command failed');
  return result.stdout.trim();
}
const args = process.argv.slice(2);
const hostIndex = args.indexOf('--host');
const host = hostIndex >= 0 ? args[hostIndex + 1] : undefined;
const expectedLength = (hostIndex >= 0 ? 2 : 0) + (args.includes('--live') ? 1 : 0);
if (args.length !== expectedLength || args.some(arg => !['--live', '--host', 'pi', 'dsh'].includes(arg)) ||
    (hostIndex >= 0 && !['pi', 'dsh'].includes(host))) {
  console.error('Usage: node doctor.mjs [--host pi|dsh] [--live]');
  process.exit(2);
}
const metadata = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'));
check('node', () => {
  if (Number(process.versions.node.split('.')[0]) < 22) throw new Error('unsupported Node');
  return process.versions.node;
}, 'Install Node.js 22 or newer.');
check('python', () => command(process.env.OPEN_GUARDRAIL_PYTHON || 'python3',
  ['-c', 'import sys; assert sys.version_info >= (3,9); print("%s.%s.%s" % sys.version_info[:3])']),
'Install Python 3.9+ or set OPEN_GUARDRAIL_PYTHON to its executable.');
check('native-binaries', () => {
  const directory = process.env.OPEN_GUARDRAIL_BINARIES || '';
  if (!isAbsolute(directory)) throw new Error('absolute directory required');
  for (const name of ['open-guardrail', 'guardrailctl']) accessSync(join(directory, name), constants.X_OK);
  return 'Both executables are accessible';
}, 'Source the installed env.sh to select the matching native package.');
if (host === 'dsh') check('host-node', () => {
  const [major, minor] = process.versions.node.split('.').map(Number);
  if (major < 24 || (major === 24 && minor < 9)) throw new Error('unsupported DSH Node');
  return process.versions.node;
}, 'DeepSeek Harness 0.2.2-rc.1 requires Node.js 24.9 or newer.');
if (host) check('host-version', () => {
  const expected = metadata.engines[host];
  const output = command(host, ['--version']);
  if (!output.split(/\s+/).includes(expected)) throw new Error('unverified host version');
  if (host === 'dsh') command('pnpm', ['--version']);
  return expected;
}, `Install the documented ${host} version${host === 'dsh' ? ' and pnpm' : ''}, and make its command available on PATH.`);
if (args.includes('--live') && checks.some(item => !item.ok)) {
  checks.push({ name: 'isolated-service', ok: false, skipped: true,
    reason: 'Prerequisite checks failed. Fix the failed checks before running --live again.' });
} else if (args.includes('--live')) {
  const { byId } = await import('./scenarios.mjs');
  const bridge = new WorkerBridge(byId('file-cleanup'));
  try {
    const status = await bridge.request('status');
    const response = await fetch(status.server + '/api/v1alpha1/meta/version', {
      signal: AbortSignal.timeout(10000), redirect: 'error',
    });
    if (!response.ok) throw new Error('version query failed');
    const version = await response.json();
    if (version.version !== metadata.version) throw new Error('version mismatch');
    checks.push({ name: 'isolated-service', ok: true, detail: { version: version.version, sampleFiles: status.files.length } });
  } catch {
    checks.push({ name: 'isolated-service', ok: false,
      fix: 'Install the native/plugin pair from the same version. Check loopback access and writable temporary storage.' });
  } finally { await bridge.close(); }
}
console.log(JSON.stringify({ packageVersion: metadata.version, checks,
  hostReadiness: 'Confirm model login and tool/Skill discovery inside the host using the trial guide.' }, null, 2));
process.exitCode = checks.every(item => item.ok) ? 0 : 1;
