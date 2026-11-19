import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { checkBuilt } from './src/check-links.mjs';

const dist=path.join(path.dirname(fileURLToPath(import.meta.url)),'dist');
const base=`/${(process.env.SITE_BASE_PATH||'').replace(/^\/+|\/+$/g,'')}/`.replace('//','/');
const {failures,links,anchors,htmlFiles}=await checkBuilt(dist,base);
if (failures.length) { console.error(failures.join('\n')); process.exit(1); }
console.log(`Verified ${links} local links/assets and ${anchors} section anchors across ${htmlFiles} HTML files at base ${base}.`);
