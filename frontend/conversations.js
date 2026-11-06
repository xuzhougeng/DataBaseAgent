'use strict';

const chatStatusNames={queued:'排队中',running:'正在执行',stopping:'正在停止…',completed:'已完成',failed:'执行失败',cancelled:'已停止',interrupted:'执行中断'};
const toolNames={delegate_agent:'调度子 Agent',list_projects:'查看项目',get_project:'读取项目',create_project:'创建项目',update_project:'更新项目',search:'检索数据',list_searches:'读取检索历史',list_candidates:'查看候选',get_candidate:'读取候选详情',import_candidates:'导入候选',review_candidates:'复核候选',review_candidate:'记录复核结论',get_pipeline:'读取处理流程',save_pipeline:'保存处理流程',lock_pipeline:'锁定流程版本',launch_jobs:'创建处理任务',list_jobs:'查看运行任务',get_job:'读取任务详情',resolve_job:'恢复任务',list_servers:'查看服务器',add_server:'添加服务器',probe_server:'检查服务器连接',list_artifacts:'查看产物',export_manifest:'导出产物清单',recent_events:'读取操作记录',get_settings:'读取设置',update_settings:'更新设置',save_model:'保存模型',delete_model:'删除模型',save_agent:'保存 Agent 配置',reset_agent:'重置 Agent 配置',tick:'巡检任务'};
const scopeKey=(pid=ui.projectId,aid=currentAgent()?.id)=>`${pid||''}|${aid||'assistant'}`;
const scopeHistory=(pid=ui.projectId)=>pid?(db.projects.find(p=>p.id===pid)?.messages||[]):(db.messages||[]);
const scopeConversations=()=>(db.conversations||[]).filter(c=>c.projectId===(ui.projectId||''));
const currentConversation=()=>scopeConversations().find(c=>c.id===ui.conversationSelections[scopeKey()]);
const conversationKey=()=>currentConversation()?.id||`new:${scopeKey()}`;
const chatDraft=()=>ui.chatDrafts[conversationKey()]||'';
const isPendingMessage=m=>m.role==='user'&&['queued','running','stopping'].includes(m.status);
const chatQueue=()=>scopeHistory().filter(isPendingMessage);
const selectedMessages=()=>{const c=currentConversation();return c?scopeHistory().filter(m=>m.conversationId===c.id):[];};
const replyCount=cid=>scopeHistory().filter(m=>m.conversationId===cid&&m.role==='assistant').length;
const messageSize=value=>new TextEncoder().encode(value).length;

