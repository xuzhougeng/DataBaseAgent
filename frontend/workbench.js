'use strict';

const paths={database:'M4 6c0-4 16-4 16 0s-16 4-16 0Zm0 0v6c0 4 16 4 16 0V6M4 12v6c0 4 16 4 16 0v-6',grid:'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',home:'M3 10 12 3l9 7M5 9v12h14V9M9 21v-8h6v8',search:'M20 20l-5-5M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0',check:'m5 12 4 4L19 6',review:'M8 3H5v18h14V3h-3M8 2h8v4H8zM8 12l2 2 5-5M8 18h7',flow:'M4 3h6v5H4zM14 16h6v5h-6zM7 8v10h7M10 5h7v11',play:'m8 5 11 7-11 7z',server:'M3 3h18v7H3zM3 14h18v7H3zM6 6h1M6 17h1M11 6h7M11 17h7',folder:'M3 6V4h6l2 3h10v13H3z',agent:'M8 3h8M12 3v3M5 6h14v14H5zM2 10v6M22 10v6M9 11h.1M15 11h.1M9 16h6',settings:'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M5 19l2-2M17 7l2-2',plus:'M12 5v14M5 12h14',arrow:'M5 12h14m-5-5 5 5-5 5',chevron:'m9 5 7 7-7 7',clock:'M12 8v5l3 2M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0',alert:'m12 3 10 18H2zM12 9v5M12 18h.1',down:'M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5',sync:'M3 11a9 9 0 0 1 15-7l3 3M21 3v4h-4M21 13A9 9 0 0 1 6 20l-3-3M3 21v-4h4',close:'M6 6l12 12M18 6 6 18',terminal:'m5 6 5 5-5 5M12 17h7',file:'M5 2h9l5 5v15H5zM14 2v6h5M8 12h8M8 16h6',pause:'M8 5v14M16 5v14',lock:'M5 10h14v11H5zM8 10V6a4 4 0 0 1 8 0v4',edit:'m15 3 6 6-12 12H3v-6zM12 6l6 6',activity:'M2 12h5l3-8 4 16 3-8h5'};
const I=n=>`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[n]||paths.file}"/></svg>`;
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

let db={projects:[],servers:[],jobs:[],artifacts:[],events:[],settings:{}};
let meta={},lastState='',loading=false;
const ui={page:'projects',projectId:'',filter:'pending',candidateId:'',jobFilter:'all',jobQuery:'',busy:new Set()};
const labels={queued:'排队中',starting:'提交中',running:'运行中',retry_wait:'等待重试',syncing:'结果同步',succeeded:'已完成',needs_help:'需要协助',pending:'待确认',accepted:'已纳入',excluded:'已排除',include:'建议纳入',exclude:'建议排除',uncertain:'需要确认',online:'连接正常',unchecked:'待检查',error:'连接异常'};
const stageNames=['待定义主题','待检索','待复核','待定义流程','处理中','已有产物'];

const current=()=>db.projects.find(p=>p.id===ui.projectId);
const projectMeta=p=>({projectId:p.id,name:p.name,description:p.description,organism:p.organism,assay:p.assay,criteria:p.criteria,query:p.query,source:p.source});
const projectJobs=(pid=ui.projectId)=>db.jobs.filter(j=>j.projectId===pid);
const projectArtifacts=(pid=ui.projectId)=>db.artifacts.filter(a=>a.projectId===pid);
const pending=p=>(p?.candidates||[]).filter(c=>c.decision==='pending');
const helpJobs=()=>db.jobs.filter(j=>j.status==='needs_help');
const active=j=>['starting','running','syncing'].includes(j.status);
const text=key=>labels[key]||key;
const badge=(s,c='')=>`<span class="badge ${c}">${esc(s)}</span>`;
const statusBadge=s=>badge(text(s),['needs_help','uncertain','error'].includes(s)?'orange':['running','starting','syncing'].includes(s)?'blue':['excluded','unchecked','queued','pending','retry_wait'].includes(s)?'gray':'');
const btn=(s,act,cls='',icon='',disabled=false)=>`<button class="button ${cls}" onclick="${act}" ${disabled?'disabled':''}>${icon?I(icon):''}${esc(s)}</button>`;
const head=(t,sub='',actions='')=>`<div class="page-head"><div><h1>${esc(t)}</h1>${sub?`<p>${esc(sub)}</p>`:''}</div><div class="actions">${actions}</div></div>`;
const field=(label,id,value='',type='input',extra='')=>`<div class="field"><label for="${id}">${esc(label)}</label>${type==='textarea'?`<textarea id="${id}" ${extra}>${esc(value)}</textarea>`:`<input id="${id}" value="${esc(value)}" ${extra}>`}</div>`;
const val=id=>document.getElementById(id)?.value.trim()||'';
const date=(stamp,fallback='—')=>stamp?new Date(stamp).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}):fallback;
const bytes=n=>n<1024?n+' B':n<1048576?(n/1024).toFixed(1)+' KB':n<1073741824?(n/1048576).toFixed(1)+' MB':(n/1073741824).toFixed(2)+' GB';
const code=v=>`<pre class="code">${esc(typeof v==='string'?v:JSON.stringify(v,null,2))}</pre>`;
const empty=(message,button='')=>`<section class="card empty">${I('folder')}<p>${esc(message)}</p>${button}</section>`;
const table=(cols,rows,emptyText)=>`<div class="table-wrap"><table><thead><tr>${cols.map(c=>`<th>${c}</th>`).join('')}</thead><tbody>${rows.join('')||`<tr><td colspan="${cols.length}" class="empty">${esc(emptyText)}</td></tr>`}</tbody></table></div>`;

