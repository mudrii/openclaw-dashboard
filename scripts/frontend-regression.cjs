// Runs the actual embedded functions without a browser or external packages.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync(require('node:path').join(__dirname, '../web/index.html'), 'utf8');
function section(start, end) {
  const a = html.indexOf(start), b = html.indexOf(end, a);
  assert.ok(a >= 0 && b > a, `missing source section ${start}`);
  return html.slice(a, b);
}
const nodes = new Map();
// absent models ids that are not in the static markup: they only exist once the
// page creates and inserts them, and go away again on remove(). Everything else
// is auto-created on first access so tests stay terse.
const absent = new Set(['gw-readiness-alert']);
function makeNode(id) {
  return {
    id, className:'', title:'', innerHTML:'', textContent:'', style:{}, value:'all', hidden:false, disabled:false,
    children:[],
    replaceChildren(){this.innerHTML='';this.children=[];},
    append(...kids){for(const c of kids){this.children.push(c);this.innerHTML+=c==null?'':(c.innerHTML||c.textContent||'');}},
    appendChild(c){this.append(c);return c;},
    querySelector(sel){return this.children.find(c=>String(c.className||'').split(/\s+/).includes(sel.replace(/^\./,'')))||null;},
    querySelectorAll(){return [];},
    insertAdjacentElement(_pos, el){if(el&&el.id){absent.delete(el.id);nodes.set(el.id, el);}},
    addEventListener(){},
    remove(){if(this.id){absent.add(this.id);nodes.delete(this.id);}},
    closest(){return null;}, setAttribute(){}, removeAttribute(){},
  };
}
const $ = id => {
  if (absent.has(id)) return null;
  if (!nodes.has(id)) nodes.set(id, makeNode(id));
  return nodes.get(id);
};
// Deterministic fetch stub: tests set fetchResponses to an array of handlers keyed by URL prefix.
const fetchRoutes = new Map();
const timers = {intervals:new Map(), nextId:1, setCount:0, clearCount:0};
const context = vm.createContext({$, console, URL, URLSearchParams, AbortSignal, setTimeout, clearTimeout,
  window:{},
  COLORS:['red'],
  document:{addEventListener(){},querySelectorAll(){return [];},getElementById:$,createElement(tag){const n=makeNode('');n.tag=tag;return n;}},
  requestAnimationFrame:fn=>fn(),
  fetchRoutes, timers,
  setInterval(fn,ms){const id=timers.nextId++;timers.intervals.set(id,{fn,ms});timers.setCount++;return id;},
  clearInterval(id){if(timers.intervals.delete(id))timers.clearCount++;},
  async fetch(url){
    for(const [prefix,handler] of fetchRoutes) if(String(url).startsWith(prefix)) return handler(String(url));
    throw new Error('no fetch route for '+url);
  },
});
context.globalThis = context;
vm.runInContext(section('const COLORS=', '// === Theme ===') +
  section('const State =', '// === Diagnostics ===') +
  section('const LogTail =', '// === DirtyChecker ===') +
  section('const DirtyChecker =', 'const RuntimePanels =') +
  section('const Renderer =', '// === Chat ===') +
  section('const SystemBar =', '// === Collapsible Sections ===') +
  section('const OperationsPanel =', 'OperationsPanel.probe();') +
  section('const App =', 'App.init();'), context);