function saveChatUI(){
  if(!meta.root)return;
  try{localStorage.setItem('dba-chat:'+meta.root,JSON.stringify({projectId:ui.projectId,drafts:ui.chatDrafts,selections:ui.conversationSelections,readCounts:ui.chatReadCounts,outbox:ui.chatOutbox}));}catch{}
}
function syncChatState(){
  if(ui.chatRoot!==meta.root){
    ui.chatRoot=meta.root;
    try{
      const saved=JSON.parse(localStorage.getItem('dba-chat:'+meta.root)||'{}');
      const strings=value=>Object.fromEntries(Object.entries(value&&typeof value==='object'?value:{}).filter(([,v])=>typeof v==='string'));
      ui.chatDrafts=strings(saved.drafts);ui.conversationSelections=strings(saved.selections);
      ui.chatReadCounts=saved.readCounts&&typeof saved.readCounts==='object'?saved.readCounts:{};
      ui.chatOutbox=saved.outbox&&typeof saved.outbox==='object'?saved.outbox:{};
      if(typeof saved.projectId==='string'&&(!saved.projectId||db.projects.some(p=>p.id===saved.projectId)))ui.projectId=saved.projectId;
    }catch{}
  }
  const key=scopeKey(),selected=ui.conversationSelections[key];
  if(selected===undefined)ui.conversationSelections[key]=scopeConversations().filter(c=>c.agentId===currentAgent()?.id&&!c.archived).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt))[0]?.id||'';
  else if(selected&&!currentConversation()&&!ui.chatSending.size)ui.conversationSelections[key]='';
  for(const [key,payload] of Object.entries(ui.chatOutbox)){
    if(!payload||typeof payload.clientId!=='string'){delete ui.chatOutbox[key];continue;}
    const found=scopeHistory(payload.projectId).find(m=>m.clientId===payload.clientId);
    if(found)acceptChatMessage(key,payload,{conversationId:found.conversationId,messageId:found.id});
  }
}
function setChatDraft(value){ui.chatDrafts[conversationKey()]=value;saveChatUI();updateComposer();}
function newConversation(){closeMentions();ui.conversationSelections[scopeKey()]='';ui.historyArchived=false;saveChatUI();render();document.getElementById('agent-message')?.focus();}
function openConversation(cid){
  closeMentions();
  const c=(db.conversations||[]).find(c=>c.id===cid);if(!c)return;
  ui.projectId=c.projectId;ui.conversationSelections[scopeKey()]=c.id;ui.page='agent';
  ui.chatReadCounts[c.id]=replyCount(c.id);closeModal();
  saveChatUI();render();
}
function openChatHistory(){ui.historyQuery='';modal(t('对话历史'),chatHistoryBody(),btn(t('新建对话'),'closeModal();newConversation()','primary','plus'),'history-modal');document.getElementById('chat-history-search')?.focus();}
function searchChatHistory(value){ui.historyQuery=value;const list=document.querySelector('.history-modal .history-list');if(list)list.innerHTML=chatHistoryList();}
function toggleChatArchive(){ui.historyArchived=!ui.historyArchived;const body=document.querySelector('.history-modal .modal-body');if(body){body.innerHTML=chatHistoryBody();body.querySelector('.history-tabs button.active')?.focus();}}
function chatHistoryList(){
  const query=(ui.historyQuery||'').trim().toLocaleLowerCase();
  const all=scopeConversations(),messages=scopeHistory(),pending=new Set(messages.filter(isPendingMessage).map(m=>m.conversationId));
  const list=all.filter(c=>!!c.archived===!!ui.historyArchived&&(!query||c.title.toLocaleLowerCase().includes(query)||messages.some(m=>m.conversationId===c.id&&m.content.toLocaleLowerCase().includes(query)))).sort((a,b)=>Number(b.pinned)-Number(a.pinned)||b.updatedAt.localeCompare(a.updatedAt)||all.indexOf(b)-all.indexOf(a));
  let group='';
  const rows=list.map(c=>{
    const day=new Date(c.updatedAt),today=new Date(),yesterday=new Date();yesterday.setDate(today.getDate()-1);
    const label=c.pinned?'已置顶':day.toDateString()===today.toDateString()?'今天':day.toDateString()===yesterday.toDateString()?'昨天':'更早';
    const divider=label!==group?`<div class="history-group">${t(label)}</div>`:'';group=label;
    const a=agents().find(a=>a.id===c.agentId),unread=Math.max(0,replyCount(c.id)-(Number(ui.chatReadCounts[c.id])||0));
    return divider+`<button class="history-item ${currentConversation()?.id===c.id?'active':''}" onclick="openConversation('${c.id}')" aria-current="${currentConversation()?.id===c.id?'true':'false'}" title="${esc(c.title)}"><span class="history-item-title">${pending.has(c.id)?'<i class="status-dot"></i>':I(c.pinned?'pin':'chat')}<strong>${esc(c.title)}</strong>${unread&&currentConversation()?.id!==c.id?`<span class="count">${unread}</span>`:''}</span><span class="history-item-meta">${esc(t(a?.name||'Agent'))}<time>${date(c.updatedAt)}</time></span></button>`;
  }).join('');
  return rows||`<div class="history-empty">${I('chat')}<p>${t(query?'没有匹配的对话':ui.historyArchived?'还没有归档对话':'发送第一条消息后，对话会自动保存在这里。')}</p></div>`;
}
function chatHistoryBody(){
  const all=scopeConversations(),n=all.filter(c=>!c.archived).length,archived=all.length-n;
  return `<div class="history-search">${I('search')}<input id="chat-history-search" aria-label="${t('搜索对话')}" placeholder="${t('搜索标题或消息…')}" value="${esc(ui.historyQuery||'')}" oninput="searchChatHistory(this.value)"></div><div class="history-tabs"><button class="${!ui.historyArchived?'active':''}" onclick="if(ui.historyArchived)toggleChatArchive()">${t('最近对话')}${n?` <span>${n}</span>`:''}</button><button class="${ui.historyArchived?'active':''}" onclick="if(!ui.historyArchived)toggleChatArchive()">${t('已归档')}${archived?` <span>${archived}</span>`:''}</button></div><div class="history-list">${chatHistoryList()}</div><p class="history-note">${I('folder')}${esc(current()?.name||t('工作空间对话'))}</p>`;
}

