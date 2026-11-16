import test from 'node:test';
import assert from 'node:assert/strict';
import { nativePlatforms, downloadForPlatform } from '../src/downloads.mjs';

test('platform identifiers resolve only to the matching published native archive', () => {
  assert.equal(nativePlatforms.length,4);
  for (const {key,url} of nativePlatforms) {
    assert.equal(downloadForPlatform(key),url);
    assert.equal(new URL(url).origin,'https://github.com');
    assert.ok(url.endsWith(`-${key}.tar.gz`));
  }
});

test('URL schemes, arbitrary URLs and object property names cannot select downloads', () => {
  for (const key of ['javascript:alert(1)','data:text/html,hello','//example.com/file','https://github.com/alibaba/open-guardrail/releases/download/other/file','<img src=x onerror=alert(1)>','__proto__','constructor','toString','',null,undefined,0,{}]) {
    assert.equal(downloadForPlatform(key),null);
  }
});

test('the actual change handler clears an injected URL and recovers on a valid selection',async t=>{
  const oldDocument=globalThis.document;
  t.after(()=>{if(oldDocument===undefined)delete globalThis.document;else globalThis.document=oldDocument;});
  let change;
  const link={href:nativePlatforms[0].url,removeAttribute(name){if(name==='href')delete this.href;}};
  globalThis.document={
    body:{dataset:{locale:'en'}},addEventListener(){},querySelectorAll(){return [];},
    querySelector(selector){
      if(selector==='#download-platform')return {addEventListener(event,handler){if(event==='change')change=handler;}};
      if(selector==='#download-native')return link;
      return null;
    },
  };
  await import('../src/site.js');
  for(const value of ['javascript:alert(1)','data:text/html,hello','https://example.com/file','__proto__']) {
    change({currentTarget:{value}});assert.equal(link.href,undefined);
  }
  for(const platform of nativePlatforms) {
    change({currentTarget:{value:platform.key},target:{value:'javascript:alert(1)'}});
    assert.equal(link.href,platform.url);
  }
});
