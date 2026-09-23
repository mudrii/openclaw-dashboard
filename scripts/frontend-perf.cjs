// Frontend performance, soak and A/B equivalence harness for web/index.html.
// Runs the real embedded script in a node:vm context against a stub DOM, a
// virtual clock and routed fetch stubs. Node built-ins only.
//
//   node scripts/frontend-perf.cjs                 # benchmark web/index.html
//   node scripts/frontend-perf.cjs --rev f1b420c   # benchmark a git revision
//   node scripts/frontend-perf.cjs --equiv f1b420c # DOM equivalence: rev vs working tree
//   node --expose-gc scripts/frontend-perf.cjs --soak [--hours 6]
//   node scripts/frontend-perf.cjs --iterations 50 --json
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const {execFileSync} = require('node:child_process');
const {performance} = require('node:perf_hooks');
const largeFixture = require('../testdata/browser/large.cjs');

const root = path.resolve(__dirname, '..');
const args = process.argv.slice(2);
const flag = name => args.includes('--' + name);
const opt = (name, def) => { const i = args.indexOf('--' + name); return i >= 0 ? args[i + 1] : def; };

function loadHtml(rev) {
  if (!rev) return fs.readFileSync(path.join(root, 'web/index.html'), 'utf8');
  return execFileSync('git', ['show', rev + ':web/index.html'], {cwd: root, encoding: 'utf8', maxBuffer: 1 << 26});
}

function appScript(html) {
  const start = html.indexOf('<script>\n// === Utilities ===');
  const end = html.indexOf('</script>', start);
  assert.ok(start >= 0 && end > start, 'application script not found');
  return html.slice(start + '<script>'.length, end);
}