const chatIconButton=(label,action,icon,disabled=false)=>`<button class="icon-button" title="${esc(t(label))}" aria-label="${esc(t(label))}" onclick="${action}" ${disabled?'disabled':''}>${I(icon)}</button>`;
function conversationActions(c){
  if(!c)return '';
  return chatIconButton('重命名对话',`renameConversation('${c.id}')`,'edit')+chatIconButton(c.pinned?'取消置顶':'置顶对话',`updateConversation('${c.id}',{pinned:${!c.pinned}})`,'pin')+chatIconButton('导出对话',`exportConversation('${c.id}')`,'down')+chatIconButton(c.archived?'恢复对话':'归档对话',`updateConversation('${c.id}',{archived:${!c.archived}})`,'archive',!c.archived&&scopeHistory().some(m=>m.conversationId===c.id&&isPendingMessage(m)));
}
function prettyToolValue(value){try{return JSON.stringify(JSON.parse(value),null,2);}catch{return value||'—';}}
function toolStep(item,index,live=false,mid=''){
  const state=live?'running':item.error?'failed':'completed',label=live?'正在执行':item.error?'执行失败':'已完成';
  return `<details class="tool-step ${state}" data-detail-id="tool-${mid}-${index}"><summary><span class="tool-state">${I(live?'sync':item.error?'alert':'check')}</span><span class="tool-name"><strong>${esc(t(toolNames[item.tool]||item.tool))}</strong><code>${esc(item.tool)}</code></span><span class="tool-step-status">${t(label)}${!live?` · ${item.ms<1000?item.ms+' ms':(item.ms/1000).toFixed(1)+' s'}`:''}</span>${I('chevron')}</summary><div class="tool-step-detail"><div class="tool-detail-label">${t('调用参数')}${item.step?`<span>${t('第 {n} 步',{n:item.step})}</span>`:''}</div><pre><code>${esc(prettyToolValue(item.args))}</code></pre>${live?`<p class="tool-awaiting">${t('等待工具返回结果…')}</p>`:`<div class="tool-detail-label">${t(item.error?'错误信息':'返回结果')}</div><pre><code>${esc(prettyToolValue(item.result))}</code></pre>`}${item.truncated?`<p class="muted">${t('内容较长，已展示可保存的部分。')}</p>`:''}</div></details>`;
}
function trace(m,activeRun){
  const key=m.kind==='delegation'?m.id:(m.replyTo||m.id),items=m.trace||[],live=activeRun&&(m.kind==='delegation'?activeRun.delegationId===m.id:activeRun.messageId===key),failed=items.filter(x=>x.error).length;
  if(!items.length&&!live)return '';
  const child=m.kind==='delegation',tool=child||!activeRun?.subagentId?activeRun?.tool:'',worker=agents().find(a=>a.id===activeRun?.subagentId);
  return `<details class="execution-trace" data-detail-id="trace-${key}" ${live?'open':''}><summary><span class="trace-icon ${live?'live':''}">${I(live?'activity':'flow')}</span><strong>${t('执行过程')}</strong><span>${t('已完成 {n} 次调用',{n:items.length})}${failed?' · '+t('{n} 次失败',{n:failed}):''}</span>${I('chevron')}</summary><div class="tool-timeline">${items.map((item,i)=>toolStep(item,i,false,key)).join('')}${live&&tool?toolStep({tool,args:activeRun.toolArgs,step:activeRun.step},items.length,true,key):''}${live&&activeRun.approvalId&&(child||!activeRun.subagentId)?approvalCard(approvalById(activeRun.approvalId)):''}${live&&!tool?`<div class="planning-step"><span class="thinking-dots"><i></i><i></i><i></i></span>${!child&&worker?t('主 Agent 已委派 {a}',{a:esc(t(worker.name))}):t(items.length?'正在整理结果与规划下一步…':'正在理解你的目标…')}<small>${t('第 {n} 步',{n:activeRun.step||1})}</small></div>`:''}</div></details>`;
}
function approvalById(id){return (meta.approvals||[]).find(a=>a.id===id);}
function approvalCard(ap){
  if(!ap)return '';
  const a=agents().find(x=>x.id===ap.agentId);
  return `<div class="approval-card" role="alert"><strong>${I('alert')}${t('{a} 请求确认',{a:esc(t(a?.name||ap.agentId))})}<code>${esc(ap.tool)}</code></strong><p>${esc(t(ap.reason))}</p>${ap.detail?`<pre class="approval-detail">${esc(ap.detail)}</pre>`:''}<details><summary>${t('查看调用参数')}</summary><pre>${esc(ap.args)}</pre></details><div class="decision">${btn(t('允许执行'),`decideApproval('${ap.id}',true)`,'small primary','check',ui.busy.has('decideApproval'))}${btn(t('拒绝'),`decideApproval('${ap.id}',false)`,'small','close',ui.busy.has('decideApproval'))}</div></div>`;
}
function decideApproval(id,approve){run(async()=>{await act('decideApproval',{approvalId:id,approve},approve?'已允许执行':'已拒绝');});}
function chatMessage(msg,run){
  if(msg.kind==='delegation')return delegationMessage(msg,run);
  const user=msg.role==='user',failed=['failed','cancelled','interrupted'].includes(msg.status),a=agents().find(a=>a.id===msg.agent);
  const position=scopeHistory().filter(m=>m.role==='user'&&m.status==='queued').findIndex(m=>m.id===msg.id)+1;
  return `<article class="msg ${user?'me':''} ${!user&&failed?'message-interrupted':''}" data-message-id="${msg.id}"><div class="message-label">${!user?`<span class="message-avatar">${I(a?.icon||'agent')}</span>`:''}<strong>${user?t('你'):esc(t(a?.name||'Agent'))}</strong><time>${date(msg.at)}</time>${chatIconButton('复制消息',`copyMessage('${msg.id}')`,'copy')}</div>${!user?trace(msg):''}<div class="message-body">${user?esc(msg.content):messageMarkdown(msg.content)}</div>${user&&msg.status!=='completed'?`<div class="message-status ${esc(msg.status)}">${I(msg.status==='queued'?'clock':failed?'alert':'activity')}${t(chatStatusNames[msg.status]||msg.status)}${msg.status==='queued'?' · '+t('第 {n} 位',{n:position}):''}</div>`:''}${failed&&(!user||!scopeHistory().some(m=>m.replyTo===msg.id))?`<div class="message-actions">${btn(t('重试这条消息'),`retryChatMessage('${user?msg.id:msg.replyTo}')`,'small','sync',!!currentConversation()?.archived)}${user?'':`<span>${t('重试前请核对已完成的操作。')}</span>`}</div>`:''}</article>`;
}
function delegationMessage(msg,run){
  const a=agents().find(a=>a.id===msg.agent),running=msg.status==='running';
  return `<article class="msg delegation-card ${msg.error?'failed':''}"><details data-detail-id="child-${msg.id}" ${running?'open':''}><summary><span class="delegation-heading">${I(a?.icon||'agent')}<strong>${esc(t(a?.name||msg.agent))}</strong>${badge(t(running?'正在执行':msg.error?'执行中断':'子任务完成'),msg.error?'orange':running?'blue':'')}<time>${date(msg.at)}</time></span><span class="delegation-task">${esc(msg.task)}</span></summary>${trace(msg,run)}${msg.content?`<div class="delegation-result message-body">${messageMarkdown(msg.content)}</div>`:''}</details></article>`;
}
function chatLog(messages){
  const replies=new Map(messages.filter(m=>m.replyTo&&m.kind!=='delegation').map(m=>[m.replyTo,m]));
  const children=new Map();for(const m of messages){if(m.kind==='delegation'&&m.replyTo){if(!children.has(m.replyTo))children.set(m.replyTo,[]);children.get(m.replyTo).push(m);}}
  const roots=messages.filter(m=>!m.replyTo),limit=ui.chatVisible?.[conversationKey()]||30;
  return (roots.length>limit?`<button class="load-earlier" onclick="loadEarlierMessages()">${I('clock')}${t('加载更早的消息')}</button>`:'')+roots.slice(-limit).map(m=>{
    const reply=replies.get(m.id),run=(meta.agentRuns||[]).find(r=>r.messageId===m.id);
    return chatMessage(m,run)+(!reply&&isPendingMessage(m)&&m.status!=='queued'?`<div class="msg agent-executing">${trace(m,run||{messageId:m.id,step:1})}</div>`:'')+(children.get(m.id)||[]).map(child=>chatMessage(child,run)).join('')+(reply?chatMessage(reply):'');
  }).join('');
}
function loadEarlierMessages(){
  const log=document.querySelector('.chat-log'),top=log.scrollTop,height=log.scrollHeight,key=conversationKey();
  ui.chatVisible||={};ui.chatVisible[key]=(ui.chatVisible[key]||30)+30;render();
  const updated=document.querySelector('.chat-log');updated.scrollTop=top+updated.scrollHeight-height;onChatScroll();
}
function messageQueue(){
  const queue=chatQueue(),waiting=queue.filter(m=>m.status==='queued'),running=queue.find(m=>m.status!=='queued'),paused=!!db.chatQueuePaused?.[ui.projectId||''];
  if(!queue.length&&!paused)return '';
  return `<div class="queue-panel ${paused?'paused':''}"><div class="queue-heading"><span>${I(paused?'pause':'clock')}<strong>${t(paused?'队列已暂停':'消息队列')}</strong><small>${t('{n} 条待发送',{n:waiting.length})}</small></span><div class="queue-controls">${paused?btn(t('继续队列'),'setQueuePaused(false)','small','play',running?.status==='stopping'):waiting.length?chatIconButton('暂停队列','setQueuePaused(true)','pause'):''}${running?btn(t(running.status==='stopping'?'正在停止…':'停止执行'),`cancelChatMessage('${running.id}')`,'small stop-button','stop',running.status==='stopping'):''}</div></div>${running?`<div class="queue-running"><i class="status-dot"></i><span>${t('正在执行')} · ${esc(running.content)}</span><button class="text-button" onclick="openConversation('${running.conversationId}')">${t('查看')}</button></div>`:''}${waiting.length?`<details class="queue-details" data-detail-id="queue-${ui.projectId||'workspace'}" open><summary>${t('按顺序处理，可编辑或移除排队消息')}${I('chevron')}</summary><ol>${waiting.map((m,i)=>`<li><span class="queue-position">${i+1}</span><button class="queue-message" onclick="openConversation('${m.conversationId}')"><strong>${esc(m.content)}</strong><small>${esc((db.conversations||[]).find(c=>c.id===m.conversationId)?.title||'')} · ${esc(t(agents().find(a=>a.id===m.agent)?.name||'Agent'))}</small></button>${chatIconButton('编辑排队消息',`editQueuedMessage('${m.id}')`,'edit')}${chatIconButton('移除排队消息',`cancelChatMessage('${m.id}')`,'close')}</li>`).join('')}</ol></details>`:''}${paused?`<p class="queue-note">${t('后续消息会保留，继续队列后按顺序执行。')}</p>`:''}</div>`;
}
function agentPage(){
  const p=current(),a=currentAgent(),m=a&&agentModel(a),c=currentConversation(),ready=!!meta.ready?.[a?.id],messages=selectedMessages(),draft=chatDraft(),key=conversationKey(),sending=ui.chatSending.has(key),outbox=ui.chatOutbox[key];
  const queue=chatQueue(),paused=!!db.chatQueuePaused?.[ui.projectId||''],blocked=!ready||!!c?.archived||sending,tooLong=messageSize(draft)>8000;
  return head(t('AI 助手'),t('围绕目标展开对话，随时查看执行过程。'),btn(t('对话历史'),'openChatHistory()','small','chat')+btn(t('新建对话'),'newConversation()','small primary','plus'))+`
    <div class="assistant-layout"><section class="assistant-workspace" aria-label="${t('AI 对话')}">
      <div class="assistant-toolbar"><div class="conversation-heading"><div><h2>${esc(c?.title||t('新对话'))}</h2><span>${I('folder')}${esc(p?.name||t('工作空间对话'))}</span></div><div class="conversation-actions">${conversationActions(c)}</div></div><div class="assistant-context"><span>${I('agent')}${t('主 Agent 统筹 · 子 Agent 按需协作')}</span>${badge(t(ready?'可用':'待配置模型'),ready?'':'orange')}</div></div>
      ${ui.connectionError?`<div class="chat-connection" role="status">${I('alert')}${t('连接中断，草稿已保留。恢复连接后会自动同步。')}</div>`:''}${meta.chatErrors?.[ui.projectId||'']?`<div class="chat-connection" role="alert">${esc(meta.chatErrors[ui.projectId||''])}</div>`:''}
      <div class="chat-log" role="log" aria-label="${t('对话记录')}" aria-live="off" onscroll="onChatScroll()">${messages.length?chatLog(messages):`<div class="assistant-welcome"><span class="welcome-icon">${I(a?.icon||'agent')}</span><h2>${t('从一个目标开始')}</h2><p>${t('主 Agent 持续跟进项目，协调检索、复核、流程和运维。')}</p><p class="muted">${t('直接描述目标，或输入 @ 选择一位子 Agent 参与。')}</p><div class="prompt-list">${currentAgentPrompts().map((prompt,index)=>`<button onclick="usePrompt(${index})">${I(index===0?'flow':index===1?'activity':'folder')}<span>${t(prompt)}</span>${I('arrow')}</button>`).join('')}</div></div>`}</div>
      <div class="chat-jump"><button id="chat-jump-latest" class="button small" onclick="jumpToLatest()" hidden>${I('down')}${t('回到最新消息')}</button></div>
      <div class="assistant-compose">${messageQueue()}${c?.archived?`<div class="setup-hint"><span>${t('这段对话已归档，恢复后可继续。')}</span>${btn(t('恢复对话'),`updateConversation('${c.id}',{archived:false})`,'small','archive')}</div>`:!ready?`<div class="setup-hint"><span>${t(models().length?'先为主 Agent 配置可用模型':'先在偏好设置中添加模型')}</span>${btn(t('配置模型'),models().length?"agentModal('assistant')":"go('settings')",'small','settings')}</div>`:''}${outbox&&!sending?`<div class="send-error" role="status">${I('alert')}<span>${t('发送尚未确认。重试会核对同一条消息，不会重复提交。')}</span></div>`:''}<div class="chat-box"><div id="agent-mentions" class="mention-menu" role="listbox" aria-label="${t('调度子 Agent')}" hidden></div><textarea id="agent-message" aria-controls="agent-mentions" aria-expanded="false" aria-autocomplete="list" aria-label="${t('向 Agent 提问')}" placeholder="${t(queue.length||paused?'继续输入，消息将加入队列…':'描述项目目标，输入 @ 调度子 Agent…')}" ${c?.archived?'disabled':''} oninput="setChatDraft(this.value);if(!ui.chatComposing)updateMentions(this)" onclick="updateMentions(this)" onblur="closeMentions()" onkeyup="if(['ArrowLeft','ArrowRight','Home','End'].includes(event.key))updateMentions(this)" oncompositionstart="ui.chatComposing=true" oncompositionend="ui.chatComposing=false;updateMentions(this)" onkeydown="chatComposerKeydown(event)">${esc(draft)}</textarea><div class="composer-footer"><button class="mention-trigger" onclick="openAgentMentions()" title="${t('调度子 Agent')}" aria-label="${t('调度子 Agent')}" ${c?.archived?'disabled':''}>@</button><button class="composer-model" onclick="${models().length?`agentModal('${a?.id}')`:"go('settings')"}" title="${esc(t('配置模型'))}">${I('agent')}<span>${m?esc(m.name):t('配置模型')}</span>${I('chevron')}</button><div class="composer-actions"><small id="chat-input-count" class="${tooLong?'over-limit':''}">${tooLong?t('消息过长，请缩短后发送'):draft?t('{n} 字',{n:Array.from(draft).length}):''}</small><button id="chat-send" class="send-button ${sending?'is-sending':''}" onclick="sendAgent()" aria-label="${t(sending?'发送中…':outbox?'重试发送':queue.length||paused?'加入队列':'发送')}" title="${t(sending?'发送中…':outbox?'重试发送':queue.length||paused?'加入队列':'发送')}" ${blocked||(!outbox&&(!draft.trim()||tooLong))?'disabled':''}>${I(sending||outbox?'sync':'send')}</button></div></div></div><div class="compose-note"><span>${t('Enter 发送 · Shift + Enter 换行')}</span><span>${t('对话自动保存在本地')}</span></div></div>
    </section></div>`;
}