// Renderer reads paging fields off RuntimePanels; the tested object is TestedRuntimePanels below.
context.RuntimePanels = {render(){}, pageSize:50, sessionPage:0};
let failed = 0;
const pending = [];
// testAsync defers the body so tests can await App.refresh() and other promise-based flows.
function testAsync(name, code) {
  pending.push(async () => {
    try { await vm.runInContext('(async()=>{'+code+'\n})()', context); console.log('PASS', name); }
    catch (e) { failed++; console.error('FAIL', name, e.message); }
  });
}
function test(name, code) {
  try { vm.runInContext('{'+code+'}', context); console.log('PASS', name); }
  catch (e) { failed++; console.error('FAIL', name, e.message); }
}
context.assert = assert;
context.html = html;
vm.runInContext(section('const RuntimePanels =',"document.addEventListener('change'").replace('const RuntimePanels =','const TestedRuntimePanels ='),context);
// SystemBar delegates the runtime-health panel to RuntimePanels; wire it to the
// real implementation so renderHealth is exercised rather than stubbed out.
vm.runInContext('RuntimePanels.renderHealth=D=>TestedRuntimePanels.renderHealth(D);RuntimePanels.renderGateway=D=>TestedRuntimePanels.renderGateway(D);', context);
test('runtime diagnostic cards stack on mobile and contain wide content', `
  assert.ok(/@media\\(max-width:768px\\)[^\\n]*#runtimeDiagnosticsRow[^\\n]*grid-template-columns:1fr!important/.test(html), 'missing mobile diagnostic stacking');
  assert.ok(/#runtimeDiagnosticsRow>div\\{min-width:0;overflow:auto\\}/.test(html), 'missing diagnostic overflow containment');
`);
test('all cost charts distinguish unknown from zero', `
  for(const method of ['renderCostChart','renderModelChart']){
    Renderer[method]('chart',[{total:null,models:{},label:'today'}]);
    assert.match($('chart').innerHTML,/Cost unavailable/);
    assert.doesNotMatch($('chart').innerHTML,/<svg/);
    Renderer[method]('chart',[{total:0,models:{model:0},label:'today'}]);
    assert.match($('chart').innerHTML,/<svg/);
  }
`);
test('dense cost charts retain every tooltip without overlapping labels or markers', `
  for(const count of [7,30]){
    const data=Array.from({length:count},(_,i)=>({label:'day-'+i,total:i===count-1?5:0}));
    Renderer.renderCostChart('denseChart',data);
    const svg=$('denseChart').innerHTML;
    assert.equal((svg.match(/<title>/g)||[]).length,count);
    assert.equal((svg.match(/font-weight=/g)||[]).length,count===7?7:0);
    const radii=[...svg.matchAll(/<circle[^>]* r="([^"]+)"/g)].map(m=>Number(m[1]));
    assert.equal(radii.length,count);
    assert.ok(Math.max(...radii)*2<330/(count-1),'markers overlap');
    assert.ok(svg.includes('day-'+(count-1)+': $5.00'));
  }
`);
test('container runtime card updates when collection arrives after system poll', `
  const data={runtimeTarget:{mode:'container'},gateway:{status:'unknown',statusReason:'host_probe_not_applicable'},runtimeInfo:{pid:42,uptimeMs:60000},runtimeHealth:{runtimeVersion:'2026.9.2',processMemory:{rssBytes:1048576}},collections:{runtimeInfo:{state:'ready',source:'gateway.status'}}};
  TestedRuntimePanels.renderGateway(data);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/42/);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/1m 0s/);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/1.0 MiB/);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/host probe not applicable/);
`);
test('empty session controls disable and recover when sessions arrive', `
  const link=TestedRuntimePanels.linkSession,tasks=TestedRuntimePanels.renderTasks;
  try{
    TestedRuntimePanels.linkSession=()=>{};TestedRuntimePanels.renderTasks=()=>{};
    TestedRuntimePanels.render({sessions:[]});
    assert.equal($('workSessionInspect').disabled,true);
    assert.equal($('nativeSession').hidden,true);
    assert.match($('workSessionSelect').innerHTML,/No sessions reported/);
    TestedRuntimePanels.render({sessions:[{key:'agent:main:one',name:'One'}]});
    assert.equal($('workSessionInspect').disabled,false);
    assert.equal($('nativeSession').hidden,false);
  }finally{TestedRuntimePanels.linkSession=link;TestedRuntimePanels.renderTasks=tasks;}
`);
test('selected container health uses its RPC verdict and does not retain stale Online', `
  const data={runtimeTarget:{mode:'container'},gateway:{status:'unknown',statusReason:'host_probe_not_applicable'},gatewayHealth:{ok:true},collections:{gatewayHealth:{state:'ready',complete:true,source:'gateway.health'}}};
  TestedRuntimePanels.renderGateway(data);
  assert.match($('hGw').innerHTML,/Online/);
  data.collections.gatewayHealth.state='stale';
  TestedRuntimePanels.renderGateway(data);
  assert.doesNotMatch($('hGw').innerHTML,/Online/);
  assert.match($('hGw').innerHTML,/stale/i);
  data.collections.gatewayHealth.state='ready';data.gatewayHealth.ok=false;
  TestedRuntimePanels.renderGateway(data);
  assert.match($('hGw').innerHTML,/Unhealthy/);
`);
test('provider data renders balances quotas and explicit partial spend', `
  TestedRuntimePanels.renderProviders({collections:{providerUsage:{state:'ready'}},providerUsage:{providers:[{provider:'minimax',plan:'Coding Plan',windows:[{label:'5h',usedPercent:0}],billing:[{type:'balance',amount:0,unit:'USD'}]}]},tokenUsage30d:[{modelId:'minimax/M3',knownCost:0,missingCostEntries:3}]});
  const html=$('providerUsagePanel').innerHTML;
  assert.match(html,/Coding Plan/);assert.match(html,/100% left/);assert.match(html,/0 USD/);
  assert.match(html,/3 unpriced entries/);
  assert.doesNotMatch(html,/undefined|NaN/);
`);
test('known cost is visible as a subtotal and never relabelled a complete total', `
  const data={usageToday:{knownCost:1.2345,missingCostEntries:2,tokensComplete:true},usageAll:{knownCost:2,missingCostEntries:3,tokensComplete:true}};
  Renderer.render({data,tabs:{}},{cost:true});
  assert.equal($('cToday').textContent,'$1.2345');assert.match($('cTodayLabel').textContent,/subtotal/);
  assert.match($('cTodaySub').textContent,/not a total/);
  data.totalCostToday=1.23;Renderer.render({data,tabs:{}},{cost:true});
  assert.equal($('cTodayLabel').textContent,"Today's Cost");assert.equal($('cToday').textContent,'$1.23');
`);
test('host metrics failure does not erase fresh selected-container health', `
  const previous=State.data;
  try{
    State.data={runtimeTarget:{mode:'container'},gatewayHealth:{ok:true},collections:{gatewayHealth:{state:'ready',complete:true,source:'gateway.health'}}};
    SystemBar.renderGatewayDegraded('host metrics unavailable');
    assert.match($('hGw').innerHTML,/Online/);
    State.data.collections.gatewayHealth.state='stale';
    SystemBar.renderGatewayDegraded('host metrics unavailable');
    assert.doesNotMatch($('hGw').innerHTML,/Online/);
  }finally{State.data=previous;}
`);
test('configuration preserves zeros and names missing values', `
  const data={agentConfig:{compaction:{mode:'safeguard',reserveTokensFloor:0,softThresholdTokens:0},telegramGroups:0,search:{provider:'fixture',maxResults:0,cacheTtlMinutes:0},gateway:{port:18789},agents:[{id:'main',model:'Alpha',fallbacks:[]}]}};
  Renderer.render({data,tabs:{usage:'today',subRuns:'today',subTokens:'today'}},{agentConfig:true});
  assert.match($('runtimeConfigPanel').innerHTML,/Reserve Tokens/);
  assert.match($('runtimeConfigPanel').innerHTML,/0 configured/);
  assert.doesNotMatch($('searchPanelInner').innerHTML,/—/);
  assert.doesNotMatch($('gatewayConfigPanelInner').innerHTML,/undefined|null/);
  assert.match($('agentTableBody').innerHTML,/None configured/);
  assert.match($('agentTableBody').innerHTML,/Not reported/);
`);
test('empty session cron and usage tables have explicit messages', `
  const data={sessions:[],crons:[]};
  Renderer.render({data,tabs:{usage:'today',subRuns:'today',subTokens:'today'}},{sessions:true,crons:true,usage:true});
  assert.match($('sessBody').innerHTML,/No sessions reported/);
  assert.match($('cronBody').innerHTML,/No automations reported/);
  assert.match($('uBody').innerHTML,/No usage reported/);
`);
test('absolute time uses dashboard timezone, not browser timezone', `
  State.data={timezone:'UTC'};
  assert.match(formatAbsTime('2026-09-05T09:07:00Z'),/09:07/);
  assert.match(formatAbsTime('2026-09-05T09:07:00Z'),/UTC/);
  State.data={timezone:'Asia/Kuala_Lumpur'};
  assert.match(formatAbsTime('2026-09-05T09:07:00Z'),/17:07/);
  State.data={timezone:'invalid'};
  assert.doesNotThrow(()=>formatAbsTime('2026-09-05T09:07:00Z'));
  assert.equal(formatAbsTime('not-a-date'),'—');
`);
test('cron refresh detects time, identity and enabled changes', `
  State.prev={crons:[{id:'old',name:'job',lastStatus:'ok',nextRun:'old'}]};
  const snap={data:{crons:[{id:'old',name:'job',lastStatus:'ok',nextRun:'new'}]},tabs:{}};
  State.data=snap.data;
  assert.equal(DirtyChecker.diff(snap).crons,true);
  snap.data.crons=[{id:'new',name:'job',lastStatus:'ok',nextRun:'old'}];
  assert.equal(DirtyChecker.diff(snap).crons,true);
  snap.data.crons=State.prev.crons;
  assert.equal(DirtyChecker.diff(snap).crons,false);
`);
test('invalid log regex is shown immediately and clears on recovery', `
  LogTail.setRegex('[');
  assert.equal($('logStatus').textContent,'Invalid regex');
  assert.equal($('logStatus').style.display,'block');
  LogTail.setRegex('normal');
  assert.equal($('logStatus').style.display,'none');
`);
test('successful log response clears old network error', `
  LogTail._setStatus('Network error while fetching logs.');
  LogTail._render({entries:[]});
  assert.equal($('logStatus').style.display,'none');
`);
test('log highlighting escapes text once', `
  LogTail.setRegex('<tag>');
  assert.equal(LogTail._highlight('a <tag> b'),'a <mark>&lt;tag&gt;</mark> b');
  LogTail.setRegex('amp');
  assert.equal(LogTail._highlight('a & b'),'a &amp; b');
`);
test('task search includes stable run identity', `
  assert.equal(filterRuntimeTasks([{id:'task',runId:'exact-run'}],'all','all','exact-run').length,1);
`);
test('token totals and bars do not depend on model pricing', `
  Renderer.renderTokenTbl('tokens',[
    {model:'a',calls:1,inputRaw:100,outputRaw:10,cacheReadRaw:20,totalTokensRaw:130,cost:null},
    {model:'b',calls:2,inputRaw:200,outputRaw:20,cacheReadRaw:40,totalTokensRaw:260,cost:1}
  ],'red');
  const out=$('tokens').innerHTML;
  assert.match(out,/width:50%/);
  assert.match(out,/width:100%/);
  assert.match(out,/>300<.*>30<.*>60<.*>390</);
  assert.doesNotMatch(out,/colspan="4"/);
  assert.match(out,/Cost not reported/);
`);
test('selected runtime metrics never fall back to host for container data', `
  const d={runtimeTarget:{mode:'container'},gateway:{pid:999,memory:'host',uptime:'host'},runtimeInfo:{pid:42,uptimeMs:90000},runtimeHealth:{processMemory:{rssBytes:1048576}},collections:{runtimeInfo:{state:'ready'},runtimeHealth:{state:'ready'}}};
  assert.equal(runtimeGatewayMetrics(d).pid,42);
  assert.equal(runtimeGatewayMetrics(d).memory,'1.0 MiB');
  assert.equal(runtimeGatewayMetrics(d).uptime,'1m 30s');
  delete d.runtimeInfo;
  assert.equal(runtimeGatewayMetrics(d).pid,'Not reported');
  assert.equal(runtimeGatewayMetrics({gateway:{pid:999,memory:'native',uptime:'2h'}}).pid,999);
`);
test('active session count is distinct from stored session count', `
  assert.equal(activeSessionSummary({sessions:[{active:false},{active:true}],sessionTotal:3}),'1 active / 3 stored');
`);
test('skills inventory and channel accounts take precedence over legacy config', `
  assert.equal(dashboardSkills({skillInventory:[{name:'found',agentId:'main',eligible:true}],skills:[]})[0].name,'found');
  const channels=dashboardChannels({channels:[{channel:'telegram',accountId:'a',connected:true},{channel:'telegram',accountId:'b',connected:false}],agentConfig:{channelStatus:{telegram:{connected:false}}}});
  assert.equal(Object.keys(channels).length,2);
  assert.equal(channels['telegram / a'].connected,true);
  assert.equal(channels['telegram / b'].connected,false);
`);
test('token trend remains visible when prices are incomplete', `
  Renderer.renderCharts({dailyChart:[{label:'today',total:null,tokens:130,models:{}}]},7);
  assert.match($('costChart').innerHTML,/Token trend/);
  assert.match($('costChart').innerHTML,/<svg/);
  assert.equal($('costChart').innerHTML.includes('$'),false);
`);
test('actual summary renderer maps runtime metrics, skills and partial-price explanation', `
  const d={runtimeTarget:{mode:'container'},runtimeInfo:{pid:42,uptimeMs:90000},runtimeHealth:{processMemory:{rssBytes:1048576}},collections:{runtimeHealth:{state:'ready'},skillInventory:{state:'ready'}},sessions:[{active:false}],sessionTotal:1,skillInventory:[{name:'runtime skill',eligible:true,modelVisible:true}],usageToday:{tokensComplete:true,costComplete:false,knownCost:0.125,missingCostEntries:2,missingCostByModel:{'p/m':2}},totalCostToday:null};
  Renderer.render({data:d,tabs:{}},{cost:true,skills:true});
  assert.equal($('hPid').textContent,42);
  assert.equal($('hMem').textContent,'1.0 MiB');
  assert.equal($('hSess').textContent,'0 active / 1 stored');
  assert.match($('skillsGrid').innerHTML,/runtime skill/);
  assert.match($('cTodaySub').textContent,/Known subtotal.*2 unpriced entries.*p\\/m/);
`);
test('runtime panels distinguish absent features from failed probes', `
  const link=TestedRuntimePanels.linkSession,tasks=TestedRuntimePanels.renderTasks;
  try{
    TestedRuntimePanels.linkSession=()=>{};TestedRuntimePanels.renderTasks=()=>{};
    TestedRuntimePanels.render({sessions:[{name:'idle',active:false}],memory:[{embedding:{checked:false,ok:false}}],channels:[{channel:'telegram',probe:{},connected:true}],pluginInventory:[{id:'bundled'}]});
    assert.match($('sessionWork').innerHTML,/Not applicable/);
    assert.match($('memoryHealth').innerHTML,/Not checked/);
    assert.match($('channelAccounts').innerHTML,/Not probed/);
    assert.match($('pluginInventory').innerHTML,/Not reported/);
  }finally{TestedRuntimePanels.linkSession=link;TestedRuntimePanels.renderTasks=tasks;}
`);
test('channel config renderer preserves separate live accounts and unknown booleans', `
  const d={agentConfig:{channels:['telegram'],channelStatus:{telegram:{connected:false}}},channels:[{channel:'telegram',accountId:'a',connected:true},{channel:'telegram',accountId:'b',connected:false}]};
  Renderer.render({data:d,tabs:{}},{agentConfig:true});
  const out=$('channelConfigPanel').innerHTML;
  assert.match(out,/telegram \\/ a/);
  assert.match(out,/telegram \\/ b/);
  assert.match(out,/NOT REPORTED/);
  assert.doesNotMatch(out,/>DISABLED</);
`);
test('missing task cells are explicit and preserve zero and false', `
  $('taskSearch').value='';$('taskStatus').value='all';$('taskRuntime').value='all';
  TestedRuntimePanels.renderTasks({tasks:[{id:'t',agent:'',ownerKey:'',sessionKey:'',durationSec:0}]});
  assert.doesNotMatch($('taskLedger').innerHTML,/<td><\\/td>/);
  assert.match($('taskLedger').innerHTML,/Not reported/);
  assert.match($('taskLedger').innerHTML,/>0s</);
  assert.equal(runtimeValue(false),'No');assert.equal(runtimeValue(0),'0');
  assert.equal(runtimeValue('  '),'Not reported');
`);
test('chart title follows cost versus token data and resets when empty', `
  Renderer.renderCharts({dailyChart:[{label:'today',total:null,tokens:10,models:{}}]},7);
  assert.equal($('costChartTitle').textContent,'Daily Token Trend');
  Renderer.renderCharts({dailyChart:[{label:'today',total:0,tokens:10,models:{}}]},7);
  assert.equal($('costChartTitle').textContent,'Daily Cost Trend');
  Renderer.renderCharts({dailyChart:[]},7);
  assert.equal($('costChartTitle').textContent,'Daily Cost Trend');
  assert.doesNotMatch(html,/section-title">📡 Active Sessions/);
`);
test('task pagination disables boundaries including empty filtered results', `
  $('taskSearch').value='';$('taskStatus').value='all';$('taskRuntime').value='all';
  const data={tasks:Array.from({length:51},(_,i)=>({id:String(i)}))};
  TestedRuntimePanels.taskPage=0;TestedRuntimePanels.renderTasks(data);
  assert.equal($('taskPrev').disabled,true);assert.equal($('taskNext').disabled,false);
  TestedRuntimePanels.taskPage=1;TestedRuntimePanels.renderTasks(data);
  assert.equal($('taskPrev').disabled,false);assert.equal($('taskNext').disabled,true);
  TestedRuntimePanels.renderTasks({tasks:[]});
  assert.equal(TestedRuntimePanels.taskPage,0);
  assert.equal($('taskPrev').disabled,true);assert.equal($('taskNext').disabled,true);
`);
test('session pagination disables boundaries and clamps shrinking results', `
  const rows=Renderer.reconcileRows,tree=Renderer.renderAgentTree;
  $('sessBody').closest=()=>null;
  try{
    Renderer.reconcileRows=()=>{};Renderer.renderAgentTree=()=>{};
    RuntimePanels.pageSize=50;RuntimePanels.sessionPage=0;
    const data={sessions:Array.from({length:51},(_,i)=>({key:String(i)}))};
    Renderer.render({data,tabs:{}},{sessions:true});
    assert.equal($('sessionPrev').disabled,true);assert.equal($('sessionNext').disabled,false);
    RuntimePanels.sessionPage=1;Renderer.render({data,tabs:{}},{sessions:true});
    assert.equal($('sessionPrev').disabled,false);assert.equal($('sessionNext').disabled,true);
    Renderer.render({data:{sessions:[]},tabs:{}},{sessions:true});
    assert.equal(RuntimePanels.sessionPage,0);
    assert.equal($('sessionPrev').disabled,true);assert.equal($('sessionNext').disabled,true);
  }finally{Renderer.reconcileRows=rows;Renderer.renderAgentTree=tree;}
`);
test('cost labels distinguish missing pricing, missing records, and partial collection', `
  assert.equal(usageMoney(null,{tokensComplete:true,missingCostEntries:2}),'Pricing missing');
  assert.equal(usageMoney(null,{tokensComplete:false,missingCostEntries:2}),'Collection incomplete');
  assert.equal(usageMoney(null,{tokensComplete:true,missingCostEntries:0}),'Cost not reported');
  assert.equal(usageMoney(0,{}),'$0.00');
`);
test('session missing values preserve the source reason', `
  assert.equal(sessionTokenLabel({tokenState:'stale'}),'Stale token count');
  assert.equal(sessionTokenLabel({tokenState:'not_reported'}),'Token count not reported');
  assert.equal(sessionContextLabel({contextState:'limit_not_reported'}),'Context limit not reported');
  assert.equal(sessionContextLabel({contextState:'tokens_stale'}),'Stale token count');
`);
test('backup dates use collected alternate fields and auth does not infer readiness', `
  assert.equal(backupTimestamp({latestSuccess:{completedAt:'2026-09-06T00:00:00Z'}},'success'),'2026-09-06T00:00:00Z');
  assert.match(modelAuthExplanation({authKind:'missing',missingProviderInUse:false}),/CLI.*not.*missing in use/);
  assert.match(modelAuthExplanation({authKind:'missing',missingProviderInUse:true}),/Missing provider in use/);
  assert.match(modelAuthExplanation({authKind:'env',missingProviderInUse:false}),/Inference not tested/);
  assert.equal(taskOwnerLabel({childSessionKey:'agent:child:main'}),'Child session: agent:child:main');
  assert.match(taskAgentLabel({id:'cron-runlog-import:job'}),/Imported.*not reported/);
`);
test('actual renderers expose field reasons, pricing source, and backup configuration', `
  const reconcile=Renderer.reconcileRows,tree=Renderer.renderAgentTree,link=TestedRuntimePanels.linkSession;
  try{
    Renderer.reconcileRows=(id,rows,key,render)=>{$(id).innerHTML=rows.map(render).join('');};Renderer.renderAgentTree=()=>{};
    TestedRuntimePanels.linkSession=()=>{};
    $('sessBody').closest=()=>null;RuntimePanels.pageSize=50;RuntimePanels.sessionPage=0;
    const data={sessions:[{name:'automation',tokenState:'stale',contextState:'tokens_stale',totalTokens:null,contextPct:null}],agentConfig:{backupConfig:{enabled:false},diagnosticConfig:{enabled:false,otel:{enabled:false}},modelPricing:{'p/m':{input:1,output:2,cacheRead:0}}},usageAll:{tokensComplete:true,missingCostEntries:2,missingCostByModel:{'p/m':2}},usageToday:{tokensComplete:true,missingCostEntries:2},diagnostics:{backup:{latestSuccess:{completedAt:'2026-09-06T00:00:00Z'}}}};
    Renderer.render({data,tabs:{}},{sessions:true,cost:true});TestedRuntimePanels.render(data);
    assert.match($('sessBody').innerHTML,/Stale token count/);assert.doesNotMatch($('sessBody').innerHTML,/Unknown/);
    assert.equal($('cAll').textContent,'Pricing missing');
    assert.match($('pricingCoverage').innerHTML,/p\\/m/);assert.match($('pricingCoverage').innerHTML,/\\$0.000000/);
    assert.match($('backupHealth').innerHTML,/explicit config.*No/);assert.match($('backupHealth').innerHTML,/06\\/09\\/2026/);
    assert.doesNotMatch($('backupHealth').innerHTML,/Unknown/);
  }finally{Renderer.reconcileRows=reconcile;Renderer.renderAgentTree=tree;TestedRuntimePanels.linkSession=link;}
`);
test('live status regions are only rewritten when their text changes', `
  const node=$('usageSource');
  let writes=0,value='';
  Object.defineProperty(node,'textContent',{configurable:true,get(){return value;},set(v){writes++;value=v;}});
  try{
    setStatusText('usageSource','same');
    setStatusText('usageSource','same');
    setStatusText('usageSource','other');
    assert.equal(writes,2,'identical text must not be re-announced');
    assert.equal(value,'other');
  }finally{delete node.textContent;node.textContent='';}
  for(const id of ['sessionSource','usageSource','runtimeSource','taskSource'])
    assert.ok(html.includes("setStatusText('"+id+"'"),id+' must be written through setStatusText');
`);
test('the runtime detail dialog is labelled by its own title', `
  const dialog=html.match(/<dialog id="runtimeDetail"[^>]*>/);
  assert.ok(dialog,'runtimeDetail dialog missing');
  assert.match(dialog[0],/aria-labelledby="runtimeDetailTitle"/);
  assert.match(html,/id="runtimeDetailTitle"/);
`);
test('collection labels format stale timestamps and name failed agents', `
  State.data={timezone:'UTC'};
  const stale=collectionLabel({state:'stale',errorCode:'row_limit',collectedAt:'2026-09-05T09:07:00Z'});
  assert.match(stale,/stale/);
  assert.match(stale,/row_limit/);
  assert.match(stale,/09:07/,'stale label must show collectedAt in the dashboard timezone');
  assert.match(stale,/UTC/);
  assert.doesNotMatch(stale,/2026-09-05T09:07:00Z/,'raw ISO timestamps are not the dashboard timezone');
  assert.match(collectionLabel({state:'partial',errorCode:'configuration_unavailable',failedAgents:['main','ops']}),/failed agents: main, ops/);
  assert.equal(collectionLabel(null),'Legacy source');
`);
test('panels reading retained data disclose stale and unavailable collections', `
  window._sysBarActive=false;
  State.data={timezone:'UTC'};
  const D={timezone:'UTC',crons:[],collections:{
    crons:{state:'stale',errorCode:'row_limit',collectedAt:'2026-09-05T09:07:00Z'},
    usageToday:{state:'partial',errorCode:'row_limit'},
    usageAll:{state:'ready'},
    usage30d:{state:'unavailable',errorCode:'configuration_unavailable'},
    sessions:{state:'stale',errorCode:'permission_denied',collectedAt:'2026-09-05T09:07:00Z'},
    configuration:{state:'unavailable',errorCode:'configuration_unavailable'}}};
  Renderer.render({data:D,tabs:{}},{crons:true,cost:true,charts:true,models:true,agentConfig:true});
  assert.match($('cronNotice').textContent,/stale.*row_limit/);
  assert.match($('cronNotice').textContent,/09:07/);
  assert.equal($('cronNotice').hidden,false);
  assert.match($('costNotice').textContent,/partial/);
  assert.match($('chartNotice').textContent,/unavailable.*configuration_unavailable/);
  assert.match($('healthNotice').textContent,/stale.*permission_denied/);
  assert.match($('modelsNotice').textContent,/unavailable/);
  assert.match($('agentConfigNotice').textContent,/unavailable/);
  const R={timezone:'UTC',crons:[],collections:{crons:{state:'ready'},usageToday:{state:'ready'},usageAll:{state:'ready'},usage30d:{state:'ready'},sessions:{state:'ready'},configuration:{state:'ready'}}};
  Renderer.render({data:R,tabs:{}},{crons:true,cost:true,charts:true,models:true,agentConfig:true});
  for(const id of ['cronNotice','costNotice','chartNotice','healthNotice','modelsNotice','agentConfigNotice']){
    assert.equal($(id).hidden,true,id+' must be hidden when the collection is ready');
    assert.equal($(id).textContent,'',id+' must be cleared when the collection is ready');
  }
`);
test('container gateway status is Unknown, never Offline', `
  // Refresh payload shape (internal/apprefresh/refresh_gateway.go).
  window._sysBarActive=false;
  Renderer.render({data:{gateway:{status:'unknown',processScope:'container',statusReason:'host_probe_not_applicable'}},tabs:{}},{});
  assert.match($('hGw').innerHTML,/Unknown \\(container: host probe not applicable\\)/);
  assert.doesNotMatch($('hGw').innerHTML,/Offline/);
  Renderer.render({data:{gateway:{status:'offline'}},tabs:{}},{});
  assert.match($('hGw').innerHTML,/Offline/);
  // /api/system payload shape (internal/appsystem/system_types.go: SystemGateway.Error).
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'unknown',error:'host_probe_not_applicable'}}});
  assert.match($('hGw').innerHTML,/Unknown \\(container: host probe not applicable\\)/);
  assert.doesNotMatch($('hGw').innerHTML,/Offline/);
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'offline'}}});
  assert.match($('hGw').innerHTML,/Offline/);
  window._sysBarActive=false;
`);
test('a failed system probe reports the gateway as unknown, not online', `
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'online'}}});
  assert.match($('hGw').innerHTML,/Online/);
  SystemBar.renderGatewayDegraded('timeout');
  assert.doesNotMatch($('hGw').innerHTML,/Online/,'a degraded system probe must not leave the gateway painted Online');
  assert.match($('hGw').innerHTML,/Unknown/);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/Unavailable/);
  window._sysBarActive=false;
`);
testAsync('an unreachable operations endpoint hides the panel and warns once', `
  const warn=console.warn;const seen=[];
  try{
    console.warn=(...a)=>seen.push(a.join(' '));
    $('operationPanel').hidden=false;
    await OperationsPanel.probe();
    await OperationsPanel.probe();
    assert.equal($('operationPanel').hidden,true,'panel must stay hidden when the probe fails');
    assert.equal(seen.length,1,'probe failure must be logged exactly once, got '+seen.length);
    assert.match(seen[0],/operations/i);
  }finally{console.warn=warn;}
`);
test('duration cells are escaped like every other interpolated field', `
  const reconcile=Renderer.reconcileRows;
  try{
    Renderer.reconcileRows=(id,rows,key,render)=>{$(id).innerHTML=rows.map(render).join('');};
    Renderer.render({data:{subagentRuns:[{id:'r',task:'t',agent:'a',status:'ok',timestamp:'x',durationSec:'<img src=x onerror=alert(1)>'}]},tabs:{subRuns:'all'}},{subRuns:true});
    assert.doesNotMatch($('srBody').innerHTML,/<img/,'duration must not emit raw markup');
    assert.match($('srBody').innerHTML,/&lt;img/);
    Renderer.render({data:{crons:[{id:'c',name:'n',schedule:'s',lastDurationMs:1500}]},tabs:{}},{crons:true});
    assert.match($('cronBody').innerHTML,/1.5s/);
  }finally{Renderer.reconcileRows=reconcile;}
`);
test('error feed expand state follows the signature, not the row position', `
  const A={severity:'error',source:'gw',count:1,signature:'sig-A',sampleMessage:'alpha'};
  const B={severity:'warn',source:'gw',count:2,signature:'sig-B',sampleMessage:'beta'};
  ErrorFeed._expanded={};
  ErrorFeed._items=[A,B];ErrorFeed.render();
  ErrorFeed.toggle(0);
  assert.match($('errorBody').innerHTML,/sig-A/);
  ErrorFeed._items=[B,A];ErrorFeed.render();
  const rows=$('errorBody').innerHTML.split('<tr class="dr">');
  assert.match(rows[1],/display:none/,'sig-B must stay collapsed after reorder');
  assert.match(rows[2],/display:table-row/,'sig-A must stay expanded after reorder');
  assert.match(rows[2],/>Hide</,'expanded row button must read Hide');
  assert.match(rows[1],/>View</);
  ErrorFeed._items=[{severity:'warn',source:'gw',count:1,sampleMessage:'no signature here'}];
  ErrorFeed.render();
  assert.doesNotThrow(()=>ErrorFeed.toggle(0));
  ErrorFeed._expanded={};ErrorFeed._items=[];
`);
test('error feed truncates the sample before escaping it', `
  ErrorFeed._expanded={};
  ErrorFeed._items=[{severity:'warn',source:'gw',count:1,signature:'s',sampleMessage:'<'.repeat(130)}];
  ErrorFeed.render();
  const cell=$('errorBody').innerHTML.match(/<td title="[^"]*">([^<]*(?:&lt;)*)<\\/td>/);
  assert.ok(cell,'sample cell not found');
  assert.equal((cell[1].match(/&lt;/g)||[]).length,120,'sample must be truncated to 120 source characters, not 120 escaped bytes');
  assert.doesNotMatch($('errorBody').innerHTML,/&l<\\/td>|&<\\/td>|&lt<\\/td>/,'truncation must not split an HTML entity');
  ErrorFeed._items=[];
`);
test('log polling restarts only when the effective interval changes', `
  try{
    LogTail._timer=null;LogTail._fast=false;LogTail.normalMs=60000;
    timers.setCount=0;timers.clearCount=0;
    LogTail._startPoll();
    assert.equal(timers.setCount,1,'first configuration must start the poll');
    LogTail.applyRuntimeConfig({logRefreshIntervalMs:120000});
    assert.equal(timers.setCount,2,'changed interval must restart the poll');
    assert.equal(timers.clearCount,1);
    LogTail.applyRuntimeConfig({logRefreshIntervalMs:120000});
    LogTail.applyRuntimeConfig({logRefreshIntervalMs:120000});
    assert.equal(timers.setCount,2,'unchanged interval must not restart the poll');
    LogTail._fast=true;
    LogTail._startPoll();
    assert.equal(timers.setCount,3,'fast toggle changes the effective interval');
  }finally{LogTail._fast=false;LogTail.normalMs=60000;}
`);
test('the real runtime health panel renders ready and unavailable collections', `
  State.data={timezone:'UTC'};
  $('runtimeHealthPanelInner').innerHTML='';
  TestedRuntimePanels.renderHealth({collections:{runtimeHealth:{state:'ready',source:'gateway.status',collectedAt:'2026-09-05T09:07:00Z'}},
    runtimeHealth:{eventLoop:{degraded:false,delayP99Ms:12},processMemory:{rssBytes:1048576},degradedPluginsCount:0}});
  const ready=$('runtimeHealthPanelInner').innerHTML;
  assert.match(ready,/ready/);
  assert.match(ready,/Loop delay p99/);
  assert.match(ready,/>12</);
  assert.match(ready,/>No</,'degraded=false must render No, not Not reported');
  TestedRuntimePanels.renderHealth({collections:{runtimeHealth:{state:'unavailable',source:'gateway.status',errorCode:'host_probe_not_applicable'}}});
  const gone=$('runtimeHealthPanelInner').innerHTML;
  assert.match(gone,/unavailable.*host_probe_not_applicable/);
  assert.match(gone,/no successful collection/);
  assert.match(gone,/Not reported/);
  // A collection sourced elsewhere is owned by Renderer, so renderHealth leaves it alone.
  $('runtimeHealthPanelInner').innerHTML='owned by renderer';
  TestedRuntimePanels.renderHealth({collections:{runtimeHealth:{state:'ready',source:'cli'}}});
  assert.equal($('runtimeHealthPanelInner').innerHTML,'owned by renderer');
`);
test('SystemBar creates and tears down the readiness alert across a degraded transition', `
  State.data={timezone:'UTC'};
  window._sysBarActive=false;window._gwOnlineConfirmed=false;
  const base={cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'online'}}};
  // Live but not ready: the alert node is created and inserted.
  SystemBar.render({...base,openclaw:{gateway:{live:true,ready:false,healthEndpointOk:true,failing:['db','queue']}}});
  const alertEl=$('gw-readiness-alert');
  assert.ok(alertEl,'readiness alert must be inserted while the gateway is live but not ready');
  assert.match(alertEl.innerHTML,/Gateway not ready: db, queue/);
  assert.equal(window._gwOnlineConfirmed,true);
  assert.match($('hGw').innerHTML,/Live/);
  // Still not ready: the existing node is reused, not duplicated.
  SystemBar.render({...base,openclaw:{gateway:{live:true,ready:false,healthEndpointOk:true,failing:['db']}}});
  assert.equal($('gw-readiness-alert'),alertEl,'the readiness alert node must be reused');
  assert.equal(alertEl.querySelector('.alert-msg').textContent,'Gateway not ready: db');
  // Ready again: the node is removed.
  SystemBar.render({...base,openclaw:{gateway:{live:true,ready:true,healthEndpointOk:true}}});
  assert.equal($('gw-readiness-alert'),null,'readiness alert must be removed once the gateway is ready');
  assert.match($('hGw').innerHTML,/Online/);
  // A failed probe removes it too and stops claiming the gateway is up.
  SystemBar.render({...base,openclaw:{gateway:{live:true,ready:false,healthEndpointOk:true,failing:[]}}});
  assert.ok($('gw-readiness-alert'));
  SystemBar.renderGatewayDegraded('network error');
  assert.equal($('gw-readiness-alert'),null);
  assert.equal(window._gwOnlineConfirmed,false);
  assert.equal(window._sysBarActive,false);
  assert.match($('hGw').innerHTML,/Unknown/);
`);
test('the real error feed renders counts, badges and escapes sample text', `
  ErrorFeed._expanded={};
  ErrorFeed._items=[];ErrorFeed.render();
  assert.equal($('errorCount').textContent,'0 issues');
  assert.equal($('diagErrorBadge').style.display,'none');
  assert.equal($('errorFeedEmpty').textContent,'No errors detected.');
  ErrorFeed._items=[
    {severity:'error',source:'gateway',count:3,signature:'s1',sampleMessage:'<b>boom</b>',firstSeen:0,lastSeen:0,lastOccurrences:[{timestamp:1,message:'<i>once</i>'}]},
    {severity:'warn',source:'cron',count:1,signature:'s2',sampleMessage:'slow'}];
  ErrorFeed.render();
  const out=$('errorBody').innerHTML;
  assert.equal($('errorCount').textContent,'2 issues');
  assert.equal($('diagErrorBadge').textContent,'⚠ 3 errors');
  assert.equal($('diagErrorBadge').style.display,'inline-block');
  assert.equal($('errorFeedEmpty').style.display,'none');
  assert.match(out,/&lt;b&gt;boom&lt;\\/b&gt;/);
  assert.doesNotMatch(out,/<b>boom<\\/b>/);
  assert.match(out,/&lt;i&gt;once&lt;\\/i&gt;/);
  assert.match(out,/Signature: s1/);
  ErrorFeed._items=[];ErrorFeed._expanded={};
`);
testAsync('fetch failure raises a sticky banner that clears on the next success', `
  assert.match(html,/id="fetchError"[^>]*role="alert"/,'missing dedicated #fetchError element');
  const renderNow=App.renderNow, logFetch=LogTail.fetch, errFetch=ErrorFeed.fetch;
  try{
    App.renderNow=()=>{};LogTail.fetch=async()=>{};ErrorFeed.fetch=async()=>{};
    $('alertsSection').innerHTML='<div class="alert-item">keep me</div>';
    fetchRoutes.set('/api/refresh',()=>{throw new Error('boom')});
    await App.refresh();
    assert.equal($('fetchError').hidden,false,'banner stays hidden after a failed refresh');
    assert.match($('fetchError').textContent,/Failed to load/);
    assert.equal($('alertsSection').innerHTML,'<div class="alert-item">keep me</div>','catch must not overwrite #alertsSection');
    fetchRoutes.set('/api/refresh',()=>({ok:true,status:200,json:async()=>({timezone:'UTC'})}));
    await App.refresh();
    assert.equal($('fetchError').hidden,true,'banner not hidden after a successful refresh');
    assert.equal($('fetchError').textContent,'','banner text not cleared after a successful refresh');
  }finally{App.renderNow=renderNow;LogTail.fetch=logFetch;ErrorFeed.fetch=errFetch;fetchRoutes.delete('/api/refresh');}
`);