// createEnv boots the whole application script. All sources of nondeterminism
// (clock, timers, fetch, animation frames) are owned by the harness.
function createEnv(html, fixture) {
  const clock = {now: fixture.now};
  const stats = {innerHTMLWrites: 0, innerHTMLBytes: 0, listeners: new Map(), intervalsSet: 0, intervalsCleared: 0, timeoutsSet: 0, timeoutsCleared: 0, fetches: new Map()};
  const timers = new Map();
  let nextTimer = 1;
  const nodes = new Map();
  const absent = new Set(['gw-readiness-alert']);
  const defaults = {taskStatus: 'all', taskRuntime: 'all'};
  const countListener = (target, type) => { const k = target + ':' + type; stats.listeners.set(k, (stats.listeners.get(k) || 0) + 1); };
  const listeners = [];
  function makeNode(id, tag) {
    let html = '';
    const classes = new Set();
    const node = {
      id, tagName: (tag || 'div').toUpperCase(), title: '', textContent: '', hidden: false, disabled: false, href: '', open: false,
      value: defaults[id] || '', dataset: {}, children: [], scrollTop: 0, scrollHeight: 0, clientHeight: 0,
      style: {setProperty() {}, removeProperty() {}},
      get className() { return [...classes].join(' '); },
      set className(v) { classes.clear(); for (const c of String(v).split(/\s+/)) if (c) classes.add(c); },
      classList: {add: c => classes.add(c), remove: c => classes.delete(c), contains: c => classes.has(c), toggle: c => (classes.has(c) ? (classes.delete(c), false) : (classes.add(c), true))},
      get innerHTML() { return html; },
      set innerHTML(v) { html = String(v); this.children = []; stats.innerHTMLWrites++; stats.innerHTMLBytes += html.length; },
      replaceChildren() { html = ''; this.children = []; },
      append(...kids) { for (const c of kids) { this.children.push(c); html += c == null ? '' : (c.innerHTML || c.textContent || ''); } },
      appendChild(c) { this.append(c); return c; },
      querySelector(sel) { return this.children.find(c => c.classList && c.classList.contains(sel.replace(/^\./, ''))) || null; },
      querySelectorAll() { return []; },
      insertAdjacentElement(_pos, el) { if (el && el.id) { absent.delete(el.id); nodes.set(el.id, el); } },
      addEventListener(type, fn) { countListener('#' + (id || tag), type); listeners.push({target: id, type, fn}); },
      removeEventListener() {},
      remove() { if (this.id) { absent.add(this.id); nodes.delete(this.id); } },
      closest() { return null; }, setAttribute() {}, removeAttribute() {}, focus() {}, scrollIntoView() {}, showModal() { this.open = true; }, close() { this.open = false; },
    };
    return node;
  }
  const $ = id => {
    if (absent.has(id)) return null;
    if (!nodes.has(id)) nodes.set(id, makeNode(id));
    return nodes.get(id);
  };
  const routes = {
    '/api/refresh': () => fixture.refreshJSON(),
    '/api/system': () => fixture.systemJSON(),
    '/api/logs': () => fixture.logsJSON(),
    '/api/errors': () => fixture.errorsJSON(),
    '/api/operations/status': () => '{"enabled":true}',
    '/themes.json': () => '{}',
  };
  const storage = new Map();
  const document = {
    hidden: false, title: '',
    documentElement: {style: {setProperty() {}}},
    addEventListener(type, fn) { countListener('document', type); listeners.push({target: 'document', type, fn}); },
    removeEventListener() {},
    querySelector() { return null; }, querySelectorAll() { return []; },
    getElementById: $, createElement: tag => makeNode('', tag),
  };
  const ctx = vm.createContext({
    console: {log() {}, warn() {}, error: (...a) => console.error('[app]', ...a)},
    URL, URLSearchParams, AbortController, AbortSignal, structuredClone, performance,
    crypto: {randomUUID: () => 'uuid'}, confirm: () => false,
    localStorage: {getItem: k => storage.get(k) ?? null, setItem: (k, v) => storage.set(k, String(v)), removeItem: k => storage.delete(k)},
    document,
    requestAnimationFrame: fn => { fn(); return 0; },
    setInterval(fn, ms) { stats.intervalsSet++; const id = nextTimer++; timers.set(id, {fn, ms: Math.max(1, ms | 0), due: clock.now + Math.max(1, ms | 0), repeat: true}); return id; },
    clearInterval(id) { if (timers.delete(id)) stats.intervalsCleared++; },
    setTimeout(fn, ms) { stats.timeoutsSet++; const id = nextTimer++; timers.set(id, {fn, ms: ms | 0, due: clock.now + (ms | 0), repeat: false}); return id; },
    clearTimeout(id) { if (timers.delete(id)) stats.timeoutsCleared++; },
    async fetch(url) {
      const u = new URL(String(url), 'http://127.0.0.1');
      stats.fetches.set(u.pathname, (stats.fetches.get(u.pathname) || 0) + 1);
      const route = routes[u.pathname];
      if (!route) return {ok: false, status: 404, json: async () => ({error: 'not found'})};
      const body = route(u);
      return {ok: true, status: 200, json: async () => JSON.parse(body)};
    },
  });
  ctx.window = ctx;
  ctx.globalThis = ctx;
  ctx.__clock = clock;
  vm.runInContext(`(()=>{const RD=Date,clock=__clock;class D extends RD{constructor(...a){if(a.length===0)super(clock.now);else super(...a);}static now(){return clock.now;}}globalThis.Date=D;})()`, ctx);
  vm.runInContext(appScript(html), ctx, {filename: 'index.html.js'});
  const flush = () => new Promise(resolve => setImmediate(resolve));
  const env = {
    ctx, nodes, stats, clock, timers, listeners, flush,
    run: code => vm.runInContext(code, ctx),
    async advance(ms) {
      const target = clock.now + ms;
      for (;;) {
        let next = null;
        for (const [id, t] of timers) if (t.due <= target && (!next || t.due < next[1].due)) next = [id, t];
        if (!next) break;
        const [id, t] = next;
        clock.now = Math.max(clock.now, t.due);
        if (t.repeat) t.due += t.ms; else timers.delete(id);
        t.fn();
        await flush();
      }
      clock.now = target;
      await flush();
    },
    async setHidden(hidden) {
      document.hidden = hidden;
      for (const l of listeners.filter(l => l.target === 'document' && l.type === 'visibilitychange')) l.fn();
      await flush();
    },
    // domState captures everything the stub DOM can observe, for A/B comparison.
    domState() {
      const out = {};
      for (const [id, n] of [...nodes].sort(([a], [b]) => a < b ? -1 : 1)) {
        out[id] = {innerHTML: n.innerHTML, textContent: String(n.textContent), className: n.className, hidden: n.hidden, disabled: n.disabled, value: n.value, href: n.href, title: n.title,
          style: JSON.stringify(Object.fromEntries(Object.entries(n.style).filter(([, v]) => typeof v !== 'function')))};
      }
      out['document.title'] = document.title;
      return out;
    },
  };
  return env;
}