function updateComposer(){
  resizeComposer();
  const draft=chatDraft(),tooLong=messageSize(draft)>8000,key=conversationKey(),count=document.getElementById('chat-input-count'),send=document.getElementById('chat-send');
  if(count){count.textContent=tooLong?t('消息过长，请缩短后发送'):draft?t('{n} 字',{n:Array.from(draft).length}):'';count.classList.toggle('over-limit',tooLong);}
  if(send)send.disabled=!meta.ready?.[currentAgent()?.id]||!!currentConversation()?.archived||ui.chatSending.has(key)||(!ui.chatOutbox[key]&&(!draft.trim()||tooLong));
}
function resizeComposer(){
  const input=document.getElementById('agent-message');if(!input)return;
  const log=document.querySelector('.chat-log'),atEnd=log&&log.scrollHeight-log.scrollTop-log.clientHeight<48;
  input.style.height='0px';input.style.height=Math.min(160,Math.max(56,input.scrollHeight))+'px';
  if(atEnd)log.scrollTop=log.scrollHeight;
}
function chatComposerKeydown(event){
  if(event.isComposing||ui.chatComposing||event.keyCode===229||event.repeat)return;
  chatKeydown(event);
}
function acceptChatMessage(key,payload,receipt){
  const sk=scopeKey(payload.projectId,payload.agentId),draft=ui.chatDrafts[key]||'';
  if(ui.conversationSelections[sk]===(payload.conversationId||''))ui.conversationSelections[sk]=receipt.conversationId;
  if(draft.trim()===payload.message)delete ui.chatDrafts[key];
  else if(key!==receipt.conversationId&&draft){ui.chatDrafts[receipt.conversationId]=draft;delete ui.chatDrafts[key];}
  delete ui.chatOutbox[key];saveChatUI();
}
function sendAgent(){
  const key=conversationKey(),a=currentAgent(),message=chatDraft().trim(),c=currentConversation();
  if(!a||!meta.ready?.[a.id]||c?.archived||ui.chatSending.has(key))return;
  if(!ui.chatOutbox[key]){
    if(!message)return;
    if(messageSize(message)>8000){toast(t('消息过长，请缩短后发送'));return;}
    ui.chatOutbox[key]={projectId:ui.projectId||'',conversationId:c?.id||'',agentId:a.id,message,clientId:crypto.randomUUID()};saveChatUI();
  }
  const payload=ui.chatOutbox[key];ui.chatSending.add(key);closeMentions();jumpToLatest();render();
  run(async()=>{
    try{const receipt=(await request('enqueueMessage',payload)).result;acceptChatMessage(key,payload,receipt);await refresh(true);}
    catch(error){
      await refresh(true).catch(()=>{});
      if(ui.chatOutbox[key]){
        if(error.status>=400&&error.status<500){delete ui.chatOutbox[key];saveChatUI();}
        toast(error.message||t('发送失败'));
      }
    }
    finally{ui.chatSending.delete(key);render();if(conversationKey()===key||currentConversation()?.id===ui.conversationSelections[scopeKey(payload.projectId,payload.agentId)])document.getElementById('agent-message')?.focus({preventScroll:true});}
  });
}
function retryChatMessage(mid){
  const original=scopeHistory().find(m=>m.id===mid);if(!original)return;
  const c=(db.conversations||[]).find(c=>c.id===original.conversationId);if(!c)return;
  if(ui.chatOutbox[c.id]||ui.chatSending.has(c.id)){toast(t('请先确认上一条消息的发送结果'));return;}
  openConversation(c.id);ui.chatOutbox[c.id]={projectId:c.projectId,conversationId:c.id,agentId:c.agentId,message:original.content,retryOf:mid,clientId:crypto.randomUUID()};saveChatUI();sendAgent();
}
function cancelChatMessage(mid){run(()=>act('cancelMessage',{projectId:ui.projectId||'',messageId:mid},''));}
function setQueuePaused(paused){run(()=>act(paused?'pauseChatQueue':'resumeChatQueue',{projectId:ui.projectId||''},''));}
function editQueuedMessage(mid){
  const m=scopeHistory().find(m=>m.id===mid);if(!m)return;ui.editQueueScope=ui.projectId||'';
  modal(t('编辑排队消息'),`<p class="muted">${t('仅在消息开始执行前可修改，保存后保持原排队位置。')}</p>`+field(t('消息内容'),'queued-message',m.content,'textarea'),btn(t('保存'),`saveQueuedMessage('${mid}')`,'primary','check'),'chat-modal');
}
function saveQueuedMessage(mid){run(async()=>{await act('editQueuedMessage',{projectId:ui.editQueueScope,messageId:mid,message:val('queued-message')},'排队消息已更新');closeModal();});}
function updateConversation(cid,patch){const c=(db.conversations||[]).find(c=>c.id===cid);if(!c)return;run(async()=>{await act('updateConversation',{projectId:c.projectId,conversationId:cid,...patch},'');saveChatUI();});}
function renameConversation(cid){
  const c=(db.conversations||[]).find(c=>c.id===cid);if(!c)return;
  modal(t('重命名对话'),field(t('对话名称'),'conversation-title',c.title,'input','maxlength="100"'),btn(t('保存'),`saveConversationTitle('${cid}')`,'primary','check'),'chat-modal');
}
function saveConversationTitle(cid){
  const c=(db.conversations||[]).find(c=>c.id===cid);if(!c)return;
  run(async()=>{await act('updateConversation',{projectId:c.projectId,conversationId:cid,title:val('conversation-title')},'');closeModal();});
}
async function copyText(value){
  try{await navigator.clipboard.writeText(value);}catch{
    const area=document.createElement('textarea');area.value=value;area.style.position='fixed';area.style.opacity='0';document.body.append(area);area.select();const ok=document.execCommand('copy');area.remove();if(!ok)throw new Error(t('复制失败，请手动选择文本'));
  }
  toast(t('已复制'));
}
function copyMessage(mid){const m=scopeHistory().find(m=>m.id===mid);if(m)run(()=>copyText(m.content));}
function exportConversation(cid){
  const c=(db.conversations||[]).find(c=>c.id===cid);if(!c)return;
  const messages=scopeHistory(c.projectId).filter(m=>m.conversationId===cid),replies=new Map(messages.filter(m=>m.replyTo).map(m=>[m.replyTo,m]));
  const ordered=messages.filter(m=>!m.replyTo).flatMap(m=>replies.has(m.id)?[m,replies.get(m.id)]:[m]);
  const text='# '+c.title+'\n\n'+ordered.map(m=>'## '+(m.role==='user'?t('你'):t(agents().find(a=>a.id===m.agent)?.name||'Agent'))+' · '+date(m.at)+' · '+t(chatStatusNames[m.status]||m.status)+'\n\n'+m.content+(m.trace?.length?'\n\n'+t('执行过程')+'\n\n'+m.trace.map(x=>'### '+x.tool+' · '+x.ms+' ms\n\n'+t('调用参数')+'\n\n'+prettyToolValue(x.args)+'\n\n'+t('返回结果')+'\n\n'+prettyToolValue(x.result)).join('\n\n'):'')).join('\n\n');
  const url=URL.createObjectURL(new Blob([text],{type:'text/markdown;charset=utf-8'})),link=document.createElement('a');link.href=url;link.download=c.title.replace(/[\\/:*?"<>|]/g,'_')+'.md';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
}
function onChatScroll(){
  const log=document.querySelector('.chat-log');if(!log)return;
  const bottom=log.scrollHeight-log.scrollTop-log.clientHeight<48;
  ui.chatScrolls[conversationKey()]={top:log.scrollTop,bottom};
  const jump=document.getElementById('chat-jump-latest');if(jump)jump.hidden=bottom;
  const c=currentConversation();if(bottom&&c&&ui.chatReadCounts[c.id]!==replyCount(c.id)){ui.chatReadCounts[c.id]=replyCount(c.id);saveChatUI();}
}
function jumpToLatest(){const log=document.querySelector('.chat-log');if(log){log.scrollTop=log.scrollHeight;onChatScroll();}}