test('hidden elements stay hidden regardless of component display rules', `
  assert.match(html,/\\[hidden\\]\\s*\\{[^}]*display:none\\s*!important/,'stylesheet must neutralise component display rules for [hidden]');
`);
test('collection notices fire on the ready to stale transition with byte-identical data', `
  window._sysBarActive=false;
  State.data={timezone:'UTC'};
  const base={timezone:'UTC',crons:[],availableModels:[],dailyChart:[]};
  const readyCollections={crons:{state:'ready'},usageToday:{state:'ready'},usageAll:{state:'ready'},usage30d:{state:'ready'},configuration:{state:'ready'}};
  const ready={...base,collections:readyCollections};
  const staleCollections={crons:{state:'stale',errorCode:'row_limit',collectedAt:'2026-09-05T09:07:00Z'},usageToday:{state:'stale',errorCode:'row_limit',collectedAt:'2026-09-05T09:07:00Z'},usageAll:{state:'ready'},usage30d:{state:'unavailable',errorCode:'configuration_unavailable'},configuration:{state:'unavailable',errorCode:'configuration_unavailable'}};
  const stale={...base,collections:staleCollections};
  const render=payload=>{State.data=payload;const snap={data:payload,tabs:{},chartDays:7};Renderer.render(snap,DirtyChecker.diff(snap));State.prev=payload;State.prevTabs=snap.tabs;State.prevChartDays=snap.chartDays;};
  State.prev=null;State.prevTabs={};State.prevChartDays=7;
  render(ready);
  render(stale);
  for(const id of ['cronNotice','costNotice','chartNotice','modelsNotice'])
    assert.equal($(id).hidden,false,id+' must fire when only the collection state changed');
  render(ready);
  for(const id of ['cronNotice','costNotice','chartNotice','modelsNotice'])
    assert.equal($(id).hidden,true,id+' must clear on the stale to ready transition');
  State.prev=null;State.prevTabs={};
`);
test('collection attempt timestamps alone never dirty a section', `
  const statuses=(attemptedAt,cronState)=>({
    crons:{source:'gw',state:cronState,collectedAt:'c',complete:true,attemptedAt},
    usageToday:{source:'gw',state:'ready',collectedAt:'c',complete:true,attemptedAt},
    usageAll:{source:'gw',state:'ready',collectedAt:'c',complete:true,attemptedAt},
    usage30d:{source:'gw',state:'ready',collectedAt:'c',complete:true,attemptedAt},
    configuration:{source:'gw',state:'ready',collectedAt:'c',complete:true,attemptedAt},
    skillInventory:{source:'gw',state:'ready',collectedAt:'c',complete:true,attemptedAt},
  });
  const base={crons:[],availableModels:[],dailyChart:[],skills:[],skillInventory:[],agentConfig:{},subagentConfig:{},channels:[],costBreakdown:[]};
  const watchers=['cost','crons','charts','models','skills','agentConfig'];
  State.prevTabs={};State.prevChartDays=7;
  State.prev={...base,collections:statuses('2026-09-05T09:07:00Z','ready')};
  State.data={...base,collections:statuses('2026-09-05T09:08:00Z','ready')};
  const idle=DirtyChecker.diff({data:State.data,tabs:{},chartDays:7});
  for(const k of watchers) assert.equal(idle[k],false,k+' must not re-render when only attemptedAt moved');
  State.data={...base,collections:statuses('2026-09-05T09:08:00Z','stale')};
  const changed=DirtyChecker.diff({data:State.data,tabs:{},chartDays:7});
  assert.equal(changed.crons,true,'a crons collection state change must re-render the crons section');
  for(const k of watchers.filter(k=>k!=='crons'))
    assert.equal(changed[k],false,k+' must not re-render for an unrelated collection state change');
  State.prev=null;State.prevTabs={};
`);
test('the cost notice discloses the weakest usage collection, not the first', `
  window._sysBarActive=false;
  State.data={timezone:'UTC'};
  const D={timezone:'UTC',collections:{usageToday:{state:'partial',errorCode:'row_limit'},usageAll:{state:'unavailable',errorCode:'configuration_unavailable'}}};
  Renderer.render({data:D,tabs:{}},{cost:true});
  assert.match($('costNotice').textContent,/unavailable/,'unavailable is weaker than partial whatever the order');
  D.collections={usageToday:{state:'stale',errorCode:'row_limit',collectedAt:'2026-09-05T09:07:00Z'},usageAll:{state:'partial',errorCode:'row_limit'}};
  Renderer.render({data:D,tabs:{}},{cost:true});
  assert.match($('costNotice').textContent,/stale/,'stale is weaker than partial');
`);
test('the gateway runtime card never contradicts the health row', `
  window._sysBarActive=false;
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'unknown',error:'host_probe_not_applicable'}}});
  assert.match($('hGw').innerHTML,/Unknown \\(container: host probe not applicable\\)/);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/Unknown \\(container: host probe not applicable\\)/);
  assert.doesNotMatch($('gatewayRuntimePanelInner').innerHTML,/Offline/,'the card must not say Offline under an Unknown health row');
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'unknown'}}});
  assert.match($('hGw').innerHTML,/Unknown/);
  assert.doesNotMatch($('hGw').innerHTML,/Offline/,'a native unknown status is not Offline');
  assert.doesNotMatch($('gatewayRuntimePanelInner').innerHTML,/Offline/);
  window._sysBarActive=false;
  Renderer.render({data:{gateway:{status:'unknown'}},tabs:{}},{});
  assert.match($('hGw').innerHTML,/Unknown/);
  assert.doesNotMatch($('hGw').innerHTML,/Offline/);
  window._sysBarActive=false;
`);
test('a skipped container host probe renders the reason instead of Live and Ready false', `
  window._sysBarActive=false;window._gwOnlineConfirmed=false;
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'unknown'}},
    openclaw:{gateway:{live:false,ready:false,healthEndpointOk:false,readyEndpointOk:false,reason:'host_probe_not_applicable'}}});
  assert.match($('hGw').innerHTML,/Unknown \\(container: host probe not applicable\\)/);
  assert.doesNotMatch($('hGw').innerHTML,/Offline/);
  assert.doesNotMatch($('hGw').innerHTML,/Live/);
  assert.match($('gatewayRuntimePanelInner').innerHTML,/Unknown \\(container: host probe not applicable\\)/);
  assert.equal($('gw-readiness-alert'),null,'a skipped probe must not raise a readiness alert');
  window._sysBarActive=false;
`);
test('the degraded gateway tooltip is cleared once the probe recovers', `
  SystemBar.renderGatewayDegraded('timeout');
  assert.match($('hGw').title,/timeout/);
  SystemBar.render({cpu:{},ram:{},swap:{},disk:{},versions:{gateway:{status:'online'}}});
  assert.equal($('hGw').title,'','SystemBar must clear the degraded tooltip after recovery');
  SystemBar.renderGatewayDegraded('timeout');
  window._sysBarActive=false;
  Renderer.render({data:{gateway:{status:'online'}},tabs:{}},{});
  assert.equal($('hGw').title,'','Renderer must clear the degraded tooltip after recovery');
  window._sysBarActive=false;
`);

(async () => {
  for (const run of pending) await run();
  process.exitCode = failed ? 1 : 0;
})();
