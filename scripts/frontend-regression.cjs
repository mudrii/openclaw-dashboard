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
const $ = id => {
  if (!nodes.has(id)) nodes.set(id, {innerHTML:'', textContent:'', style:{}, value:'all', replaceChildren(){this.innerHTML='';}});
  return nodes.get(id);
};
const context = vm.createContext({$, console, URL, URLSearchParams, AbortSignal, setTimeout, clearTimeout,
  window:{},RuntimePanels:{render(){}},
  COLORS:['red'], esc:s=>String(s??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;'),
  document:{addEventListener(){},querySelectorAll(){return [];}},
});
vm.runInContext(section('function relTime(', '// === Theme ===') +
  section('const State =', '// === DataLayer ===') +
  section('const LogTail =', 'const ErrorFeed =') +
  section('const DirtyChecker =', 'const RuntimePanels =') +
  section('const Renderer =', '// === Chat ==='), context);
let failed = 0;
function test(name, code) {
  try { vm.runInContext('{'+code+'}', context); console.log('PASS', name); }
  catch (e) { failed++; console.error('FAIL', name, e.message); }
}
context.assert = assert;
context.html = html;
vm.runInContext(section('const RuntimePanels =',"fetch('/api/operations/status')").replace('const RuntimePanels =','const TestedRuntimePanels ='),context);
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
  const link=TestedRuntimePanels.linkSession,tasks=TestedRuntimePanels.renderTasks,health=TestedRuntimePanels.renderHealth;
  try{
    TestedRuntimePanels.linkSession=()=>{};TestedRuntimePanels.renderTasks=()=>{};TestedRuntimePanels.renderHealth=()=>{};
    TestedRuntimePanels.render({sessions:[{name:'idle',active:false}],memory:[{embedding:{checked:false,ok:false}}],channels:[{channel:'telegram',probe:{},connected:true}],pluginInventory:[{id:'bundled'}]});
    assert.match($('sessionWork').innerHTML,/Not applicable/);
    assert.match($('memoryHealth').innerHTML,/Not checked/);
    assert.match($('channelAccounts').innerHTML,/Not probed/);
    assert.match($('pluginInventory').innerHTML,/Not reported/);
  }finally{TestedRuntimePanels.linkSession=link;TestedRuntimePanels.renderTasks=tasks;TestedRuntimePanels.renderHealth=health;}
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
  const reconcile=Renderer.reconcileRows,tree=Renderer.renderAgentTree,link=TestedRuntimePanels.linkSession,health=TestedRuntimePanels.renderHealth;
  try{
    Renderer.reconcileRows=(id,rows,key,render)=>{$(id).innerHTML=rows.map(render).join('');};Renderer.renderAgentTree=()=>{};
    TestedRuntimePanels.linkSession=()=>{};TestedRuntimePanels.renderHealth=()=>{};
    $('sessBody').closest=()=>null;RuntimePanels.pageSize=50;RuntimePanels.sessionPage=0;
    const data={sessions:[{name:'automation',tokenState:'stale',contextState:'tokens_stale',totalTokens:null,contextPct:null}],agentConfig:{backupConfig:{enabled:false},diagnosticConfig:{enabled:false,otel:{enabled:false}},modelPricing:{'p/m':{input:1,output:2,cacheRead:0}}},usageAll:{tokensComplete:true,missingCostEntries:2,missingCostByModel:{'p/m':2}},usageToday:{tokensComplete:true,missingCostEntries:2},diagnostics:{backup:{latestSuccess:{completedAt:'2026-09-06T00:00:00Z'}}}};
    Renderer.render({data,tabs:{}},{sessions:true,cost:true});TestedRuntimePanels.render(data);
    assert.match($('sessBody').innerHTML,/Stale token count/);assert.doesNotMatch($('sessBody').innerHTML,/Unknown/);
    assert.equal($('cAll').textContent,'Pricing missing');
    assert.match($('pricingCoverage').innerHTML,/p\\/m/);assert.match($('pricingCoverage').innerHTML,/\\$0.000000/);
    assert.match($('backupHealth').innerHTML,/explicit config.*No/);assert.match($('backupHealth').innerHTML,/06\\/09\\/2026/);
    assert.doesNotMatch($('backupHealth').innerHTML,/Unknown/);
  }finally{Renderer.reconcileRows=reconcile;Renderer.renderAgentTree=tree;TestedRuntimePanels.linkSession=link;TestedRuntimePanels.renderHealth=health;}
`);
process.exitCode = failed ? 1 : 0;