// makeFixture wraps the large payload as mutable JSON sources so each fetch
// returns a freshly parsed object, as a browser would.
function makeFixture(sizes) {
  const f = largeFixture(sizes);
  const fx = {now: f.now, raw: f, refreshText: JSON.stringify(f.data), systemText: JSON.stringify(f.system), logsText: JSON.stringify(f.logs), errorsText: JSON.stringify(f.errors)};
  fx.refreshJSON = () => fx.refreshText;
  fx.systemJSON = () => fx.systemText;
  fx.logsText1000 = JSON.stringify({...f.logs, entries: f.logs.entries.slice(-1000)});
  fx.logsJSON = () => fx.logsText1000;
  fx.errorsText80 = JSON.stringify({items: f.errors.items.slice(0, 80)});
  fx.errorsJSON = () => fx.errorsText80;
  fx.mutate = fn => { const d = JSON.parse(fx.refreshText); fn(d); fx.refreshText = JSON.stringify(d); };
  return fx;
}

function stats(samples) {
  const s = [...samples].sort((a, b) => a - b);
  const q = p => s[Math.min(s.length - 1, Math.ceil(p * s.length) - 1)];
  return {median: q(0.5), p95: q(0.95), min: s[0], n: s.length};
}

function bench(name, iterations, setup, fn) {
  const samples = [];
  for (let i = 0; i < Math.max(3, iterations >> 3); i++) { setup && setup(i); fn(i); } // warm-up
  for (let i = 0; i < iterations; i++) {
    setup && setup(i);
    const t = performance.now();
    fn(i);
    samples.push(performance.now() - t);
  }
  return {name, ...stats(samples)};
}

async function bootEnv(html, fixture) {
  const env = createEnv(html, fixture);
  await env.flush();
  await env.advance(0);
  return env;
}

