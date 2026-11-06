// Fails if a Chinese string literal in the frontend scripts, or gallery text in examples/catalog.json, has no English entry in i18n.js.
// Run: node scripts/check-i18n.js
const fs=require('fs'),vm=require('vm');
const ctx={localStorage:{getItem:()=>null},navigator:{language:'en'},document:{documentElement:{}}};
vm.runInNewContext(fs.readFileSync('frontend/i18n.js','utf8')+';this.EN=EN',ctx);
const src=['frontend/workbench.js','frontend/conversations.js','frontend/mcp.js','frontend/examples.js'].map(path=>fs.readFileSync(path,'utf8')).join('\n');
const skip=new Set(['中文']);// ponytail: language toggle label and sample JSON stay as-is
const missing=[...src.matchAll(/'((?:[^'\\\n]|\\.)*)'/g)].map(m=>vm.runInNewContext(`'${m[1]}'`))
  .filter(s=>/[一-鿿]/.test(s)&&!skip.has(s)&&!s.startsWith('[\n')&&!(s in ctx.EN));
// Gallery text in examples/catalog.json is shown through t() too; project fields are example data.
const catalog=JSON.parse(fs.readFileSync('examples/catalog.json','utf8')).examples;
missing.push(...catalog.flatMap(ex=>[ex.title,ex.summary,...ex.groups.flatMap(g=>[g.title,g.note]),...ex.files.flatMap(f=>[f.title,f.note])]).filter(s=>s&&/[一-鿿]/.test(s)&&!(s in ctx.EN)));
if(missing.length){console.error('Missing English:\n'+[...new Set(missing)].join('\n'));process.exit(1);}
console.log('i18n ok');