async function request(path,body){
  const res=await fetch('/api/'+path,body===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  const data=await res.json();
  if(!res.ok)throw new Error(data.error||`请求失败 ${res.status}`);
  return data;
}
async function refresh(force=false){
  if(loading)return;
  loading=true;
  try{
    const reply=await request('state'),signature=JSON.stringify(reply);
    db=reply.state;meta=reply;
    if(!current()&&db.projects.length)ui.projectId=db.projects[0].id;
    const editing=document.querySelector('#modal-root .modal')||document.activeElement?.matches('input,textarea,select');
    if(force||signature!==lastState&&!editing){lastState=signature;render();}
  }finally{loading=false;}
}
async function act(action,body={},message='已保存'){
  if(ui.busy.has(action))return;
  ui.busy.add(action);document.body.classList.add('working');
  try{const result=await request(action,body);await refresh(true);if(message)toast(message);return result;}
  finally{ui.busy.delete(action);if(!ui.busy.size)document.body.classList.remove('working');if(action==='search'||action==='reviewAgent')render();}
}
function run(fn){Promise.resolve().then(fn).catch(e=>toast(e.message||String(e)));}
function go(page){ui.page=page;render();document.querySelector('main')?.scrollTo(0,0);}
function selectProject(id){ui.projectId=id;ui.candidateId='';ui.page='overview';render();}
function stage(p){if(projectJobs(p.id).some(j=>j.status==='succeeded'))return 5;if(projectJobs(p.id).length||p.draft.locked)return 4;if(p.candidates.some(c=>c.decision==='accepted'))return 3;if(p.candidates.length)return 2;if(p.criteria&&p.query)return 1;return 0;}

function render(){
  const p=current();
  if(!p&&!['projects','servers','agent'].includes(ui.page))ui.page='projects';
  const pages={projects:allProjects,overview,search:searchPage,review:reviewPage,workflow:workflowPage,jobs:jobsPage,servers:serversPage,results:resultsPage,agent:agentPage};
  document.getElementById('app').innerHTML=`<div class="app">${sidebar(p)}<main>${(pages[ui.page]||allProjects)()}</main>${aside(p)}</div>`;
}

function sidebar(p){
  const jobs=projectJobs(),helps=helpJobs(),needHelp=jobs.filter(j=>j.status==='needs_help').length;
  const item=(key,icon,name,count='',cls='',disabled=false)=>`<button class="nav ${ui.page===key?'active':''}" onclick="go('${key}')" ${disabled?'disabled':''}>${I(icon)}<span>${name}</span>${count?`<span class="count ${cls}">${count}</span>`:''}</button>`;
  return `<nav class="sidebar" aria-label="主导航"><div class="brand">${I('database')}DBA</div>
    ${item('projects','grid','所有项目',db.projects.length)}
    <div class="label">当前项目</div>
    <select class="project-selector" aria-label="切换项目" onchange="selectProject(this.value)" ${!p?'disabled':''}>${p?db.projects.map(v=>`<option value="${v.id}" ${v.id===ui.projectId?'selected':''}>${esc(v.name)}</option>`).join(''):'<option>尚无项目</option>'}</select>
    ${item('overview','home','项目概览','','',!p)}
    ${item('search','search','主题与检索','','',!p)}
    ${item('review','review','数据复核',p?pending(p).length:'','',!p)}
    ${item('workflow','flow','处理流程','','',!p)}
    ${item('jobs','activity','运行任务',needHelp||jobs.filter(active).length||'',needHelp?'warn':'',!p)}
    ${item('results','folder','数据与产物',projectArtifacts().length||'','',!p)}
    <div class="label">工作空间</div>
    ${item('servers','server','服务器资源',db.servers.length||'')}
    ${item('agent','agent','Agent 中心',helps.length||'',helps.length?'warn':'')}
    <div class="sidebar-bottom"><button class="nav" onclick="settings()">${I('settings')}<span>偏好设置</span></button><div class="version">v0.1.0 · 本地工作空间</div></div></nav>`;
}

function aside(p){
  const events=db.events.filter(e=>!e.projectId||e.projectId===ui.projectId).slice(-6).reverse(),helps=helpJobs(),msgs=(p?.messages||[]).slice(-4),paused=db.settings.paused,ready=p&&meta.modelReady;
  return `<aside class="aside"><div class="aside-head">${I('agent')}<div><strong>DBA Agent</strong><small>${paused?'巡检已暂停，远端进程继续运行':`每 ${db.settings.intervalSeconds} 秒巡检 · ${db.jobs.filter(active).length} 个活跃任务`}</small></div><button class="icon-button" onclick="tick()" aria-label="立即巡检" title="立即巡检">${I('sync')}</button><button class="icon-button" onclick="toggleAgent()" aria-label="${paused?'恢复巡检':'暂停巡检'}" title="${paused?'恢复巡检':'暂停巡检'}">${I(paused?'play':'pause')}</button></div>
    ${helps.length?`<div>${helps.map(j=>`<div class="help-card"><strong>${I('alert')}${esc(j.accession)} 需要你的判断</strong><p>${esc(j.help||j.error)}</p>${btn('查看并处理',`jobDetail('${j.id}')`,'small')}</div>`).join('')}</div>`:''}
    <div><div class="aside-label"><span>最近动态</span><span>上次巡检 ${date(db.lastTick,'尚未巡检')}</span></div>${events.length?events.map(e=>`<div class="event ${e.kind==='help'?'warn':e.kind==='error'?'err':''}"><time>${date(e.at)}</time><strong>${esc(e.title)}</strong>${e.detail?`<p>${esc(e.detail)}</p>`:''}${e.jobId?`<button class="text-button" onclick="jobDetail('${e.jobId}')">查看任务</button>`:''}</div>`).join(''):'<p class="muted">项目动态会显示在这里。</p>'}</div>
    <div class="chat">${msgs.map(m=>`<div class="msg ${m.role==='user'?'me':''}">${esc(m.content)}</div>`).join('')}<div class="chat-box"><textarea id="agent-message" aria-label="向 Agent 提问" placeholder="${ready?'就当前项目向 Agent 提问，Enter 发送':p?'先在偏好设置中配置模型':'先选择一个项目'}" ${ready?'':'disabled'} onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();sendAgent();}"></textarea>${btn('发送','sendAgent()','primary small','',!ready)}</div></div></aside>`;
}

function allProjects(){
  return head('所有项目',`${db.projects.length} 个项目 · ${db.servers.length} 台服务器 · ${db.jobs.filter(active).length} 个任务运行中`,btn('新建项目','newProject()','primary','plus'))+`<div class="grid-projects">${db.projects.map(p=>{const s=stage(p),jobs=projectJobs(p.id),attention=pending(p).length||jobs.some(j=>j.status==='needs_help');return `<button class="project-card" onclick="selectProject('${p.id}')"><div class="project-top"><h2>${esc(p.name)}</h2>${badge(stageNames[s],attention?'orange':s?'':'gray')}</div><p>${esc(p.description||'尚未填写研究目标')}</p><div class="project-meta">${p.candidates.length} 条候选 · ${pending(p).length} 条待复核 · ${jobs.filter(active).length} 个任务运行中 · ${projectArtifacts(p.id).length} 份产物</div><div class="progress"><i style="width:${s*20}%"></i></div></button>`;}).join('')}<button class="project-card new" onclick="newProject()">${I('plus')}新建项目</button></div>`;
}

function overview(){
  const p=current(),jobs=projectJobs(),s=stage(p),v=p.draft,accepted=p.candidates.filter(c=>c.decision==='accepted').length;
  const steps=[['确定主题',p.criteria&&p.query?'范围与检索式已保存':'填写研究范围与检索式','search'],['检索数据',p.candidates.length?`${p.candidates.length} 条候选 · ${p.searches.length} 次检索`:'尚未检索','search'],['复核纳入',`${pending(p).length} 待确认 · ${accepted} 已纳入`,'review'],['定义流程',v.locked?`v${v.version} 已锁定`:p.versions.length?'正在编辑新版本':'尚未锁定','workflow'],['下载与处理',jobs.length?`${jobs.filter(active).length} 运行 · ${jobs.filter(j=>j.status==='succeeded').length} 完成 · ${jobs.filter(j=>j.status==='needs_help').length} 求助`:'尚未创建任务','jobs']];
  const next=[['填写研究范围与检索式','search'],['运行检索','search'],['逐条复核候选数据','review'],['定义并锁定处理流程','workflow'],['查看运行任务','jobs'],['查看本地产物','results']][s];
  return head(p.name,p.description||'尚未填写研究目标')+`<section class="card"><div class="stepper">${steps.map(([n,t,key],i)=>`<button class="step ${i<s?'done':i===s?'current':''}" onclick="go('${key}')"><span class="step-num">${i<s?I('check'):i+1}</span><strong>${n}</strong><small>${esc(t)}</small></button>`).join('')}</div><div class="card-foot"><span class="muted">当前阶段：${stageNames[s]}</span>${btn(next[0],`go('${next[1]}')`,'primary small','arrow')}</div></section>`+(jobs.length?`<section class="card"><div class="card-head"><h2>最近任务</h2><button class="text-button" onclick="go('jobs')">全部 ${jobs.length} 个 ${I('chevron')}</button></div>${jobsTable(jobs.slice(-5).reverse())}</section>`:'');
}

function searchPage(){
  const p=current();
  return head('主题与检索','',btn('保存','saveScope()','','check')+btn(ui.busy.has('search')?'正在检索…':'运行检索','runSearch()','primary','search',ui.busy.has('search')))+`<section class="card"><div class="card-head"><h2>研究主题</h2>${badge(p.criteria?'范围已定义':'待定义',p.criteria?'':'gray')}</div><div class="content-pad">${field('项目名称','project-name',p.name)}${field('研究目标','topic',p.description,'textarea')}<div class="form-grid">${field('研究对象','organism',p.organism)}${field('数据类型','assay',p.assay)}</div>${field('纳入标准','criteria',p.criteria,'textarea')}</div></section>
    <section class="card"><div class="card-head"><h2>检索策略</h2><button class="text-button" onclick="importCandidates()">${I('plus')}导入候选 JSON</button></div><div class="content-pad"><div class="form-grid"><div class="field"><label for="source">来源</label><select id="source">${['Europe PMC','PubMed','GEO'].map(x=>`<option ${x===p.source?'selected':''}>${x}</option>`).join('')}</select></div>${field('本次获取上限（1–1000）','search-limit',100,'input','type="number" min="1" max="1000"')}</div>${field('检索表达式','query',p.query,'textarea','class="mono"')}<p class="muted">检索偏宽以保证召回；命中总数与实际获取数分别记录，重复记录自动合并。</p></div></section>
    ${p.searches.length?`<section class="card"><div class="card-head"><h2>检索历史</h2><small>最近 ${Math.min(10,p.searches.length)} 次</small></div>${table(['时间','来源','检索表达式','命中','获取','新增'],p.searches.slice(-10).reverse().map(r=>`<tr><td>${date(r.at)}</td><td>${esc(r.source)}</td><td class="mono wrap">${esc(r.query)}</td><td>${r.total}</td><td>${r.fetched}</td><td>${r.added}</td></tr>`),'')}</section>`:''}`;
}

function reviewPage(){
  const p=current(),filtered=p.candidates.filter(c=>ui.filter==='all'||c.decision===ui.filter);
  let c=filtered.find(v=>v.id===ui.candidateId)||filtered[0];if(c)ui.candidateId=c.id;
  const tabs=`<div class="tabs">${[['pending','待确认'],['accepted','已纳入'],['excluded','已排除'],['all','全部']].map(([key,n])=>`<button class="tab ${ui.filter===key?'active':''}" onclick="ui.filter='${key}';render()">${n} ${key==='all'?p.candidates.length:p.candidates.filter(c=>c.decision===key).length}</button>`).join('')}</div>`;
  return head('数据复核','Agent 给出建议与原文依据，是否纳入由你确认并记录理由。',btn(ui.busy.has('reviewAgent')?'正在复核…':'Agent 复核待确认项','reviewAgent()','primary','agent',ui.busy.has('reviewAgent')||!pending(p).length))+`<div class="toolbar">${tabs}</div>`+(!c?empty(p.candidates.length?'这一分类暂无记录。':'还没有候选数据，先运行检索或导入。',btn('主题与检索',"go('search')",'small')):`<div class="review-layout"><div class="candidate-list">${filtered.map(a=>`<button class="paper ${a.id===c.id?'selected':''}" onclick="ui.candidateId='${a.id}';render()"><div class="paper-top">${statusBadge(a.decision==='pending'?a.suggestion||'pending':a.decision)}<span>${esc(a.accession||a.source)}</span></div><h3>${esc(a.title)}</h3><small>${esc([a.organism,a.year,a.source].filter(Boolean).join(' · '))}</small></button>`).join('')}</div>
    <section class="card"><div class="card-head"><h2>${esc(c.accession||c.id)}</h2>${statusBadge(c.decision==='pending'?c.suggestion||'pending':c.decision)}</div><div class="content-pad"><div class="detail-title">${esc(c.title)}</div><div class="tags"><span class="tag">${esc(c.source)}</span>${c.organism?`<span class="tag">${esc(c.organism)}</span>`:''}${c.doi?`<span class="tag">DOI ${esc(c.doi)}</span>`:''}</div>
      <h4>Agent 判断${c.confidence?' · 置信度 '+esc(c.confidence):''}</h4><p class="muted">${esc(c.reason||'尚未复核。配置模型后可生成建议，也可直接人工核对。')}</p>${c.evidence?`<div class="evidence">${esc(c.evidence)}</div>`:''}
      <details><summary>来源摘要与链接</summary><p class="abstract">${esc(c.abstract||'来源未提供摘要。')}</p><p class="muted">${c.url?`<a href="${esc(c.url)}" target="_blank" rel="noreferrer">${esc(c.url)}</a>`:'未提供来源链接'}</p></details></div>
      <div class="content-pad">${field('数据来源编号（GSE / SRP / 自定义）','review-accession',c.accession)}${field('直接下载地址（可选）','review-url',c.downloadUrl)}${field('样本分组（可选，每行：样本编号, 分组）','review-groups',c.groups,'textarea','class="mono"')}${field('人工判断与理由（必填）','review-note',c.reviewNote,'textarea')}<div class="decision">${btn('确认纳入',"decide('accepted')",'primary','check')}${btn('排除',"decide('excluded')",'','close')}${btn('稍后再看',"decide('pending')",'','clock')}</div>${c.reviewedAt?`<p class="muted" style="margin-top:10px">上次复核 ${date(c.reviewedAt)}</p>`:''}</div></section></div>`);
}

function workflowPage(){
  const p=current(),v=p.draft,locked=v.locked,dis=locked?'disabled':'';
  const actions=locked?btn('编辑新版本','unlockFlow()','','edit'):btn('保存草稿','saveFlow()','','check')+btn('保存并锁定','lockFlow()','primary','lock');
  return head('处理流程',locked?`v${v.version} 已锁定。任务绑定此版本的完整快照；编辑会生成新草稿，已有任务继续使用原版本。`:'步骤在所选 Linux 服务器上顺序执行，错误即停止。锁定版本后才能创建任务。',actions)+`<div class="two-col"><div>${v.steps.map((s,i)=>`<div class="flow-node"><span class="flow-number">${i+1}</span><div class="row-main"><strong>${esc(s.name)}</strong><small>${esc(s.tool)}${s.command?'':' · 待配置命令'}</small></div><button class="icon-button" onclick="editStep(${i})" aria-label="${locked?'查看':'编辑'}${esc(s.name)}">${I(locked?'chevron':'edit')}</button></div>`).join('')}${locked?'':btn('添加步骤','addStep()','small','plus')}
    <details class="card" style="margin-top:18px"><summary>命令可用的环境变量</summary>${code('DBA_ACCESSION       来源编号\nDBA_SOURCE_URL      数据集下载地址\nDBA_INPUT           保留在远端的输入目录\nDBA_OUTPUT          本次运行的输出目录（步骤在此目录内运行）\nDBA_SAMPLE_GROUPS   人工提供的样本分组\nDBA_CPUS            本任务申请的 CPU 数（线程数需在命令中使用）')}</details></div>
    <section class="card"><div class="card-head"><h2>运行配置</h2>${p.versions.length?`<small>${p.versions.length} 个已锁定版本</small>`:''}</div><div class="content-pad">${field('流程名称','flow-name',v.name,'input',dis)}<div class="field"><label for="flow-server">执行服务器</label><select id="flow-server" ${dis}><option value="">选择 SSH 服务器</option>${db.servers.map(s=>`<option value="${s.id}" ${s.id===v.serverId?'selected':''}>${esc(s.name)}</option>`).join('')}</select></div><div class="form-grid">${field('每任务 CPU 数','flow-cpus',v.cpus,'input',`type="number" min="1" ${dis}`)}${field('项目并发上限','flow-concurrency',v.concurrency,'input',`type="number" min="1" ${dis}`)}</div>${field('同步回本地的产物（每行一个相对路径）','flow-outputs',v.outputs.join('\n'),'textarea',`class="mono" ${dis}`)}</div><div class="card-foot"><span class="muted">只提交已人工纳入、且未运行此版本的数据</span>${btn('创建处理任务','launchJobs()','primary small','play',!locked)}</div></section></div>`;
}

const jobMatch=j=>(ui.jobFilter==='all'||(ui.jobFilter==='running'?active(j):ui.jobFilter==='waiting'?['queued','retry_wait'].includes(j.status):j.status===ui.jobFilter))&&j.accession.toLowerCase().includes(ui.jobQuery.toLowerCase());
function jobsTable(jobs){return table(['数据编号','状态','流程','服务器','更新',''],jobs.map(j=>`<tr><td><strong class="mono">${esc(j.accession)}</strong></td><td>${statusBadge(j.status)}</td><td>v${j.pipeline.version} · 第 ${j.attempt} 次</td><td>${esc(j.server.name)}</td><td>${date(j.updatedAt)}</td><td><button class="icon-button" onclick="jobDetail('${j.id}')" aria-label="查看 ${esc(j.accession)}">${I('chevron')}</button></td></tr>`),'暂无任务');}
function jobsPage(){
  const jobs=projectJobs(),n=f=>jobs.filter(f).length;
  return head('运行任务',`${n(active)} 运行中 · ${n(j=>['queued','retry_wait'].includes(j.status))} 排队或等待重试 · ${n(j=>j.status==='needs_help')} 需要协助 · ${n(j=>j.status==='succeeded')} 已完成`,btn('立即巡检','tick()','','sync'))+`<div class="toolbar"><div class="tabs">${[['all','全部'],['running','运行中'],['waiting','排队 / 重试'],['needs_help','需要协助'],['succeeded','已完成']].map(([key,t])=>`<button class="tab ${ui.jobFilter===key?'active':''}" onclick="ui.jobFilter='${key}';render()">${t}</button>`).join('')}</div><input class="searchbox" aria-label="搜索任务" placeholder="搜索数据编号…" value="${esc(ui.jobQuery)}" oninput="ui.jobQuery=this.value;document.getElementById('jobs-table').innerHTML=jobsTable(projectJobs().filter(jobMatch))"></div><section class="card" id="jobs-table">${jobsTable(jobs.filter(jobMatch))}</section>`;
}

function serversPage(){
  return head('服务器资源','使用本机 SSH 密钥或 SSH Agent 连接 Linux 服务器。请先在终端确认主机指纹并完成免密登录。',btn('添加服务器','addServer()','primary','plus'))+(!db.servers.length?empty('尚未配置远程服务器。'):`<div class="large-grid">${db.servers.map(s=>{const used=db.jobs.filter(j=>j.server.id===s.id&&['starting','running'].includes(j.status)).reduce((n,j)=>n+j.pipeline.cpus,0);return `<section class="server-card"><div class="server-title"><h3>${esc(s.name)}</h3>${statusBadge(s.status)}</div><div class="mono muted">${esc(s.user+'@'+s.host+':'+s.port)}<br>${esc(s.workDir)}</div><div class="meter"><span>已调度 ${used} / ${s.cpuQuota} CPU</span><div class="progress"><i style="width:${Math.min(100,used/s.cpuQuota*100)}%"></i></div><span>最多 ${s.maxJobs} 个任务</span></div><div class="muted">上次检查 ${date(s.checkedAt,'尚未检查')}</div><div class="decision">${btn('连接检查',`testServer('${s.id}')`,'small','sync')}${btn('检查详情',`serverDetail('${s.id}')`,'small','terminal')}</div></section>`;}).join('')}</div>`);
}

function resultsPage(){
  const artifacts=projectArtifacts(),syncing=projectJobs().filter(j=>j.status==='syncing').length;
  return head('数据与产物',`${artifacts.length} 份产物 · ${bytes(artifacts.reduce((n,a)=>n+a.size,0))} · 逐文件 SHA-256 校验后归档${syncing?` · ${syncing} 组正在同步`:''}`,btn('导出产物清单','exportManifest()','','down'))+`<section class="card"><div class="card-head"><h2>本地结果目录</h2><small class="mono">${esc(meta.root+'/projects/'+ui.projectId+'/results/')}</small></div>${table(['产物','数据集','大小','SHA-256','归档时间',''],artifacts.map(a=>`<tr><td><strong>${esc(a.name)}</strong></td><td class="mono">${esc(a.accession)}</td><td>${bytes(a.size)}</td><td class="mono">${esc(a.sha256.slice(0,12))}…</td><td>${date(a.at)}</td><td><button class="text-button" onclick="artifact('${a.id}')">预览</button></td></tr>`),'尚无本地产物。任务完成后，列出的结果文件会同步并归档到这里。')}</section>`;
}

function agentPage(){
  const helps=helpJobs(),events=db.events.slice(-100).reverse(),s=db.settings;
  return head('Agent 中心',s.paused?'自动巡检已暂停，远端进程继续运行。':`每 ${s.intervalSeconds} 秒巡检远端任务，可恢复错误最多重试 ${s.maxRetries} 次；${meta.modelReady?'模型已配置':'模型尚未配置'}。`,btn(s.paused?'恢复巡检':'暂停巡检','toggleAgent()',s.paused?'primary':'',s.paused?'play':'pause')+btn('偏好设置','settings()','','settings'))+`<section class="card"><div class="card-head"><h2>求助请求</h2>${badge(helps.length?`${helps.length} 项待处理`:'无',helps.length?'orange':'gray')}</div>${helps.length?helps.map(j=>`<div class="content-pad"><div class="server-title"><strong class="mono">${esc(j.accession)}</strong><span class="muted">${esc(db.projects.find(p=>p.id===j.projectId)?.name||'')}</span></div><p class="muted">${esc(j.help)}</p>${j.error?`<p class="muted">${esc(j.error)}</p>`:''}${btn('查看证据与处理',`jobDetail('${j.id}')`,'small','arrow')}</div>`).join(''):'<div class="content-pad muted">目前没有需要协助的任务。</div>'}</section>
    <section class="card"><div class="card-head"><h2>操作记录</h2><small>最近 ${events.length} 条</small></div>${table(['时间','动作','依据与结果'],events.map(e=>`<tr><td>${date(e.at)}</td><td>${esc(e.title)}</td><td class="wrap">${esc(e.detail)}${e.jobId?` <button class="text-button" onclick="jobDetail('${e.jobId}')">查看任务</button>`:''}</td></tr>`),'等待第一条记录')}</section>`;
}

function toast(message){document.getElementById('toast-root').innerHTML=`<div class="toast" role="status">${esc(message)}</div>`;clearTimeout(window.toastTimer);window.toastTimer=setTimeout(()=>document.getElementById('toast-root').innerHTML='',6500);}
function modal(title,body,actions=''){document.getElementById('modal-root').innerHTML=`<div class="modal-backdrop" onclick="if(event.target===this)closeModal()"><section class="modal" role="dialog" aria-modal="true" aria-label="${esc(title)}"><div class="modal-head"><h2>${esc(title)}</h2><button class="icon-button" onclick="closeModal()" aria-label="关闭">${I('close')}</button></div><div class="modal-body">${body}</div><div class="modal-actions">${btn('关闭','closeModal()')}${actions}</div></section></div>`;document.querySelector('.modal input,.modal textarea,.modal button')?.focus();}
function closeModal(){document.getElementById('modal-root').innerHTML='';}
document.addEventListener('keydown',e=>{if(e.key==='Escape')closeModal();});

function newProject(){modal('新建数据库项目',field('项目名称','new-name')+field('研究目标','new-desc','','textarea'),btn('创建项目','createProject()','primary','plus'));}
function createProject(){run(async()=>{const name=val('new-name');if(!name)throw new Error('请输入项目名称');await act('createProject',{name,description:val('new-desc')},'项目已创建');ui.projectId=db.projects.at(-1).id;closeModal();go('search');});}
function scopeBody(){const p=current();return {...projectMeta(p),name:val('project-name')||p.name,description:val('topic'),organism:val('organism'),assay:val('assay'),criteria:val('criteria'),query:val('query'),source:val('source')};}
function saveScope(){run(()=>act('saveProject',scopeBody(),'研究范围与检索策略已保存'));}
function runSearch(){run(async()=>{const body=scopeBody(),limit=Number(val('search-limit'));if(!body.query)throw new Error('请填写检索表达式');await act('saveProject',body,'');toast('正在检索，请等待来源服务返回…');const r=(await act('search',{...body,limit},'')).result;modal('检索已完成',code(`来源：${r.source}\n命中总数：${r.total}\n本次获取：${r.fetched}\n合并后新增：${r.added}`)+`<p class="muted">达到获取上限时，请调整上限或补充检索式。结果未经人工复核。</p>`,btn('开始复核',"closeModal();go('review')",'primary','arrow'));});}
function importCandidates(){modal('导入候选数据',`<p class="muted">粘贴 JSON 数组，每条必须有 title。</p>${field('JSON 数据','import-json','[\n  {"title":"研究或数据集标题","source":"GEO","accession":"GSE...","organism":"Homo sapiens","url":"https://...","downloadUrl":"","abstract":"摘要与实验说明"}\n]','textarea','class="mono" style="min-height:220px"')}`,btn('导入并去重','doImport()','primary'));}
function doImport(){run(async()=>{const candidates=JSON.parse(val('import-json'));if(!Array.isArray(candidates))throw new Error('需要 JSON 数组');await act('importCandidates',{projectId:ui.projectId,candidates},'候选数据已导入');closeModal();go('review');});}
function reviewAgent(){run(async()=>{toast('Agent 正在逐条复核，本次最多处理 20 条…');await act('reviewAgent',{projectId:ui.projectId},'Agent 建议已保存，请核对原文后确认');});}
function decide(decision){run(async()=>{await act('review',{projectId:ui.projectId,candidateId:ui.candidateId,decision,note:val('review-note'),groups:val('review-groups'),downloadUrl:val('review-url'),accession:val('review-accession')},'人工判断已记录');});}
function readFlow(){const v=current().draft;return {...v,name:val('flow-name'),serverId:val('flow-server'),cpus:Number(val('flow-cpus')),concurrency:Number(val('flow-concurrency')),outputs:val('flow-outputs').split('\n').map(x=>x.trim()).filter(Boolean)};}
function saveFlow(){run(()=>act('savePipeline',{projectId:ui.projectId,pipeline:readFlow()},'流程草稿已保存'));}
function lockFlow(){run(()=>act('lockPipeline',{projectId:ui.projectId,pipeline:readFlow()},'流程版本已锁定，可创建处理任务'));}
function unlockFlow(){run(()=>act('savePipeline',{projectId:ui.projectId,pipeline:{...current().draft,locked:false}},'已创建可编辑草稿，已有任务保留原版本'));}
function editStep(i){ui.flowDraft=readFlow();const s=ui.flowDraft.steps[i],locked=current().draft.locked,dis=locked?'disabled':'';modal(locked?'流程步骤':'编辑流程步骤',field('步骤名称','step-name',s.name,'input',dis)+field('工具与版本','step-tool',s.tool,'input',dis)+field('Linux shell 命令','step-command',s.command,'textarea',`class="mono" style="min-height:220px" ${dis}`),locked?'':btn('保存步骤',`saveStep(${i})`,'primary')+(ui.flowDraft.steps.length>1?btn('删除步骤',`removeStep(${i})`):''));}
function saveStep(i){run(async()=>{const base=ui.flowDraft||current().draft,v={...base,steps:base.steps.map(s=>({...s}))};v.steps[i]={name:val('step-name'),tool:val('step-tool'),command:val('step-command')};await act('savePipeline',{projectId:ui.projectId,pipeline:v},'步骤已保存');ui.flowDraft=null;closeModal();});}
function removeStep(i){run(async()=>{const base=ui.flowDraft||current().draft,v={...base,steps:base.steps.filter((_,k)=>k!==i)};await act('savePipeline',{projectId:ui.projectId,pipeline:v},'步骤已删除');ui.flowDraft=null;closeModal();});}
function addStep(){run(()=>act('savePipeline',{projectId:ui.projectId,pipeline:{...readFlow(),steps:[...current().draft.steps,{name:'新处理步骤',tool:'用户定义',command:''}]}},'已添加步骤'));}
function launchJobs(){const p=current(),v=p.draft;modal('创建远程处理任务',code(`流程：${v.name} · v${v.version}\n服务器：${db.servers.find(s=>s.id===v.serverId)?.name||''}\n资源：每任务 ${v.cpus} CPU，并发 ${v.concurrency}\n人工纳入：${p.candidates.filter(c=>c.decision==='accepted').length} 条`)+`<p class="muted">只提交已人工纳入、且尚未运行此流程版本的数据。锁定的命令将在所选服务器上真实运行，巡检按配额自动调度。</p>`,btn('确认加入队列','confirmLaunch()','primary','play'));}
function confirmLaunch(){run(async()=>{await act('launch',{projectId:ui.projectId},'任务已加入队列，等待 Agent 调度');closeModal();go('jobs');});}
function jobDetail(jid){const j=db.jobs.find(v=>v.id===jid);if(!j)return;const help=j.status==='needs_help';modal(j.accession+' · 任务详情',`<div class="tags">${statusBadge(j.status)}<span class="tag">${esc(j.server.name)}</span><span class="tag">流程 v${j.pipeline.version}</span><span class="tag">第 ${j.attempt} 次</span></div><p class="muted">${esc(j.remoteDir||'尚未提交')}<br>更新 ${date(j.updatedAt)}</p>${j.error?`<div class="notice error">${I('alert')}${esc(j.error)}</div>`:''}${code(j.log||'等待远端运行日志…')}${help?`<h4>需要你的判断</h4><p class="muted">${esc(j.help)}</p>${field('已完成的处理说明（必填）','resolve-note','','textarea')}${field('补充样本分组（可选，每行：样本编号, 分组）','resolve-groups',j.input.groups,'textarea','class="mono"')}`:''}`,help?btn('已处理，恢复任务',`resolveJob('${j.id}')`,'primary','play'):'');}
function resolveJob(jid){run(async()=>{await act('resolveJob',{jobId:jid,note:val('resolve-note'),groups:val('resolve-groups')},'处理记录已保存，任务将在巡检后继续');closeModal();});}
function addServer(){modal('添加远程服务器',field('服务器名称','server-name')+`<div class="form-grid">${field('主机地址','server-host')}${field('SSH 用户','server-user')}</div><div class="form-grid">${field('SSH 端口','server-port',22,'input','type="number"')}${field('专用工作目录（Linux 绝对路径）','server-dir','/home/research/dba')}</div>${field('本机 SSH 私钥路径（留空使用 SSH Agent / 默认密钥）','server-key')}<div class="form-grid">${field('可供 DBA 调度的 CPU 数','server-cpus',16,'input','type="number" min="1"')}${field('最多并行任务数','server-max',2,'input','type="number" min="1"')}</div><p class="muted">私钥保留在本机。应用不保存密码，不自动接受未知主机指纹。</p>`,btn('保存配置','saveServer()','primary'));}
function saveServer(){run(async()=>{await act('addServer',{server:{name:val('server-name'),host:val('server-host'),user:val('server-user'),port:Number(val('server-port')),workDir:val('server-dir'),keyPath:val('server-key'),cpuQuota:Number(val('server-cpus')),maxJobs:Number(val('server-max'))}},'服务器配置已保存');closeModal();});}
function testServer(sid){run(async()=>{toast('正在检查 SSH 和远程运行环境…');await act('probeServer',{serverId:sid},'服务器检查通过');serverDetail(sid);});}
function serverDetail(sid){const s=db.servers.find(v=>v.id===sid);modal(s.name+' · 检查详情',code(s.details||'尚未检查连接。'));}
function settings(){const s=db.settings;modal('偏好设置',`<h4>Agent 巡检</h4><div class="form-grid">${field('巡检间隔（秒）','setting-interval',s.intervalSeconds,'input','type="number" min="10" max="86400"')}${field('可恢复错误最大重试次数','setting-retries',s.maxRetries,'input','type="number" min="0" max="10"')}</div><h4>模型（Chat Completions 协议）</h4>${field('API 基础地址（以 /v1 等服务前缀结尾）','setting-url',s.modelUrl||'')}${field('模型名称','setting-model',s.model||'')}${field('API Key 的环境变量名','setting-key',s.apiKeyEnv||'DBA_MODEL_API_KEY')}<p class="muted">密钥只从启动应用的环境读取，不写入项目文件。模型用于复核建议与项目对话，请求包含项目说明、候选元信息或任务摘要。</p>`,btn('保存设置','saveSettings()','primary'));}
function saveSettings(){run(async()=>{await act('settings',{settings:{...db.settings,intervalSeconds:Number(val('setting-interval')),maxRetries:Number(val('setting-retries')),modelUrl:val('setting-url'),model:val('setting-model'),apiKeyEnv:val('setting-key')}},'设置已保存');closeModal();});}
function toggleAgent(){run(()=>act('settings',{settings:{...db.settings,paused:!db.settings.paused}},db.settings.paused?'巡检已恢复':'自动巡检已暂停'));}
function tick(){run(()=>act('tick',{},'已发起一次巡检'));}
function sendAgent(){const message=val('agent-message');if(!message)return;run(()=>act('message',{projectId:ui.projectId,message},''));}
function artifact(aid){run(async()=>{const a=db.artifacts.find(x=>x.id===aid),r=await request('artifact?id='+encodeURIComponent(aid));modal(a.name+' · 预览',`<p class="muted mono">${esc(a.path)}<br>SHA-256 ${esc(a.sha256)}</p><p class="muted">最多预览前 64 KiB。</p>${code(r.text)}`);});}
function exportManifest(){run(async()=>{const payload=await request('manifest?projectId='+encodeURIComponent(ui.projectId)),blob=new Blob([JSON.stringify(payload,null,2)],{type:'application/json'}),url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download='dba_manifest.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);toast('产物清单已导出');});}

run(async()=>{await refresh(true);setInterval(()=>{refresh().catch(()=>{});},3000);});
