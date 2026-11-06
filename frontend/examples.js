'use strict';
// Built-in examples (examples/catalog.json, embedded in the binary). Previews only read;
// "创建示例项目" adds one project per example and language and reopens it on later clicks.
// The data follows the UI language: switching refetches the catalog in that language.

let exampleCatalog=null,exampleLoad=null,exampleLoadLang='',exampleError='';
function exampleList(){
  if(!exampleLoad||exampleLoadLang!==lang){
    const want=exampleLoadLang=lang;exampleError='';
    exampleLoad=request('examples?lang='+want).then(r=>{if(want===lang){exampleCatalog=r.examples||[];render();}}).catch(e=>{if(want===lang){exampleError=e.message||String(e);render();}});
  }
  return exampleCatalog||[];
}
function retryExamples(){exampleLoad=null;exampleError='';render();}
const exampleById=id=>(exampleCatalog||[]).find(ex=>ex.id===id);
const exampleDataLang=()=>lang==='en'?'en':'zh';
const exampleProject=ex=>db.projects.find(p=>p.example===ex.id&&(p.exampleLang||'zh')===exampleDataLang());
const exampleSiteURL=id=>`/api/example-site?id=${encodeURIComponent(id)}&lang=${exampleDataLang()}`;
const fileName=path=>path.split('/').pop();
function openExample(id){ui.exampleId=id;go('examples');}
function examplesHome(){ui.exampleId='';go('examples');}

function exampleThumb(ex){
  const layout=ex.mode==='collect'?'flow':ex.template?.layout||'table',cells={table:5,cards:3,flow:4}[layout]||4;
  return `<span class="example-thumb thumb-${layout}" aria-hidden="true">${'<i></i>'.repeat(cells)}</span>`;
}
function exampleCard(ex){
  const facts=ex.mode==='static'?[t(ex.template?.layout==='cards'?'卡片布局':'表格布局'),t('{n} 条记录',{n:exampleRows(ex).length})]:[t('{n} 个文件',{n:ex.files.length})];
  return `<button class="example-card" onclick="openExample('${ex.id}')">${exampleThumb(ex)}<span class="example-card-body"><small>${modeTitle(ex)}</small><strong>${esc(t(ex.title))}</strong><span>${esc(t(ex.summary))}</span><span class="example-card-foot"><span class="muted">${facts.map(esc).join(' · ')}</span><span class="example-open">${t('预览')} ${I('arrow')}</span></span></span></button>`;
}
function examplesStrip(){
  const list=exampleList();
  if(!list.length)return '';
  return `<section class="examples-strip"><div class="section-head"><div><h2>${t('看看示例')}</h2><p>${t('预览生成的网站和收集流程，不会改动工作空间。')}</p></div><button class="text-button" onclick="examplesHome()">${t('全部示例')} ${I('chevron')}</button></div><div class="example-grid">${list.map(exampleCard).join('')}</div></section>`;
}
const exampleNote=(extra='')=>`<div class="mode-note">${I('alert')}<span>${t('示例数据来自 DBA 设计稿，不是真实论文、测序结果或运行记录。')}${extra?' '+extra:''}</span></div>`;

function examplesPage(){
  const list=exampleList();
  if(!exampleCatalog)return head(t('示例'))+(exampleError?empty(t('示例加载失败：{e}',{e:exampleError}),btn(t('重试'),'retryExamples()','small','sync')):empty(t('正在加载示例…')));
  const ex=exampleById(ui.exampleId);
  if(ex)return exampleDetail(ex);
  const section=(mode,title)=>{const items=list.filter(ex=>ex.mode===mode);return items.length?`<div class="section-head"><h2>${t(title)}</h2></div><div class="example-grid">${items.map(exampleCard).join('')}</div>`:'';};
  return head(t('示例'),t('安装包内置的示例。点开即可预览生成的网站和收集流程，也可下载数据文件，或一键创建示例项目。'))+exampleNote()+section('static','已有数据 · 静态网站')+section('collect','收集数据 · 分析准备');
}