async function runBench(html, iterations) {
  const fx = makeFixture();
  const env = await bootEnv(html, fx);
  const {run, ctx, stats: st} = env;
  const results = [];
  const parse = () => JSON.parse(fx.refreshText);
  const one = parse();
  const changed = parse();
  changed.sessions[0].totalTokens += 1;
  const measureWrites = fn => { const w = st.innerHTMLWrites, b = st.innerHTMLBytes; fn(); return {writes: st.innerHTMLWrites - w, bytes: st.innerHTMLBytes - b}; };
  ctx.__fresh = parse;

  // Full refresh pipeline as App.refresh drives it: snapshot, diff, render, commit.
  results.push(bench('refresh: cold (prev=null)', iterations, () => { run('State.prev=null;Renderer._svgCache={};'); ctx.__next = parse(); run('State.update(__next)'); }, () => run('App.renderNow()')));
  run('State.prev=null;State.update(__fresh());App.renderNow()');
  results.push(bench('refresh: no change', iterations, () => { ctx.__next = parse(); run('State.update(__next)'); }, () => run('App.renderNow()')));
  let flip = false;
  results.push(bench('refresh: one session changed', iterations, () => { flip = !flip; const d = parse(); d.sessions[0].totalTokens += flip ? 1 : 2; ctx.__next = d; run('State.update(__next)'); }, () => run('App.renderNow()')));
  // Phase breakdown on a no-change refresh.
  run('State.update(__fresh());App.renderNow()');
  results.push(bench('  phase: State.snapshot', iterations, () => { ctx.__next = parse(); run('State.update(__next)'); }, () => run('State.snapshot()')));
  results.push(bench('  phase: DirtyChecker.diff (no change)', iterations, () => { ctx.__snap = run('State.snapshot()'); }, () => run('DirtyChecker.diff(__snap)')));
  results.push(bench('  phase: Renderer.render all flags', iterations, () => { run('Renderer._svgCache={}'); ctx.__snap = run('State.snapshot()'); },
    () => run('Renderer.render(__snap,{alerts:1,health:1,cost:1,crons:1,sessions:1,usage:1,subRuns:1,subTokens:1,charts:1,models:1,skills:1,git:1,agentConfig:1})')));
  results.push(bench('  phase: RuntimePanels.render', iterations, null, () => run('RuntimePanels.render(State.data)')));
  results.push(bench('tab switch (usage 7d<->30d)', iterations, i => run(`State.setTab('usage',${i % 2 ? "'7d'" : "'30d'"})`), () => run('App.renderNow()')));
  run("State.setTab('usage','today');App.renderNow()");
  results.push(bench('charts: renderCharts 30d (cache cleared)', iterations, () => run('Renderer._svgCache={}'), () => run('Renderer.renderCharts(State.data,30)')));
  results.push(bench('charts: renderCharts 30d (cache hit)', iterations, null, () => run('Renderer.renderCharts(State.data,30)')));

  // Logs: 1000 is the server cap (logLimitMax); 5000 stresses the client.
  const logs1000 = JSON.parse(fx.logsText1000), logs5000 = fx.raw.logs;
  const noSeen = {...logs1000, entries: logs1000.entries.map(({seenAt, ...e}) => e)};
  run("LogTail.setRegex('')");
  ctx.__logs = logs1000; results.push(bench('logs: render 1000', iterations, null, () => run('LogTail._render(__logs)')));
  ctx.__logs5 = logs5000; results.push(bench('logs: render 5000', Math.max(5, iterations >> 2), null, () => run('LogTail._render(__logs5)')));
  ctx.__logsNS = noSeen; results.push(bench('logs: render 1000 without seenAt', iterations, null, () => run('LogTail._render(__logsNS)')));
  run("LogTail.setRegex('timeout|error')");
  results.push(bench('logs: render 1000 with regex', iterations, null, () => run('LogTail._render(__logs)')));
  results.push(bench('logs: setRegex (re-render cached 1000)', iterations, i => null, i => run(`LogTail.setRegex(${i % 2 ? "'timeout'" : "'upstream'"})`)));
  run("LogTail.setRegex('')");

  // Error feed: the server caps at 80; 1000 stresses the client.
  const errItems = fx.raw.errors.items;
  ctx.__errs = errItems.map(it => run('ErrorFeed')._normalizeItem(it));
  ctx.__errs80 = ctx.__errs.slice(0, 80);
  results.push(bench('errors: render 80', iterations, () => run('ErrorFeed._items=__errs80'), () => run('ErrorFeed.render()')));
  results.push(bench('errors: render 1000', Math.max(5, iterations >> 2), () => run('ErrorFeed._items=__errs'), () => run('ErrorFeed.render()')));

  // DOM write volume per cycle — a proxy for browser parse/layout cost.
  run('State.prev=null;State.update(__fresh());App.renderNow()');
  const writes = {
    'refresh: no change': measureWrites(() => { ctx.__next = parse(); run('State.update(__next);App.renderNow()'); }),
    'refresh: one session changed': measureWrites(() => { const d = parse(); d.sessions[0].totalTokens += 7; ctx.__next = d; run('State.update(__next);App.renderNow()'); }),
    'logs: poll with identical payload': measureWrites(() => run('LogTail._render(__logs)')),
  };
  void one; void changed;
  return {results, writes};
}

function printBench(label, {results, writes}) {
  console.log(`\n== ${label} ==`);
  console.log('operation'.padEnd(46) + 'median ms'.padStart(11) + 'p95 ms'.padStart(10) + 'n'.padStart(6));
  for (const r of results) console.log(r.name.padEnd(46) + r.median.toFixed(3).padStart(11) + r.p95.toFixed(3).padStart(10) + String(r.n).padStart(6));
  console.log('\nDOM innerHTML writes per cycle:');
  for (const [k, v] of Object.entries(writes)) console.log('  ' + k.padEnd(44) + String(v.writes).padStart(5) + ' writes ' + String(v.bytes).padStart(9) + ' bytes');
}

