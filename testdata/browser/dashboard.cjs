// Synthetic data only; no account, workspace, session, or credential records.
module.exports = function fixture() {
  const now=Date.now(), iso=new Date(now).toISOString();
  const collection=source=>({source,state:'ready',complete:true,collectedAt:iso,attemptedAt:iso});
  const model=(name,cost)=>({model:name,modelId:'fixture/'+name,calls:2,input:'80',output:'20',cacheRead:'0',cacheWrite:'0',totalTokens:'100',inputRaw:80,outputRaw:20,cacheReadRaw:0,cacheWriteRaw:0,totalTokensRaw:100,cost,costComplete:true,tokensComplete:true,missingCostEntries:0});
  const usage={timezone:'UTC',scope:'all',tokensComplete:true,costComplete:true,totalTokens:200,knownCost:5,missingCostEntries:0,cacheStatus:{status:'fresh'}};
  const data={schemaVersion:2,botName:'UI Verification',botEmoji:'🧪',timezone:'UTC',lastRefresh:iso,lastRefreshMs:now,runtimeTarget:{mode:'container',container:'fixture'},
    gateway:{status:'unknown',statusReason:'host_probe_not_applicable',port:18789},runtimeInfo:{pid:42,uptimeMs:3600000},compactionMode:'safeguard',
    gatewayHealth:{ok:true,ts:now},providerUsage:{updatedAt:now,providers:[{provider:'fixture',displayName:'Fixture provider',plan:'Test plan',windows:[{label:'5h',usedPercent:0,resetAt:now+3600000}],billing:[{type:'balance',amount:0,unit:'USD'}]}]},
    runtimeHealth:{runtimeVersion:'2026.9.2',processMemory:{rssBytes:104857600},eventLoop:{degraded:false,delayP99Ms:2},taskAudit:{warnings:0,errors:0},degradedPluginsCount:0,degradedSecretOwnersCount:0},
    collections:Object.fromEntries(['channels','configuration','crons','diagnostics','memory','memoryIndex','modelReadiness','pluginInventory','runtimeHealth','runtimeInfo','gatewayHealth','providerUsage','sessions','skillInventory','tasks','usageToday','usage7d','usage30d','usageAll'].map(k=>[k,collection(k==='sessions'?'gateway.sessions.list':k==='runtimeHealth'?'gateway.status':'fixture.'+k)])),
    alerts:[{severity:'low',icon:'ℹ️',message:'Fixture alert <img src=x onerror="window.__xss=1">'}],
    totalCostToday:5,totalCostAllTime:5,projectedMonthly:150,costBreakdown:[{model:'Alpha',cost:3},{model:'Beta',cost:2}],
    dailyChart:Array.from({length:30},(_,i)=>({date:new Date(now-(29-i)*86400000).toISOString().slice(0,10),label:'day-'+i,total:i===29?5:0,tokens:i===29?200:0,calls:i===29?4:0,models:i===29?{Alpha:3,Beta:2}:{},subagentRuns:1,subagentCost:0})),
    sessionCount:60,sessionTotal:60,sessions:Array.from({length:60},(_,i)=>({key:'agent:main:fixture-'+i,name:'Session '+i,agent:'main',model:'Alpha',modelId:'fixture/alpha',type:i===1?'subagent':'main',active:i<2,activeRunIds:i<2?['run-'+i]:[],spawnedBy:i===1?'agent:main:fixture-0':'',contextPct:25,contextTokens:400,totalTokens:100,tokenState:'fresh',updatedAt:now,placement:{state:'foreground'},visibility:'shared',goal:i===0?{objective:'Verify UI',status:'active',tokensUsed:10,tokenBudget:100}:undefined})),
    crons:[{id:'job-1',name:'Fixture job',enabled:true,status:'ok',payloadKind:'agentTurn',sessionTarget:'isolated',schedule:'Every hour',timezone:'UTC',lastRun:'1m ago',nextRun:'in 59m',lastDurationMs:1000,model:'Alpha',configRevision:'fixture-revision'},{id:'job-2',name:'Disabled job',enabled:false,status:'disabled',payloadKind:'heartbeat',sessionTarget:'main',schedule:'Every 30m'}],
    tasks:Array.from({length:60},(_,i)=>({id:'task-'+i,task:'Task '+i,agent:'main',runtime:i%2?'cron':'subagent',status:i%2?'succeeded':'running',ownerKey:'agent:main:fixture-0',runId:'run-'+i,progressSummary:'Step '+i,deliveryStatus:'delivered',durationSec:i})),
    subagentRuns:[{id:'sub-1',task:'Fixture subtask',agent:'main',durationSec:5,status:'succeeded',timestamp:iso}],
    memory:[{agentId:'main',provider:'fixture',embedding:{checked:true,ok:true},dreaming:{enabled:true,shortTermCount:0,promotedToday:0,phases:{deep:{nextRunAtMs:now+3600000}}}}],
    memoryIndex:[{agentId:'main',status:{model:'embedding-fixture',files:0,chunks:0,dirty:false}}],
    channels:[{channel:'test',accountId:'default',enabled:true,configured:true,running:true,connected:true,healthState:'healthy',probe:{ok:true},hasError:false}],
    pluginInventory:[{id:'workboard',version:'1.0',origin:'bundled',enabled:true,status:'loaded',toolNames:['board.list']}],
    skillInventory:[{agentId:'main',name:'Fixture skill',eligible:true,modelVisible:true,disabled:false,missing:{}},{agentId:'main',name:'Missing dependency',eligible:false,modelVisible:false,disabled:false,missing:{bins:['fixture-tool']}}],
    modelReadiness:[{agentId:'main',modelId:'fixture/alpha',allowed:true,default:true,catalogName:'Alpha',authKind:'api-key',missingProviderInUse:false}],
    diagnostics:{backup:{enabled:true,lastAttemptAt:iso,lastSuccessAt:iso}},
    availableModels:[{name:'Alpha',provider:'fixture',status:'active'},{name:'Beta',provider:'fixture',status:'available'}],
    gitLog:[{hash:'abc123',message:'Fixture change',ago:'1h ago'}],
    agentConfig:{agents:[{id:'main',role:'Primary',model:'Alpha',modelId:'fixture/alpha',workspace:'/fixture/workspace',fallbacks:[],context1m:false,isDefault:true}],primaryModel:'fixture/alpha',fallbacks:[],imageModel:'fixture/beta',streamMode:'off',compaction:{mode:'safeguard',reserveTokensFloor:0,memoryFlush:{enabled:false},softThresholdTokens:0},telegramDmPolicy:'allowlist',telegramGroups:0,channels:['test'],search:{provider:'fixture',maxResults:0,cacheTtlMinutes:0},gateway:{port:18789,mode:'local',bind:'loopback',authMode:'token',tailscale:'off'},hooks:[{name:'fixture-hook',enabled:true}],plugins:[{name:'workboard',enabled:true}],skills:[{name:'fixture-skill',enabled:true}],tts:false,diagnostics:true,bindings:[{agentId:'main',kind:'default'}],subagentConfig:{maxSpawnDepth:0,maxChildrenPerAgent:0,maxConcurrent:0},backupConfig:{enabled:true},diagnosticConfig:{enabled:true,otel:{enabled:false}},modelPricing:{}},
    logConfig:{logTailLines:250,logFastRefreshMs:3000,logRefreshIntervalMs:15000}};
  for(const suffix of ['Today','7d','30d','All']){data['usage'+suffix]={...usage};data[suffix==='All'?'tokenUsage':'tokenUsage'+suffix]=[model('Alpha',3),model('Beta',2)];}
  for(const suffix of ['Today','7d','30d'])data['subagentRuns'+suffix]=data.subagentRuns;
  const system={cpu:{percent:20},ram:{totalBytes:1073741824,usedBytes:536870912,percent:50},swap:{percent:0,totalBytes:0,usedBytes:0},disk:{totalBytes:10737418240,usedBytes:5368709120,percent:50},versions:{openclaw:'2026.9.2',gateway:{status:'unknown',statusReason:'host_probe_not_applicable',version:'2026.9.2'}},openclaw:{gateway:{reason:'host_probe_not_applicable'},status:{currentVersion:'2026.9.2'}},pollSeconds:60};
  const logs={entries:['gateway','cron','session','subagent'].flatMap(source=>['info','warn','error'].map(severity=>({source,severity,timestamp:now,message:source+' '+severity+' fixture <img src=x onerror="window.__xss=1">'}))),collectionSource:'fixture',truncated:false};
  const errors={items:[{source:'gateway',severity:'error',count:3,signature:'fixture-error',sampleMessage:'Fixture error <img src=x>',firstSeen:now,lastSeen:now,samples:[{message:'Sample error',timestamp:now}]}]};
  return {data,system,logs,errors};
};
