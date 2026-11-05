// Run with node --test scripts/test-conversations.js. No browser or npm dependencies.
const test=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
function fixture(){
  const saved=new Map(),document={documentElement:{},getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},activeElement:null};
  const ctx=vm.createContext({document,navigator:{language:'en'},localStorage:{getItem:k=>saved.get(k)||null,setItem:(k,v)=>saved.set(k,v)},window:{innerWidth:1280},TextEncoder,crypto:{randomUUID:()=> 'client-test'},setTimeout:()=>{},clearTimeout:()=>{},setInterval:()=>{},console});
  for(const name of ['i18n','conversations','workbench']){
    let source=fs.readFileSync(`frontend/${name}.js`,'utf8');
    if(name==='workbench')source=source.slice(0,source.lastIndexOf('run(async()=>{await refresh(true);'));
    vm.runInContext(source,ctx);
  }
  vm.runInContext(`
    db={projects:[],servers:[],jobs:[],artifacts:[],events:[],messages:[],conversations:[],chatQueuePaused:{},settings:{agents:[{id:'assistant',name:'Project assistant',icon:'agent',description:'Test',tools:[]},{id:'scout',name:'Search strategist',description:'Test',tools:[]}],models:[{id:'test',name:'Test model'}]}};
    meta={root:'/isolated-fixture',ready:{assistant:true,scout:true},agentRuns:[]};
    ui.projectId='';ui.agentId='assistant';syncChatState();
    render=()=>{};
  `,ctx);
  return {ctx,eval:code=>vm.runInContext(code,ctx),saved};
}

test('drafts and selections are isolated by conversation, project, and agent and survive reload',()=>{
  const f=fixture();
  f.eval(`setChatDraft('workspace draft');chooseAgent('scout');setChatDraft('scout draft');chooseAgent('assistant');`);
  assert.equal(f.eval('chatDraft()'),'workspace draft');
  f.eval(`db.conversations=[{id:'c1',projectId:'',agentId:'assistant',title:'first',updatedAt:'2026-01-01'}];openConversation('c1');setChatDraft('saved chat draft');newConversation();`);
  assert.equal(f.eval('chatDraft()'),'workspace draft');
  f.eval(`ui.chatRoot=null;ui.chatDrafts={};syncChatState();openConversation('c1');`);
  assert.equal(f.eval('chatDraft()'),'saved chat draft');
  f.eval(`db.projects=[{id:'p',messages:[]}];ui.projectId='p';syncChatState();setChatDraft('project draft');ui.projectId='';syncChatState();`);
  assert.equal(f.eval('chatDraft()'),'saved chat draft');
});

test('acceptance reconciles lost acknowledgements without clearing newer typing or switching chats',()=>{
  const f=fixture();
  f.eval(`ui.chatOutbox['new:|assistant']={clientId:'one',projectId:'',agentId:'assistant',conversationId:'',message:'sent'};ui.chatDrafts['new:|assistant']='new typing';db.conversations=[{id:'c1',projectId:'',agentId:'assistant',title:'sent',updatedAt:'2026'}];db.messages=[{id:'m1',clientId:'one',conversationId:'c1',role:'user',content:'sent'}];syncChatState();`);
  assert.equal(f.eval('currentConversation().id'),'c1');
  assert.equal(f.eval('chatDraft()'),'new typing');
  assert.equal(f.eval('Object.keys(ui.chatOutbox).length'),0);
  f.eval(`ui.conversationSelections['|assistant']='other';acceptChatMessage('c1',{projectId:'',agentId:'assistant',conversationId:'c1',message:'old'},{conversationId:'c1'});`);
  assert.equal(f.eval(`ui.conversationSelections['|assistant']`),'other');
});

test('tool timeline escapes untrusted arguments, results, and titles',()=>{
  const f=fixture();
  f.eval(`db.conversations=[{id:'c1',projectId:'',agentId:'assistant',title:'<img src=x onerror=bad()>',updatedAt:'2026-01-01'}];ui.conversationSelections['|assistant']='c1';`);
  const html=f.eval(`trace({id:'m1',trace:[{tool:'get_project',args:'{"x":"<script>bad()</script>"}',result:'<img src=x onerror=bad()>',error:true,ms:123,step:1}]})`);
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(html.includes('&lt;img'));
  assert.ok(!html.includes('<script>'));
  assert.ok(html.includes('Failed'));
  assert.ok(html.includes('123 ms'));
  const history=f.eval('chatHistoryPanel()');
  assert.ok(!history.includes('<img'));
});

test('chat renders user/reply pairs in order even when later requests were already queued',()=>{
  const f=fixture();
  f.eval(`db.messages=[{id:'u1',conversationId:'c',role:'user',agent:'assistant',content:'first',status:'completed'},{id:'u2',conversationId:'c',role:'user',agent:'assistant',content:'second',status:'completed'},{id:'a1',conversationId:'c',role:'assistant',agent:'assistant',content:'first answer',replyTo:'u1',status:'completed'},{id:'a2',conversationId:'c',role:'assistant',agent:'assistant',content:'second answer',replyTo:'u2',status:'completed'}];`);
  const html=f.eval('chatLog(db.messages)');
  assert.ok(html.indexOf('data-message-id="a1"')<html.indexOf('data-message-id="u2"'));
  assert.ok(html.indexOf('data-message-id="u2"')<html.indexOf('data-message-id="a2"'));
});

test('input remains usable during execution and empty submissions are disabled',()=>{
  const f=fixture();
  f.eval(`meta.agentRuns=[{projectId:'',agentId:'assistant',messageId:'u1'}];db.messages=[{id:'u1',conversationId:'c',role:'user',agent:'assistant',content:'running',status:'running'}];`);
  let html=f.eval('agentPage()');
  assert.ok(!/<textarea[^>]*disabled/.test(html));
  assert.match(html,/<button id="chat-send"[^>]*disabled/);
  f.eval(`setChatDraft('another prompt')`);
  html=f.eval('agentPage()');
  assert.match(html,/<button id="chat-send"[^>]*aria-label="Queue message"/);
  assert.ok(!/<button id="chat-send"[^>]*disabled/.test(html));
  assert.ok(!/<button id="chat-send"[^>]*>[^<]*Send/.test(html));
});

test('IME composition, held Enter, and Shift+Enter never submit a partial message',()=>{
  const f=fixture();f.eval('var submitted=0;sendAgent=()=>submitted++;');
  for(const event of [{isComposing:true},{keyCode:229},{shiftKey:true},{repeat:true}]){
    f.ctx.inputEvent={key:'Enter',preventDefault:()=>{},...event};f.eval('chatComposerKeydown(inputEvent)');
  }
  assert.equal(f.eval('submitted'),0);
  f.ctx.inputEvent={key:'Enter',preventDefault:()=>{}};f.eval('chatComposerKeydown(inputEvent)');
  assert.equal(f.eval('submitted'),1);
});

test('long histories render progressively without dropping stored messages',()=>{
  const f=fixture();
  f.eval(`db.messages=Array.from({length:45},(_,i)=>({id:'u'+i,role:'user',agent:'assistant',content:'message '+i,status:'completed'}));`);
  const html=f.eval('chatLog(db.messages)');
  assert.ok(html.includes('Load earlier messages'));
  assert.ok(!html.includes('data-message-id="u0"'));
  assert.ok(html.includes('data-message-id="u44"'));
  assert.equal(f.eval('db.messages.length'),45);
});
