// Optional real-browser suite; no browser dependency is added to the application.
// PLAYWRIGHT_MODULE=/path/to/playwright DASHBOARD_TEST_URL=http://127.0.0.1:8082 node scripts/ui-browser-smoke.cjs
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const fixture=require('../testdata/browser/dashboard.cjs');
const base=process.env.DASHBOARD_TEST_URL||'http://127.0.0.1:8082';
const output=path.resolve(process.env.UI_TEST_OUTPUT||'dist/ui-browser-results');fs.mkdirSync(output,{recursive:true});
const results=[];let page;
const test=async(name,fn)=>{try{await fn();results.push({name,status:'passed'});console.log('PASS',name);}catch(e){results.push({name,status:'failed',error:e.message});console.error('FAIL',name,e.message);}};
const text=id=>page.locator('#'+id).innerText();
const click=id=>page.locator('#'+id).click();
const waitText=(id,part)=>page.waitForFunction(([id,part])=>document.getElementById(id).textContent.includes(part),[id,part]);
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.BROWSER_CHANNEL||'chrome'});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  page=await context.newPage();page.setDefaultTimeout(7000);
  const f=fixture(),pageErrors=[],requests=[],unhandled=[];let mode='rich',chatAvailable=true,operationOutcome='succeeded';
  page.on('pageerror',e=>pageErrors.push(e.message));
  await page.route('**/api/**',async route=>{
   const req=route.request(),url=new URL(req.url());requests.push({path:url.pathname,method:req.method(),query:url.search,body:req.postData()});
   let body,status=200;
   switch(url.pathname){
    case '/api/refresh':if(mode==='outage'){status=503;body={error:'fixture outage'};}else body=f.data;break;
    case '/api/system':if(mode==='outage'){status=503;body={error:'fixture outage'};}else body=f.system;break;
    case '/api/logs':body=f.logs;break;
    case '/api/errors':body=f.errors;break;
    case '/api/chat/status':body={available:chatAvailable,state:chatAvailable?'configured':'credentials_missing',inferenceVerified:false};break;
    case '/api/chat':body={answer:'Fixture reply <script>window.__xss=1</script>'};break;
    case '/api/operations/status':body={enabled:true};break;
    case '/api/operations':body={outcome:operationOutcome};break;
    case '/api/automation/runs':body={entries:[{runAtMs:Date.now(),status:'ok',completionStatus:'succeeded',deliveryStatus:'delivered',durationMs:10,sessionKey:'agent:main:fixture-0',diagnostics:['fixture history']}],total:51,hasMore:!url.searchParams.get('offset')||url.searchParams.get('offset')==='0',nextOffset:50};break;
    case '/api/session/context':body={progressStatus:{state:'ready'},boardStatus:{state:'ready'},progress:{markdown:'Fixture progress',steps:[{step:'Verify',status:'done'}]},widgets:[{title:'Fixture widget',contentKind:'markdown',grantState:'allowed',revision:1}]};break;
    case '/api/workboard':body={state:'ready',boards:[{id:'board-1',name:'Fixture board',total:2,active:1,archived:1,byStatus:{active:1}}]};break;
    default:unhandled.push(url.pathname);status=500;body={error:'Unmocked endpoint'};
   }
   await route.fulfill({status,contentType:'application/json',body:JSON.stringify(body)});
  });
  const load=async()=>{await page.goto(base,{waitUntil:'domcontentloaded'});await waitText('botName','UI Verification');await click('oc-expand-all');};
  await load();
  await test('page identity, all twelve sections and populated diagnostics',async()=>{
   assert.equal(await page.title(),'UI Verification');assert.equal(await page.locator('.oc-section').count(),12);
   for(const id of ['healthRow','costNotice','donutLegend','costChart','modelChart','cronBody','sessBody','uBody','srBody','taskLedger','sessionWork','memoryHealth','memoryIndexHealth','channelAccounts','modelsGrid','agentCards','runtimeConfigPanel','modelRoutingPanel','channelConfigPanel','gatewayConfigPanelInner','subagentConfigPanel','capsPanelInner','hooksPanelInner','searchPanelInner','bindingsPanel','agentTableBody','skillsGrid','gitPanel']){
    if(id==='costNotice')continue;assert.ok((await text(id)).trim()||await page.locator('#'+id+' svg').count(),id+' empty');
   }
   assert.match(await text('cToday'),/\$5\.00/);assert.match(await text('hPid'),/42/);
  });
  await test('every section toggle, expand/collapse-all and persisted state',async()=>{
   const keys=await page.locator('.oc-section').evaluateAll(nodes=>nodes.map(e=>e.dataset.section));
   for(const key of keys){const toggle=page.locator(`[data-section="${key}"] .oc-section-toggle`);await toggle.click();assert.equal(await toggle.getAttribute('aria-expanded'),'false');await toggle.click();assert.equal(await toggle.getAttribute('aria-expanded'),'true');}
   await click('oc-collapse-all');assert.equal(await page.locator('.oc-section-toggle[aria-expanded="true"]').count(),0);
   await page.reload();await waitText('botName','UI Verification');assert.equal(await page.locator('.oc-section-toggle[aria-expanded="true"]').count(),0);await click('oc-expand-all');
  });
  await test('all themes, persistence and desktop/mobile layouts',async()=>{
   await click('themeBtn');const ids=await page.locator('[data-theme-id]').evaluateAll(nodes=>nodes.map(e=>e.dataset.themeId));assert.ok(ids.length>=6);
   for(const id of ids){if(!(await page.locator('#themeMenu').getAttribute('class')).includes('open'))await click('themeBtn');await page.locator(`[data-theme-id="${id}"]`).click();assert.equal(await page.evaluate(()=>localStorage.getItem('ocDashTheme')),id);}
   const theme=await page.evaluate(()=>localStorage.getItem('ocDashTheme'));await page.reload();await waitText('botName','UI Verification');assert.equal(await page.evaluate(()=>localStorage.getItem('ocDashTheme')),theme);
   for(const width of [1440,768,390]){await page.setViewportSize({width,height:1000});await page.locator('[data-section="health"]').screenshot({path:path.join(output,`health-${width}.png`)});const overflow=await page.evaluate(()=>[...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().width>innerWidth).map(e=>({tag:e.tagName,id:e.id,cls:e.className,width:e.getBoundingClientRect().width,scroll:getComputedStyle(e).overflowX})));fs.writeFileSync(path.join(output,'overflow-'+width+'.json'),JSON.stringify(overflow,null,2));await page.screenshot({path:path.join(output,'layout-'+width+'.png'),fullPage:true});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'page overflows at '+width);}
   await page.setViewportSize({width:1440,height:1000});
  });
  await test('chart ranges and all four token/subagent ranges',async()=>{
   for(const id of ['cTab30','cTab7']){await click(id);await page.waitForFunction(id=>document.getElementById(id).classList.contains('active'),id);assert.ok(await page.locator('#modelChart svg').count());}
   for(const prefix of ['uTab','srTab'])for(const suffix of ['7','30','A','T']){await click(prefix+suffix);await page.waitForFunction(id=>document.getElementById(id).className.includes('active'),prefix+suffix);}
  });
  await test('session pagination and scroll retention',async()=>{
   await click('sessionNext');await waitText('sessionPage','2');assert.match(await text('sessBody'),/Session 59/);assert.equal(await page.locator('#sessionNext').isDisabled(),true);await click('sessionPrev');await waitText('sessionPage','1');
   const scroll=page.locator('#sessBody').locator('xpath=ancestor::div[contains(@style,"overflow")][1]');await scroll.evaluate(e=>e.scrollTop=120);const before=await scroll.evaluate(e=>e.scrollTop);
   f.data.sessions[0].totalTokens=101;await page.locator('.header-right .refresh-btn').click();await waitText('sessBody','101');assert.equal(await scroll.evaluate(e=>e.scrollTop),before);
  });
  await test('all task filters, search, pagination and empty results',async()=>{
   await click('taskNext');await waitText('taskPage','Page 2');await click('taskPrev');
   for(const id of ['taskStatus','taskRuntime']){const values=await page.locator('#'+id+' option').evaluateAll(nodes=>nodes.map(e=>e.value));for(const value of values){await page.selectOption('#'+id,value);assert.match(await text('taskPage'),/Page 1/);}await page.selectOption('#'+id,'all');}
   await page.fill('#taskSearch','Task 59');assert.match(await text('taskLedger'),/Task 59/);await page.fill('#taskSearch','no-match');assert.match(await text('taskLedger'),/No rows/);await page.fill('#taskSearch','');
  });
  await test('automation history pagination and modal close',async()=>{
   await page.locator('#cronBody [data-automation-id]').first().click();await waitText('runtimeDetailBody','recorded runs');await page.locator('#runtimeDetailBody [data-run-offset="50"]').click();await waitText('runtimeDetailBody','recorded runs');assert.ok(await page.locator('#runtimeDetailBody [data-run-offset="0"]').count());await page.locator('[data-close-runtime]').click();assert.equal(await page.locator('#runtimeDetail').isVisible(),false);
  });
  await test('session links, progress/widgets and Workboard details',async()=>{
   await page.selectOption('#workSessionSelect','agent:main:fixture-1');assert.match(await page.locator('#nativeSession').getAttribute('href'),/fixture-1/);
   await page.locator('[data-open-work]').click();await waitText('runtimeDetailBody','Fixture widget');assert.match(await text('runtimeDetailBody'),/Fixture progress/);await page.locator('[data-close-runtime]').click();
   await click('workboardSummary');await waitText('runtimeDetailBody','Fixture board');await page.locator('[data-close-runtime]').click();
   for(const id of ['nativeControl','nativeSession','nativeChat'])assert.match(await page.locator('#'+id).getAttribute('href'),/^http:\/\/127\.0\.0\.1:18789\//);
  });
  await test('every runtime disclosure exposes populated fields',async()=>{
   const details=page.locator('#oc-body-runtime > details');for(let i=0;i<await details.count();i++){const d=details.nth(i);await d.locator('summary').click();assert.equal(await d.getAttribute('open'),'');assert.ok((await d.innerText()).trim().length>20);await d.locator('summary').click();}
  });
  await test('log sources/severities, regex validation, pause and speed',async()=>{
   for(const id of ['logSourceCron','logSourceSession','logSourceSubagent'])assert.equal(await page.locator('#'+id).isDisabled(),true);
   for(const id of ['logSourceGateway','logSourceAll','logSevInfo','logSevWarn','logSevError','logSevAll']){await click(id);await page.waitForFunction(id=>document.getElementById(id).classList.contains('active'),id);}
   await page.fill('#logRegexFilter','fixture');await page.waitForFunction(()=>document.querySelector('#logTail mark'));await page.fill('#logRegexFilter','[');await waitText('logStatus','Invalid regex');await page.fill('#logRegexFilter','');
   await click('logPauseBtn');assert.match(await text('logPauseBtn'),/Resume/);await click('logPauseBtn');assert.match(await text('logPauseBtn'),/Pause/);await click('logFastBtn');assert.match(await text('logFastBtn'),/Normal/);await click('logFastBtn');
  });
  await test('error sort, occurrence expansion and diagnostic badge',async()=>{
   await click('errSortLast');await waitText('errorBody','Fixture error');await click('errSortCount');await page.locator('#errorBody button').first().click();assert.match(await text('errorBody'),/Signature: fixture-error/);await page.locator('#errorBody button').first().click();await page.locator('[data-section="errors"] .oc-section-toggle').click();await click('diagErrorBadge');assert.equal(await page.locator('[data-section="errors"] .oc-section-toggle').getAttribute('aria-expanded'),'true');
  });
  await test('chat input, keyboard, chips, escaping and unavailable capability',async()=>{
   await click('chatBtn');await page.waitForFunction(()=>!document.getElementById('chatInput').disabled);await page.fill('#chatInput','Fixture question');await page.locator('#chatInput').press('Enter');await waitText('chatMessages','Fixture reply');
   for(const chip of await page.locator('.chat-chip').all()){await chip.click();await page.waitForFunction(()=>!document.getElementById('chatSend').disabled);}
   chatAvailable=false;await page.locator('.chat-close').click();await click('chatBtn');await page.waitForFunction(()=>document.getElementById('chatInput').disabled);assert.match(await text('chatAvailability'),/credentials_missing/);await page.locator('.chat-close').click();
  });
  await test('operator validation, cancellation, each confirmed action and token clearing',async()=>{
   await page.locator('#operationPanel summary').click();await click('operationSubmit');await waitText('operationResult','token is required');
   await page.fill('#operatorToken','fixture-token');const before=requests.filter(r=>r.path==='/api/operations').length;page.once('dialog',d=>d.dismiss());await click('operationSubmit');assert.equal(requests.filter(r=>r.path==='/api/operations').length,before);
   for(const action of ['automation.enable','automation.disable','automation.run','session.abort']){await page.selectOption('#operationAction',action);await page.fill('#operatorToken','fixture-token');page.once('dialog',d=>d.accept());await click('operationSubmit');await waitText('operationResult','succeeded');await page.waitForFunction(()=>document.getElementById('operatorToken').value==='');const sent=JSON.parse(requests.filter(r=>r.path==='/api/operations').at(-1).body);assert.equal(sent.action,action);if(action==='session.abort')assert.ok(sent.runId&&sent.sessionKey);else assert.equal(sent.configRevision,'fixture-revision');}
   operationOutcome='failed';await page.fill('#operatorToken','fixture-token');page.once('dialog',d=>d.accept());await click('operationSubmit');await waitText('operationResult','failed');await page.locator('#operationPanel summary').click();
  });
  await test('zero-valued configuration and absent optional fields',async()=>{
   assert.match(await text('runtimeConfigPanel'),/Reserve Tokens\s*0/);assert.match(await text('runtimeConfigPanel'),/0 configured/);assert.match(await text('searchPanelInner'),/Max Results\s*0/);assert.match(await text('searchPanelInner'),/Cache TTL\s*0 min/);
   f.data.agentConfig.gateway={port:18789};f.data.agentConfig.agents[0].context1m=undefined;await page.locator('.header-right .refresh-btn').click();await waitText('gatewayConfigPanelInner','Not reported');assert.doesNotMatch(await text('gatewayConfigPanelInner'),/undefined|null/);assert.match(await text('agentTableBody'),/None configured/);assert.match(await text('agentTableBody'),/Not reported/);
  });
  await test('legacy log source controls',async()=>{
   f.data.collections.sessions.source='legacy.session.files';await page.locator('.header-right .refresh-btn').click();await page.waitForFunction(()=>!document.getElementById('logSourceCron').disabled);
   for(const id of ['logSourceCron','logSourceSession','logSourceSubagent','logSourceAll']){await click(id);await page.waitForFunction(id=>document.getElementById(id).classList.contains('active'),id);}
  });
  await test('empty tables and missing session target have explicit states',async()=>{
   f.data.sessions=[];f.data.sessionTotal=0;f.data.crons=[];f.data.tokenUsageToday=[];f.data.tasks=[];await page.locator('.header-right .refresh-btn').click();await page.waitForFunction(()=>document.getElementById('sessCount').textContent.startsWith('0 total'));
   assert.match(await text('sessBody'),/No sessions reported/);assert.match(await text('cronBody'),/No automations reported/);assert.match(await text('uBody'),/No usage reported/);assert.equal(await page.locator('[data-open-work]').isDisabled(),true);assert.equal(await page.locator('#workSessionSelect').isDisabled(),true);
  });
  await test('refresh outage and recovery are visible',async()=>{
   mode='outage';await page.locator('.header-right .refresh-btn').click();await page.waitForFunction(()=>!document.getElementById('fetchError').hidden);mode='rich';await page.locator('.header-right .refresh-btn').click();await page.waitForFunction(()=>document.getElementById('fetchError').hidden);
  });
  await test('no script errors, injection, or unhandled API requests',async()=>{assert.deepEqual(pageErrors,[]);assert.deepEqual(unhandled,[]);assert.equal(await page.evaluate(()=>window.__xss),undefined);assert.equal(await page.locator('img[src="x"]').count(),0);});
  await page.screenshot({path:path.join(output,'final-desktop.png'),fullPage:true});
  fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({results,requests,pageErrors,unhandled},null,2));
 } finally{await browser.close();}
 if(results.some(r=>r.status==='failed'))process.exitCode=1;
})().catch(e=>{console.error(e);process.exitCode=1;});
