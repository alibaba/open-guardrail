const {test} = require("node:test");
const assert = require("node:assert/strict");
const {readFileSync} = require("node:fs");
const {join} = require("node:path");
const vm = require("node:vm");

test("the console opens directly and makes no identity requests", async () => {
 const html = readFileSync(join(__dirname, "static/index.html"), "utf8");
 const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map(match => [match[1], {
  hidden:true, textContent:"", addEventListener(){}, removeAttribute(){}
 }]));
 const paths = [], modules = [], lifecycle = [];
 const window = {};
 for (const name of ["ConsoleUI","SpecUI","RunUI","AuditUI"]) {
  window[name] = {setAvailable(value){modules.push([name,value]);}, route(){lifecycle.push("route");}};
 }
 vm.runInNewContext(readFileSync(join(__dirname,"static/app.js"),"utf8"), {
  window, document:{querySelector(selector){return elements.get(selector.slice(1));}},
  AbortSignal, setText(el,key){el.textContent=key;},
  fetch:async(path, options)=>{
   paths.push(path);
   assert.equal(options.credentials,"omit");
   return {ok:true,status:200,json:async()=>path==="/readyz"?{status:"ready"}:{version:"test"}};
  }
 });
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(elements.get("home").hidden,false);
 assert.equal(modules.length,4);
 assert.ok(modules.every(([,value])=>value));
 assert.deepEqual(lifecycle,["route"]);
 assert.deepEqual(paths.sort(),["/api/v1alpha1/meta/version","/readyz"]);
 assert.equal(elements.get("version").textContent,"test");
 for (const id of ["setup","login","logout","password-form","create-key"]) assert.equal(elements.has(id),false);
});
