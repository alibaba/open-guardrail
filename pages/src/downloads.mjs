import { release } from './release.mjs';

export const nativePlatforms = [
  ['darwin-arm64', 'macOS · Apple Silicon'],
  ['darwin-amd64', 'macOS · Intel'],
  ['linux-amd64', 'Linux · amd64'],
  ['linux-arm64', 'Linux · arm64'],
].map(([key,label]) => {
  const asset = release.assets.find(a => a.name.endsWith(`-${key}.tar.gz`));
  if (!asset) throw new Error(`Missing native release asset: ${key}`);
  return {key,label,url:asset.url};
});

export function downloadForPlatform(key) {
  return nativePlatforms.find(platform => platform.key === key)?.url ?? null;
}
