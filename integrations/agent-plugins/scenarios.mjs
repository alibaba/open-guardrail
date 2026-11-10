import { existsSync } from 'node:fs';

// pack.mjs is source-only: npm excludes it from the published package. Use
// bundled assets in an installed package even when it sits below a checkout.
const checkout = new URL('../../scenarios/', import.meta.url);
const packaged = new URL('./assets/scenarios/', import.meta.url);
const root = existsSync(new URL('./pack.mjs', import.meta.url)) ? checkout : packaged;

const cleanup = (await import(new URL('file-cleanup/scenario.mjs', root))).default;
export const scenarios = [cleanup];

export function byId(id) {
  const scenario = scenarios.find((item) => item.id === id);
  if (!scenario) throw new Error(`Unknown OpenGuardrail scenario: ${id}`);
  return scenario;
}
