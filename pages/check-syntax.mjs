import { readdir } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = new URL('./', import.meta.url);
for (const directory of ['', 'src/', 'test/']) {
  for (const file of await readdir(new URL(directory, root))) {
    if (!/\.(mjs|js)$/.test(file)) continue;
    const result = spawnSync(process.execPath, ['--check', fileURLToPath(new URL(directory+file, root))], {stdio:'inherit'});
    if (result.error) throw result.error;
    if (result.status !== 0) process.exit(result.status || 1);
  }
}
