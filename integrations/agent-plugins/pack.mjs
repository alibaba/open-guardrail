import { mkdir, copyFile, rm } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const root = new URL('../../', import.meta.url);
// Both directories are generated outputs, never scenario source.
await rm(new URL('./assets/', import.meta.url), { recursive: true, force: true });
await rm(new URL('./skills/', import.meta.url), { recursive: true, force: true });
const destination = new URL('./assets/scenarios/file-cleanup/', import.meta.url);
await mkdir(destination, { recursive: true });
for (const name of ['scenario.mjs', 'worker.py', 'runtime.py', 'policy.yaml']) {
  await copyFile(new URL(`scenarios/file-cleanup/${name}`, root), new URL(name, destination));
}
const skill = new URL('./skills/guardrail-cleanup/', import.meta.url);
const scenarioSkill = new URL('./skills/guardrail-cleanup/', destination);
await mkdir(skill, { recursive: true });
await mkdir(scenarioSkill, { recursive: true });
await copyFile(new URL('scenarios/file-cleanup/skills/guardrail-cleanup/SKILL.md', root),
  new URL('SKILL.md', skill));
await copyFile(new URL('scenarios/file-cleanup/skills/guardrail-cleanup/SKILL.md', root),
  new URL('SKILL.md', scenarioSkill));
for (const name of ['LICENSE', 'NOTICE']) {
  await copyFile(new URL(name, root), fileURLToPath(new URL(`./${name}`, import.meta.url)));
}
