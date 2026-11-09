import { mkdir, copyFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const root = new URL('../../', import.meta.url);
const destination = new URL('./assets/cleanup-demo/', import.meta.url);
await mkdir(destination, { recursive: true });
for (const name of ['demo.py', 'policy.yaml']) {
  await copyFile(new URL(`examples/cleanup-demo/${name}`, root), new URL(name, destination));
}
for (const name of ['LICENSE', 'NOTICE']) {
  await copyFile(new URL(name, root), fileURLToPath(new URL(`./${name}`, import.meta.url)));
}