// runSoak simulates hours of polling (1s tick, 60s refresh, 15s logs, 5s
// system) with visibility flips, and reports structure sizes, timers and
// listeners so unbounded growth is visible.
async function runSoak(html, hours) {
  const fx = makeFixture({logs: 1000, errors: 80});
  const env = await bootEnv(html, fx);
  const {run, stats: st} = env;
  const gc = global.gc || (() => {});
  const snapshot = label => {
    gc();
    return {label, virtualHours: ((env.clock.now - fx.now) / 3600000).toFixed(2), heapMB: +(process.memoryUsage().heapUsed / 1048576).toFixed(1), activeTimers: env.timers.size,
      intervals: [...env.timers.values()].filter(t => t.repeat).length, listeners: [...st.listeners.values()].reduce((a, v) => a + v, 0),
      recentFinished: Object.keys(run('Renderer._recentFinished')).length, svgCache: Object.keys(run('Renderer._svgCache')).length,
      errorItems: run('ErrorFeed._items.length'), errorExpanded: Object.keys(run('ErrorFeed._expanded')).length, logEntries: (run('LogTail._lastPayload')?.entries || []).length,
      chatHistory: run('Chat.history.length'), fetches: Object.fromEntries(st.fetches)};
  };
  const rows = [snapshot('boot')];
  const listenersAfterBoot = rows[0].listeners;
  const baseLogs = JSON.parse(fx.logsText1000);
  const totalMs = hours * 3600000, step = 60000;
  let cycle = 0;
  for (let t = 0; t < totalMs; t += step) {
    cycle++;
    // Churn: sessions become active/finish, a cron changes, logs roll forward.
    fx.mutate(d => {
      for (const s of d.sessions) if (s.active || (cycle + Number(s.key.slice(s.key.lastIndexOf('s') + 1))) % 97 === 0) s.active = !s.active;
      d.crons[cycle % d.crons.length].lastRun = cycle + 'm ago';
      d.sessions[cycle % d.sessions.length].totalTokens += cycle;
    });
    fx.logsText1000 = JSON.stringify({...baseLogs, entries: baseLogs.entries.map((e, i) => ({...e, message: e.message + ' c' + cycle, seenAt: new Date(fx.now + t + i).toISOString()}))});
    if (cycle % 30 === 0) await env.setHidden(true);
    if (cycle % 30 === 2) await env.setHidden(false);
    if (cycle % 45 === 0) run('LogTail.toggleFast()');
    await env.advance(step);
    if (cycle % Math.max(1, Math.floor(totalMs / step / 6)) === 0) rows.push(snapshot('cycle ' + cycle));
  }
  rows.push(snapshot('end'));
  console.log('\n== soak: ' + hours + 'h virtual, ' + cycle + ' refresh cycles ==');
  console.table(rows.map(({fetches, ...r}) => r));
  console.log('fetch counts:', rows.at(-1).fetches);
  console.log('timers: intervals set', st.intervalsSet, 'cleared', st.intervalsCleared, '| timeouts set', st.timeoutsSet, 'cleared-or-fired', st.timeoutsSet - [...env.timers.values()].filter(t => !t.repeat).length);
  console.log('listeners by target:', Object.fromEntries(st.listeners));
  const end = rows.at(-1);
  assert.equal(end.listeners, listenersAfterBoot, 'event listeners grew during soak');
  assert.ok(end.intervals <= 4, 'more than 4 live intervals: ' + end.intervals);
  assert.ok(end.activeTimers - end.intervals <= 1, 'pending timeouts leaked');
  assert.ok(end.svgCache <= 3, 'svg cache grew');
  console.log('soak assertions passed');
}

