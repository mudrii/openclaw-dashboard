// Local synthetic dashboard for hands-on browser checks. Never contacts OpenClaw.
// node scripts/ui-fixture-server.cjs (http://127.0.0.1:8083)
const http=require('node:http'),fs=require('node:fs'),path=require('node:path');
const fixture=require('../testdata/browser/dashboard.cjs');
const root=path.resolve(__dirname,'..');
let f=fixture(),mode='rich',chatAvailable=true,operationOutcome='succeeded';
http.createServer((req,res)=>{
 const url=new URL(req.url,'http://127.0.0.1:8083');
 if(url.pathname==='/'){
  mode=url.searchParams.get('scenario')||'rich';f=fixture();
  if(mode==='empty'){f.data.sessions=[];f.data.sessionTotal=0;f.data.sessionCount=0;f.data.crons=[];f.data.tasks=[];for(const key of ['tokenUsageToday','tokenUsage7d','tokenUsage30d','tokenUsage'])f.data[key]=[];}
  res.writeHead(200,{'Content-Type':'text/html'});res.end(fs.readFileSync(path.join(root,'web/index.html'),'utf8').replaceAll('__RUNTIME__','Fixture').replaceAll('__VERSION__','synthetic'));return;
 }
 if(url.pathname==='/themes.json'){res.writeHead(200,{'Content-Type':'application/json'});res.end(fs.readFileSync(path.join(root,'assets/runtime/themes.json')));return;}
 if(!url.pathname.startsWith('/api/')){res.writeHead(404);res.end();return;}
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
    default:status=500;body={error:'Unmocked endpoint'};
   }
 res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(body));
}).listen(8083,'127.0.0.1',()=>console.log('Synthetic dashboard: http://127.0.0.1:8083'));