function exampleAction(ex){
  const p=exampleProject(ex);
  return p?btn(t('打开示例项目'),`selectProject('${p.id}')`,'primary','arrow'):btn(t('创建示例项目'),`confirmExampleProject('${ex.id}')`,'primary','plus');
}
function exampleDetail(ex){
  const back=`<button class="text-button example-back" onclick="examplesHome()">${I('arrow')}${t('全部示例')}</button>`;
  const actions=(ex.mode==='static'?btn(t('下载网站 HTML'),`downloadExampleSite('${ex.id}')`,'','down'):'')+exampleAction(ex);
  return back+head(t(ex.title),t(ex.summary),actions)+exampleNote(ex.mode==='collect'?t('检索会访问真实来源；处理任务需要你自己的 SSH 服务器和锁定的流程。示例产物只展示结果格式。'):'')+(ex.mode==='static'?exampleWebsite(ex):exampleCollect(ex));
}
function exampleRows(ex){
  const f=ex.files.find(f=>f.path===ex.siteData);
  return f?f.text.trim().split(/\r?\n/).slice(1):[];
}
function exampleWebsite(ex){
  const tpl=ex.template||{},facts=[[t('模板'),`${t(tpl.label||'')} · ${ex.skill}`],[t('布局'),t(tpl.layout==='cards'?'卡片布局':'表格布局')],[t('必需字段'),(tpl.requiredColumns||[]).join(', ')||'—'],[t('记录'),t('{n} 条记录',{n:exampleRows(ex).length})]];
  return `<div class="example-facts">${facts.map(([k,v])=>`<div><small>${esc(k)}</small><strong>${esc(v)}</strong></div>`).join('')}</div>
    <section class="card"><div class="card-head"><div><h2>${t('网站预览')}</h2><small>${t('与生成的网站相同，可以在其中搜索。')}</small></div></div><iframe class="site-preview" title="${t('网站预览')}" sandbox="allow-scripts" src="${exampleSiteURL(ex.id)}"></iframe></section>
    ${exampleGroups(ex,false)}
    <section class="card"><div class="card-head"><h2>${t('自己动手')}</h2></div><ol class="example-howto">${[t('所有项目 → 新建项目 → 已有数据 · 静态网站'),t('在“静态网站”页选择模板“{t}”',{t:t(tpl.label||ex.skill)}),t('导入上面的 {f}，扩展名会选中对应格式',{f:fileName(ex.siteData)}),t('生成静态网站，预览后下载 HTML')].map(s=>`<li>${esc(s)}</li>`).join('')}</ol></section>`;
}
function exampleCollect(ex){
  const p=ex.project,topic=[['研究目标',p.description],['研究对象',p.organism],['数据类型',p.assay],['纳入标准',p.criteria],['来源',p.source]].filter(([,v])=>v);
  return `${exampleStep(1,t('研究主题与检索式'),t('创建示例项目后，这些内容填在“主题与检索”页。'))}
    <section class="card"><div class="content-pad"><dl class="example-topic">${topic.map(([k,v])=>`<div><dt>${t(k)}</dt><dd>${esc(v)}</dd></div>`).join('')}</dl><h4>${t('检索表达式')}</h4>${code(p.query)}</div></section>
    ${exampleGroups(ex,true)}`;
}
const exampleStep=(n,title,note)=>`<div class="example-step"><span class="step-num">${n}</span><div><h2>${esc(title)}</h2><p>${esc(note)}</p></div></div>`;
function exampleGroups(ex,numbered){
  return ex.groups.map((g,i)=>{
    const files=ex.files.map((f,index)=>({...f,index})).filter(f=>f.group===g.id);
    return (numbered?exampleStep(i+2,t(g.title),t(g.note)):`<div class="section-head"><div><h2>${t(g.title)}</h2><p>${t(g.note)}</p></div></div>`)+`<div class="example-files">${files.map(f=>exampleFile(ex,f)).join('')}</div>`;
  }).join('');
}
function exampleFile(ex,f){
  const preview=f.path===ex.candidates?exampleCandidates(f.text):f.path===ex.siteData?`<details><summary>${t('查看原始数据')}</summary>${exampleTable(f.text)}</details>`:f.path.endsWith('.tsv')?exampleTable(f.text):code(prettyJSON(f));
  return `<section class="card example-file"><div class="card-head"><div><h2>${esc(t(f.title))}</h2><small class="mono">${esc(fileName(f.path))}</small></div>${btn(t('下载'),`downloadExampleFile('${ex.id}',${f.index})`,'small','down')}</div>${f.note?`<p class="example-file-note muted">${esc(t(f.note))}</p>`:''}${preview}</section>`;
}
function prettyJSON(f){if(!f.path.endsWith('.json'))return f.text;try{return JSON.stringify(JSON.parse(f.text),null,2);}catch{return f.text;}}
function exampleTable(text){
  const [cols=[],...rows]=text.trim().split(/\r?\n/).map(line=>line.split('\t'));
  return table(cols.map(esc),rows.map(r=>`<tr>${r.map(c=>`<td>${esc(c)}</td>`).join('')}</tr>`),'');
}
function exampleCandidates(text){
  let rows=[];try{rows=JSON.parse(text);}catch{}
  return `<div class="example-candidates">${rows.map(c=>`<article><div class="paper-top">${statusBadge('pending')}<span class="mono">${esc(c.accession)}</span></div><h3>${esc(c.title)}</h3><small>${esc([c.organism,c.source].filter(Boolean).join(' · '))}</small><p>${esc(c.abstract)}</p></article>`).join('')}</div>`;
}

function confirmExampleProject(id){
  const ex=exampleById(id);if(!ex)return;
  modal(t('创建示例项目'),`<p>${t('将在工作空间新建项目“{n}”，并载入示例数据。',{n:esc(ex.project.name)})}</p><p class="muted">${t(ex.mode==='static'?'网站会直接生成；之后可在静态网站页修改数据并重新生成。':'候选记录导入后为待复核，可在“数据复核”页逐条判断。')}</p><p class="muted">${t('当前版本不能删除项目。之后再点这个示例，会打开同一个项目。')}</p>`,btn(t('创建并打开'),`createExampleProject('${ex.id}')`,'primary','plus'));
}
function createExampleProject(id){
  run(async()=>{const r=(await act('createExampleProject',{exampleId:id,lang:exampleDataLang()},'')).result;closeModal();toast(t(r.created?'示例项目已创建':'已打开现有的示例项目'));selectProject(r.projectId);});
}
function saveBlob(name,blob){
  const url=URL.createObjectURL(blob),link=document.createElement('a');
  link.href=url;link.download=name;document.body.appendChild(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),10000);
}
function downloadExampleFile(id,index){
  const f=exampleById(id)?.files[index];
  if(f)saveBlob(fileName(f.path),new Blob([f.text],{type:'text/plain;charset=utf-8'}));
}
function downloadExampleSite(id){
  run(async()=>{const res=await fetch(exampleSiteURL(id)+'&download=1');if(!res.ok)throw new Error(t('下载失败'));saveBlob('index.html',await res.blob());});
}