// runEquiv drives two independent environments (baseline revision and working
// tree) through the same scripted sequence and requires identical stub DOM
// state at every checkpoint.
async function runEquiv(rev) {
  const baseHtml = loadHtml(rev), curHtml = loadHtml();
  const sizes = [{}, {sessions: 3, crons: 0, models: 1, days: 1, logs: 0, errors: 0, tasks: 0, runs: 0}, {sessions: 0, crons: 0, models: 0, days: 0, logs: 5, errors: 1, tasks: 0, runs: 0}];
  let checkpoints = 0;
  for (const size of sizes) {
    const fxA = makeFixture(size), fxB = makeFixture(size);
    const A = await bootEnv(baseHtml, fxA), B = await bootEnv(curHtml, fxB);
    const both = async fn => { await fn(A, fxA); await fn(B, fxB); };
    const check = label => {
      checkpoints++;
      const a = A.domState(), b = B.domState();
      for (const id of new Set([...Object.keys(a), ...Object.keys(b)])) assert.deepEqual(b[id], a[id], `${label}: #${id} differs from ${rev}`);
    };
    check('boot ' + JSON.stringify(size));
    if (!Object.keys(size).length) {
      // Guard against a vacuous comparison: the big fixture must populate the
      // containers whose rendering paths changed.
      for (const id of ['sessBody', 'cronBody', 'uBody', 'logTail', 'errorBody', 'costChart', 'modelChart', 'sessionWork', 'taskLedger', 'workSessionSelect', 'memoryHealth', 'agentTree'])
        assert.ok(B.nodes.get(id)?.innerHTML.length > 200, '#' + id + ' not populated by the fixture');
      assert.match(B.nodes.get('errorBody').innerHTML, /\d{2}\/\d{2}\/\d{4}, \d{2}:\d{2}:\d{2} GMT\+8/, 'error feed times not formatted in the dashboard timezone');
    }
    const steps = [
      ['60s later', async e => e.advance(60000)],
      ['session + cron changes', async (e, fx) => { fx.mutate(d => { if (d.sessions[0]) { d.sessions[0].totalTokens += 5; d.sessions[0].active = !d.sessions[0].active; } if (d.crons[0]) d.crons[0].nextRun = 'soon'; }); await e.advance(60000); }],
      ['timezone change', async (e, fx) => { fx.mutate(d => { d.timezone = 'America/New_York'; }); await e.advance(60000); }],
      ['invalid timezone', async (e, fx) => { fx.mutate(d => { d.timezone = 'Not/AZone'; }); await e.advance(60000); }],
      ['tabs + chart range', async e => { e.run("OCUI.setUsageTab('30d');OCUI.setSubRunsTab('7d');OCUI.setSubTokensTab('all');OCUI.setChartDays(30)"); await e.flush(); }],
      ['log regex + severity + source', async e => { e.run("OCUI.setLogRegex('timeout');OCUI.setLogSeverity('error');OCUI.setLogSource('gateway')"); await e.advance(15000); }],
      ['invalid regex then clear', async e => { e.run("OCUI.setLogRegex('[');OCUI.setLogRegex('');OCUI.setLogSeverity('all');OCUI.setLogSource('all')"); await e.advance(15000); }],
      ['log payload without seenAt', async (e, fx) => { const L = JSON.parse(fx.logsText1000); L.entries = L.entries.map(({seenAt, ...x}) => x); fx.logsText1000 = JSON.stringify(L); await e.advance(15000); }],
      ['error sort + expand', async e => { e.run("OCUI.setErrorSort('last_seen');"); await e.flush(); e.run("ErrorFeed.toggle('0')"); await e.advance(60000); }],
      ['session page 2', async e => { e.run('RuntimePanels.sessionPage=1;Renderer.render(State.snapshot(),{sessions:true})'); await e.flush(); }],
      ['hidden then visible', async e => { await e.setHidden(true); await e.advance(120000); await e.setHidden(false); await e.advance(1000); }],
      ['sessions emptied', async (e, fx) => { fx.mutate(d => { d.sessions = []; d.crons = []; d.dailyChart = []; }); await e.advance(60000); }],
      ['recent window expiry', async e => e.advance(6 * 60000)],
    ];
    for (const [label, fn] of steps) { await both(fn); check(label + ' ' + JSON.stringify(size)); }
  }
  console.log(`equivalence: ${checkpoints} checkpoints identical to ${rev}`);
}

