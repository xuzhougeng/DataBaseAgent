// Fails if a Chinese string literal in workbench.js has no English entry in i18n.js.
// Run: node scripts/check-i18n.js
const fs=require('fs'),vm=require('vm');
const ctx={localStorage:{getItem:()=>null},navigator:{language:'en'},document:{documentElement:{}}};
vm.runInNewContext(fs.readFileSync('frontend/i18n.js','utf8')+';this.EN=EN',ctx);
const src=['frontend/workbench.js','frontend/conversations.js','frontend/mcp.js'].map(path=>fs.readFileSync(path,'utf8')).join('\n');
const skip=new Set(['中文']);// ponytail: language toggle label and sample JSON stay as-is
const missing=[...src.matchAll(/'((?:[^'\\\n]|\\.)*)'/g)].map(m=>vm.runInNewContext(`'${m[1]}'`))
  .filter(s=>/[一-鿿]/.test(s)&&!skip.has(s)&&!s.startsWith('[\n')&&!(s in ctx.EN));
if(missing.length){console.error('Missing English:\n'+[...new Set(missing)].join('\n'));process.exit(1);}
console.log('i18n ok');
