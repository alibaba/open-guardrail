import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const modules = process.env.OPEN_GUARDRAIL_HOST_MODULES;
test('pi native resource loader discovers the packaged tools and Skill', { skip: !modules }, async () => {
  const { DefaultResourceLoader, SettingsManager } = await import(pathToFileURL(join(modules, '@earendil-works/pi-coding-agent/dist/index.js')).href);
  const directory = await mkdtemp(join(tmpdir(), 'guardrail-pi-loader-'));
  try {
    const loader = new DefaultResourceLoader({ cwd: directory, agentDir: directory,
      settingsManager: SettingsManager.inMemory(), noContextFiles: true, noExtensions: true,
      additionalExtensionPaths: [fileURLToPath(new URL('../pi.mjs', import.meta.url))],
      additionalSkillPaths: [fileURLToPath(new URL('../skills', import.meta.url))] });
    await loader.reload();
    const loaded = loader.getExtensions();
    assert.deepEqual(loaded.errors, []);
    const names = loaded.extensions.flatMap(extension => [...extension.tools.keys()]);
    assert(names.includes('guardrail_cleanup_execute'));
    assert.equal(names.filter(name => name.startsWith('guardrail_cleanup_')).length, 4);
    assert(loader.getSkills().skills.some(skill => skill.name === 'guardrail-cleanup'));
  } finally { await rm(directory, { recursive: true, force: true }); }
});
