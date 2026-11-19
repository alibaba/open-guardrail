import { readdir, readFile, stat } from 'node:fs/promises';
import path from 'node:path';

export async function checkBuilt(dist, base) {
  const failures = [], cachedIds = new Map();
  let links = 0, anchors = 0;
  async function walk(dir) {
    const entries = await readdir(dir, {withFileTypes:true});
    return (await Promise.all(entries.map(e => e.isDirectory() ? walk(path.join(dir,e.name)) : [path.join(dir,e.name)]))).flat();
  }
  const files = (await walk(dist)).filter(f => f.endsWith('.html'));
  function decode(value, source, href) {
    try { return decodeURIComponent(value); }
    catch { failures.push(`${path.relative(dist,source)}: invalid URL encoding: ${href}`); return null; }
  }
  async function checkAnchor(file, hash, source, href) {
    if (!hash || !file.endsWith('.html')) return;
    const decoded = decode(hash,source,href);
    if (decoded === null) return;
    if (!cachedIds.has(file)) {
      const html = await readFile(file,'utf8');
      cachedIds.set(file,new Set([...html.matchAll(/\bid="([^"]+)"/g)].map(m=>m[1])));
    }
    if (cachedIds.get(file).has(decoded)) anchors++;
    else failures.push(`${path.relative(dist,source)}: missing anchor ${href}`);
  }
  for (const file of files) {
    const html = await readFile(file,'utf8');
    for (const match of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
      const href = match[1].replaceAll('&amp;','&');
      if (href.startsWith('#')) { await checkAnchor(file,href.slice(1),file,href); continue; }
      if (!href.startsWith('/') || href.startsWith('//')) continue;
      if (!href.startsWith(base)) { failures.push(`${path.relative(dist,file)}: outside deployment base: ${href}`); continue; }
      const pathname = decode(href.split('#')[0].split('?')[0].slice(base.length),file,href);
      if (pathname === null) continue;
      let target = path.resolve(dist,pathname);
      if (!target.startsWith(path.resolve(dist)+path.sep) && target !== path.resolve(dist)) {
        failures.push(`${path.relative(dist,file)}: outside output directory: ${href}`); continue;
      }
      try {
        if ((await stat(target)).isDirectory()) target=path.join(target,'index.html');
        await stat(target); links++;
        await checkAnchor(target,href.split('#')[1],file,href);
      } catch { failures.push(`${path.relative(dist,file)}: missing ${href}`); }
    }
  }
  return {failures,links,anchors,htmlFiles:files.length};
}