// runBrowser measures the same cycles in real Chrome, including DOM parsing,
// style and layout (forced with offsetHeight), which the stub DOM cannot show.
// Optional: needs Playwright, as scripts/ui-browser-smoke.cjs does.
async function runBrowser(html, iterations) {
  const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
  const fx = makeFixture();
  const page_ = html.replaceAll('__RUNTIME__', 'Perf').replaceAll('__VERSION__', 'perf');
  const browser = await chromium.launch({headless: true, channel: process.env.BROWSER_CHANNEL || 'chrome'});
  try {
    const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/*', route => {
      const u = new URL(route.request().url());
      const body = u.pathname === '/' ? page_ : u.pathname === '/themes.json' ? '{}' : u.pathname === '/api/refresh' ? fx.refreshText : u.pathname === '/api/system' ? fx.systemText
        : u.pathname === '/api/logs' ? fx.logsText1000 : u.pathname === '/api/errors' ? fx.errorsText80 : u.pathname === '/api/operations/status' ? '{"enabled":true}' : '{}';
      return route.fulfill({status: 200, contentType: u.pathname === '/' ? 'text/html' : 'application/json', body});
    });
    await page.goto('http://127.0.0.1:1/', {waitUntil: 'load'});
    await page.waitForFunction(() => document.getElementById('botName').textContent === 'Perf Fixture');
    await page.evaluate(() => OCUI.expandAll());
    const results = await page.evaluate(async ({refresh, logs, errors, iterations}) => {
      const measure = (name, n, setup, fn) => {
        const samples = [];
        for (let i = 0; i < n + 3; i++) {
          setup && setup(i);
          const t = performance.now();
          fn(i);
          void document.body.offsetHeight;
          if (i >= 3) samples.push(performance.now() - t);
        }
        samples.sort((a, b) => a - b);
        return {name, median: samples[Math.floor(samples.length / 2)], p95: samples[Math.min(samples.length - 1, Math.ceil(samples.length * 0.95) - 1)], n};
      };
      const cycle = () => { const snap = State.snapshot(); Renderer.render(snap, DirtyChecker.diff(snap)); State.commitPrev(snap); };
      const out = [];
      out.push(measure('browser refresh: cold (prev=null)', iterations, () => { State.prev = null; Renderer._svgCache = {}; State.update(JSON.parse(refresh)); }, cycle));
      out.push(measure('browser refresh: no change', iterations, () => State.update(JSON.parse(refresh)), cycle));
      out.push(measure('browser refresh: one session changed', iterations, i => { const d = JSON.parse(refresh); d.sessions[0].totalTokens += i; State.update(d); }, cycle));
      out.push(measure('browser refresh: via App.refresh path (applyRuntimeConfig)', iterations, () => State.update(JSON.parse(refresh)), () => { LogTail.applyRuntimeConfig(State.data.logConfig); cycle(); }));
      const L = JSON.parse(logs);
      out.push(measure('browser logs: poll, identical 1000 entries', iterations, null, () => LogTail._render(L)));
      out.push(measure('browser logs: poll, one new entry', iterations, i => { L.entries = L.entries.slice(1).concat({...L.entries[0], message: 'new ' + i}); }, () => LogTail._render(L)));
      const E = JSON.parse(errors);
      out.push(measure('browser errors: render 80', iterations, () => { ErrorFeed._items = E.items.map(x => ErrorFeed._normalizeItem(x)); }, () => ErrorFeed.render()));
      return out;
    }, {refresh: fx.refreshText, logs: fx.logsText1000, errors: fx.errorsText80, iterations});
    assert.deepEqual(errors, [], 'page errors');
    return {results, writes: {}};
  } finally { await browser.close(); }
}

// runBrowserEquiv loads a revision and the working tree in real Chrome under a
// frozen clock, drives both through the same refresh/log/error sequence, and
// requires identical serialized DOM and form state.
async function runBrowserEquiv(rev) {
  const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
  const browser = await chromium.launch({headless: true, channel: process.env.BROWSER_CHANNEL || 'chrome'});
  const capture = async html => {
    const fx = makeFixture({logs: 1000, errors: 80});
    const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.clock.install({time: fx.now});
    await page.route('**/*', route => {
      const u = new URL(route.request().url());
      const body = u.pathname === '/' ? html : u.pathname === '/themes.json' ? '{}' : u.pathname === '/api/refresh' ? fx.refreshText : u.pathname === '/api/system' ? fx.systemText
        : u.pathname === '/api/logs' ? fx.logsText1000 : u.pathname === '/api/errors' ? fx.errorsText80 : u.pathname === '/api/operations/status' ? '{"enabled":true}' : '{}';
      return route.fulfill({status: 200, contentType: u.pathname === '/' ? 'text/html' : 'application/json', body});
    });
    await page.goto('http://127.0.0.1:1/', {waitUntil: 'load'});
    await page.clock.pauseAt(fx.now + 60000);
    await page.clock.runFor(1000);
    await page.waitForFunction(() => document.getElementById('botName').textContent === 'Perf Fixture');
    const refresh = async () => { await page.evaluate(() => App.refresh()); await page.clock.runFor(100); };
    await page.evaluate(() => { OCUI.expandAll(); document.getElementById('workSessionSelect').value = 'agent:work:s1'; });
    await refresh();
    await refresh();
    fx.mutate(d => { d.sessions[0].totalTokens += 3; d.sessions = d.sessions.slice(1).concat(d.sessions[0]); d.timezone = 'America/New_York'; });
    await refresh();
    await page.evaluate(async () => { await LogTail.fetch(); LogTail._setStatus('Network error while fetching logs.'); await LogTail.fetch(); await ErrorFeed.fetch(); });
    fx.mutate(d => { d.sessions = []; d.crons = []; });
    await refresh();
    fx.mutate(d => { d.sessions = largeFixture().data.sessions; });
    await refresh();
    const state = await page.evaluate(() => {
      const body = document.body.cloneNode(true);
      body.querySelectorAll('script').forEach(s => s.remove());
      const controls = [...document.querySelectorAll('select,input,textarea')].map(e => [e.id, e.value, e.disabled]);
      return {html: body.innerHTML.replace(/Updated: [^<]*/, ''), controls, title: document.title};
    });
    assert.deepEqual(errors, [], 'page errors');
    await page.close();
    return state;
  };
  try {
    const a = await capture(loadHtml(rev)), b = await capture(loadHtml());
    assert.ok(a.html.length > 100000, 'baseline DOM unexpectedly small');
    assert.deepEqual(b.controls, a.controls, 'form control state differs');
    assert.equal(b.title, a.title);
    if (a.html !== b.html) {
      let i = 0; while (a.html[i] === b.html[i]) i++;
      assert.fail('DOM differs at ' + i + ':\n  ' + rev + ': ' + a.html.slice(i - 80, i + 120) + '\n  tree: ' + b.html.slice(i - 80, i + 120));
    }
    console.log(`browser equivalence: ${a.html.length} bytes of DOM and ${a.controls.length} form controls identical to ${rev}`);
  } finally { await browser.close(); }
}

(async () => {
  const iterations = Number(opt('iterations', 30));
  if (flag('equiv')) return runEquiv(opt('equiv'));
  if (flag('browser-equiv')) return runBrowserEquiv(opt('browser-equiv'));
  const rev = opt('rev');
  const html = loadHtml(rev);
  if (flag('soak')) return runSoak(html, Number(opt('hours', 6)));
  if (flag('browser')) return printBench('browser, ' + (rev ? 'revision ' + rev : 'working tree'), await runBrowser(html, iterations));
  const out = await runBench(html, iterations);
  if (flag('json')) { console.log(JSON.stringify(out)); return; }
  printBench(rev ? 'revision ' + rev : 'working tree', out);
})().catch(e => { console.error(e); process.exitCode = 1; });
