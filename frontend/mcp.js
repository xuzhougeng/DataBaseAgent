'use strict';

const mcpServers=()=>db.settings.mcpServers||[];
const mcpStateLabels={disabled:'已停用',needs_token:'待登录或配置 Token',needs_env:'待配置环境变量',configured:'待测试连接'};
let mcpEditingId='',mcpTesting=false,mcpTestSequence=0;

// Authenticated plugins show how they are signed in; "configured" alone still reads as "test pending".
function mcpCardBadge(id){const state=meta.mcpStates?.[id]||'configured',auth=state==='configured'&&meta.mcpAuth?.[id];return auth?badge(t(auth==='oauth'?'已登录':'已配置 Token')):badge(t(mcpStateLabels[state]||'待测试连接'),'gray');}
function mcpSettings(){
  return `<section class="card"><div class="card-head"><h2>${t('MCP 插件')}</h2>${btn(t('添加 MCP 插件'),'mcpModal()','small','plus')}</div><div class="content-pad"><p class="muted">${t('默认提供 GitHub 和 Cloudflare。配置认证后，主 Agent 可按需发现和调用插件工具，调用记录保留在对话中。')}</p><div class="mcp-grid">${mcpServers().map(v=>`<button class="mcp-card" onclick="mcpModal('${v.id}')" aria-haspopup="dialog"><span class="mcp-card-heading"><strong>${esc(v.name)}</strong>${mcpCardBadge(v.id)}</span><span class="muted mono">${esc(v.transport==='http'?v.url:v.command)}</span><span class="mcp-card-heading"><small>${v.transport==='stdio'?'STDIO':'Streamable HTTP'}</small><span class="text-button">${t('配置与测试')} ${I('chevron')}</span></span></button>`).join('')||`<p class="muted">${t('尚未配置 MCP 插件')}</p>`}</div></div></section>`;
}
function mcpModal(sid){
  const v=mcpServers().find(x=>x.id===sid)||{transport:'http',enabled:true,startupTimeoutSec:20,toolTimeoutSec:60};
  mcpEditingId=v.id||'';mcpTestSequence++;mcpTesting=false;
  const auth=meta.mcpAuth?.[v.id]||'';
  modal(t(v.id?'配置 MCP 插件':'添加 MCP 插件'),`<form id="mcp-form" onsubmit="event.preventDefault();saveMCP()">
    ${field(t('插件名称'),'mcp-name',v.name||'','input','required maxlength="120"')}
    <div class="form-grid"><div class="field"><label for="mcp-transport">${t('传输方式')}</label><select id="mcp-transport" onchange="updateMCPTransport()"><option value="http" ${v.transport==='http'?'selected':''}>Streamable HTTP</option><option value="stdio" ${v.transport==='stdio'?'selected':''}>STDIO</option></select></div><label class="mcp-enable"><input type="checkbox" id="mcp-enabled" ${v.enabled?'checked':''}> ${t('启用插件')}</label></div>
    <div id="mcp-http">${field(t('MCP 服务地址'),'mcp-url',v.url||'','input','type="url"')}
      <div class="field"><label>${t('认证')}</label>
        <p class="mcp-auth">${auth==='oauth'?badge(t('已通过浏览器登录')):auth==='token'?badge(t('已保存 Token'),'blue'):badge(t('未认证'),'gray')} ${v.id?btn(t(auth==='oauth'?'重新登录':'浏览器登录'),'loginMCP()','small','arrow'):`<span class="muted">${t('保存后即可使用浏览器登录')}</span>`}</p>
        <p class="muted">${t('推荐浏览器登录：在浏览器中授权，无需手动复制 Token。服务不支持登录时，改为填写下方 Token。')}</p>
      </div>
      ${field(t('Bearer Token（备选）'),'mcp-token','','input','type="password" autocomplete="new-password" spellcheck="false"')}
      <p class="muted">${t(auth?'留空保持当前认证；填写新 Token 会替换已保存的登录或 Token。':'GitHub 填写 PAT，Cloudflare 填写 API Token。Token 存入系统凭据库。')}</p>
      ${auth?`<label class="muted"><input type="checkbox" id="mcp-clear-token" onchange="document.getElementById('mcp-token').disabled=this.checked"> ${t(auth==='oauth'?'退出登录（移除已保存的登录）':'移除已保存的 Token')}</label>`:''}
      <details><summary>${t('高级认证')}</summary>
        ${field(t('OAuth Client ID（可选）'),'mcp-client-id',v.oauthClientId||'','input','autocomplete="off" spellcheck="false"')}
        ${field(t('OAuth Client Secret（可选，仅登录时使用）'),'mcp-client-secret','','input','type="password" autocomplete="new-password" spellcheck="false"')}
        <p class="muted">${t('GitHub 等不支持自动注册的服务需要自建 OAuth App：回调地址填 http://127.0.0.1/callback（端口任意）。Secret 只随登录保存在系统凭据库。')}</p>
        ${field(t('Token 环境变量（可选）'),'mcp-token-env',v.bearerTokenEnv||'')}
        <p class="muted">${t('已保存的登录或 Token 优先于环境变量；无认证服务请全部留空。')}</p>
      </details>
    </div>
    <div id="mcp-stdio">${field(t('可执行文件或命令名'),'mcp-command',v.command||'')}${field(t('命令参数（每行一个）'),'mcp-args',(v.args||[]).join('\n'),'textarea')}${field(t('传入的环境变量名（每行一个）'),'mcp-env',(v.envVars||[]).join('\n'),'textarea')}<p class="muted">${t('本地插件以当前用户身份运行，工作目录为当前工作空间。参数直接传给程序；凭据请通过环境变量传入。')}</p></div>
    <details><summary>${t('超时与工具范围')}</summary><div class="form-grid">${field(t('连接超时（秒）'),'mcp-startup',v.startupTimeoutSec,'input','type="number" min="1" max="120" required')}${field(t('工具超时（秒）'),'mcp-timeout',v.toolTimeoutSec,'input','type="number" min="1" max="600" required')}</div>${field(t('允许的工具（每行一个，留空允许全部）'),'mcp-allow',(v.enabledTools||[]).join('\n'),'textarea')}${field(t('禁用的工具（每行一个，优先于允许列表）'),'mcp-deny',(v.disabledTools||[]).join('\n'),'textarea')}</details>
    <p class="muted">${t('保存后测试已保存的配置。测试只连接并读取工具列表。')}</p><div id="mcp-test-result" aria-live="polite"></div>
  </form>`,(v.id?btn(t('删除插件'),'deleteMCP()','','trash')+btn(t('测试连接'),'testMCP()','','sync'):'')+`<button class="button primary" type="submit" form="mcp-form">${t('保存')}</button>`,'mcp-modal');
  updateMCPTransport();document.getElementById('mcp-name').focus();
}
function updateMCPTransport(){
  const http=val('mcp-transport')==='http';
  document.getElementById('mcp-http').hidden=!http;
  document.getElementById('mcp-stdio').hidden=http;
  document.getElementById('mcp-url').required=http;
  document.getElementById('mcp-command').required=!http;
}
function mcpLines(id){return val(id).split('\n').map(s=>s.trim()).filter(Boolean);}
function saveMCP(){
  if(!document.getElementById('mcp-form').reportValidity())return;
  const token=val('mcp-token'),clear=document.getElementById('mcp-clear-token')?.checked;
  const mcpServer={id:mcpEditingId,name:val('mcp-name'),transport:val('mcp-transport'),enabled:document.getElementById('mcp-enabled').checked,url:val('mcp-url'),command:val('mcp-command'),args:mcpLines('mcp-args'),envVars:mcpLines('mcp-env'),bearerTokenEnv:val('mcp-token-env'),oauthClientId:val('mcp-client-id'),startupTimeoutSec:Number(val('mcp-startup')),toolTimeoutSec:Number(val('mcp-timeout')),enabledTools:mcpLines('mcp-allow'),disabledTools:mcpLines('mcp-deny')};
  run(async()=>{
    const reply=await act('saveMCPServer',{mcpServer,apiKey:mcpServer.transport==='http'?(clear?'':token||undefined):''},'MCP 插件配置已保存');
    if(!reply)return;
    if(mcpServer.id){closeModal();return;}
    // A new plugin reopens so the user can sign in or test it right away.
    mcpModal(reply.result.mcpServerId);
    document.getElementById('mcp-test-result').textContent=t('已保存。现在可以使用浏览器登录或测试连接。');
  });
}
// While a test or sign-in runs, every control in the dialog is disabled and closeModal() refuses, so the request cannot be abandoned by accident.
function mcpBusy(on){mcpTesting=on;document.querySelectorAll('.mcp-modal button').forEach(b=>b.disabled=on);const back=document.querySelector('.modal-backdrop');if(back)back.dataset.locked=on?'1':'';}
function testMCP(){
  if(mcpTesting)return;
  const sid=mcpEditingId,sequence=++mcpTestSequence;
  const result=document.getElementById('mcp-test-result');if(!sid||!result)return;
  mcpBusy(true);result.textContent=t('正在连接并读取工具列表…');
  run(async()=>{
    try{
      const reply=await request('testMCPServer',{mcpServerId:sid});
      if(sequence===mcpTestSequence&&result.isConnected)result.innerHTML=`<p>${esc(t('连接成功，可用工具 {n} 个',{n:reply.result.toolCount}))}</p>${code(reply.result.tools.join('\n'))}`;
    }catch(error){if(sequence===mcpTestSequence&&result.isConnected)result.textContent=error.message;}
    finally{if(sequence===mcpTestSequence)mcpBusy(false);}
  });
}
function loginMCP(){
  if(mcpTesting)return;
  const sid=mcpEditingId,sequence=++mcpTestSequence,secret=val('mcp-client-secret');
  const result=document.getElementById('mcp-test-result');if(!sid||!result)return;
  mcpBusy(true);result.textContent=t('正在连接服务并打开浏览器授权页面，完成授权后会自动返回…');
  run(async()=>{
    try{
      const reply=await request('loginMCPServer',{mcpServerId:sid,apiKey:secret||undefined});
      await refresh(true);
      if(sequence!==mcpTestSequence)return;
      mcpModal(sid);mcpTestSequence=sequence;
      document.getElementById('mcp-test-result').innerHTML=`<p>${esc(t(reply.result.signedIn?'登录成功，可用工具 {n} 个':'该服务无需登录，可用工具 {n} 个',{n:reply.result.toolCount}))}</p>${code(reply.result.tools.join('\n'))}`;
    }catch(error){if(sequence===mcpTestSequence&&result.isConnected)result.textContent=error.message;}
    finally{if(sequence===mcpTestSequence)mcpBusy(false);}
  });
}
function deleteMCP(){
  const sid=mcpEditingId;
  modal(t('删除插件'),`<p>${t('删除插件配置及已保存的登录或 Token？此操作不会删除远端资源。')}</p>`,btn(t('确认删除'),`confirmDeleteMCP('${sid}')`,'primary','trash'));
}
function confirmDeleteMCP(sid){run(async()=>{await act('deleteMCPServer',{mcpServerId:sid},'MCP 插件已删除');closeModal();});}
