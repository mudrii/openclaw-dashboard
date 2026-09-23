// Large synthetic dashboard payloads for frontend performance checks.
// Synthetic data only; deterministic for a given `now` and size options.
module.exports = function largeFixture(opts = {}) {
  const n = {sessions: 1000, crons: 500, models: 200, days: 90, logs: 5000, errors: 1000, tasks: 1000, runs: 500, ...opts};
  const now = opts.now ?? Date.UTC(2026, 8, 1, 12, 0, 0), iso = new Date(now).toISOString();
  let seed = 42;
  const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648;
  const pick = arr => arr[Math.floor(rnd() * arr.length)];
  const collection = source => ({source, state: 'ready', complete: true, collectedAt: iso, attemptedAt: iso});
  const modelNames = Array.from({length: n.models}, (_, i) => 'model-' + i);
  const usageRow = (name, i) => {
    const input = 1000 + i * 37, output = 200 + i * 11, cache = i * 5, total = input + output + cache;
    return {model: name, modelId: 'fixture/' + name, calls: 3 + i, input: input.toLocaleString(), output: output.toLocaleString(), cacheRead: cache.toLocaleString(), cacheWrite: '0', totalTokens: total.toLocaleString(), inputRaw: input, outputRaw: output, cacheReadRaw: cache, cacheWriteRaw: 0, totalTokensRaw: total, cost: +(total / 1e5).toFixed(4), knownCost: +(total / 1e5).toFixed(4), costComplete: true, tokensComplete: true, missingCostEntries: 0};
  };
  const usageTable = () => modelNames.map(usageRow);
  const usage = {timezone: 'UTC', scope: 'all', tokensComplete: true, costComplete: true, totalTokens: 123456, knownCost: 42, missingCostEntries: 0};
  const agents = ['main', 'work', 'group', 'ops', 'research', 'qa', 'build', 'docs', 'infra', 'data'];
  const sessions = Array.from({length: n.sessions}, (_, i) => {
    const sub = i % 5 === 1, active = i % 17 === 0;
    return {key: 'agent:' + agents[i % agents.length] + ':s' + i, name: 'Session ' + i, agent: agents[i % agents.length], model: pick(modelNames), modelId: 'fixture/' + pick(modelNames),
      type: sub ? 'subagent' : pick(['main', 'group', 'cron']), active, activeRunIds: active ? ['run-' + i] : [], spawnedBy: sub ? 'agent:' + agents[(i - 1) % agents.length] + ':s' + (i - 1) : '',
      contextPct: Math.floor(rnd() * 100), contextTokens: 4000, totalTokens: 1000 + i * 13, tokenState: 'fresh', label: 'label ' + i, subject: 'subject ' + i,
      updatedAt: now - i * 60000, placement: {state: 'foreground'}, visibility: 'shared', goal: i % 10 === 0 ? {objective: 'Goal ' + i, status: 'active', tokensUsed: i, tokenBudget: 1000} : undefined};
  });
  const crons = Array.from({length: n.crons}, (_, i) => ({id: 'job-' + i, name: 'Job ' + i, enabled: i % 9 !== 0, status: pick(['ok', 'error', 'none', 'running']), lastStatus: 'ok', payloadKind: pick(['agentTurn', 'heartbeat', 'systemEvent']),
    sessionTarget: pick(['isolated', 'main']), schedule: 'Every ' + (5 + i % 55) + 'm', timezone: 'UTC', lastRun: (i % 60) + 'm ago', nextRun: 'in ' + (i % 60) + 'm', lastDurationMs: 100 + i * 7, model: pick(modelNames),
    lastDeliveryStatus: pick(['delivered', 'not-delivered', 'unknown', 'not-requested']), flapping: i % 50 === 0, consecutiveErrors: i % 4, lastDiagnostics: i % 7 === 0 ? ['diag ' + i] : [], configRevision: 'rev-' + i}));
  const dailyChart = Array.from({length: n.days}, (_, i) => {
    const models = {};
    for (let m = 0; m < 6; m++) models[modelNames[m]] = +(rnd() * 3).toFixed(4);
    const total = Object.values(models).reduce((a, v) => a + v, 0);
    return {date: new Date(now - (n.days - 1 - i) * 86400000).toISOString().slice(0, 10), label: 'd' + i, total, tokens: Math.floor(rnd() * 1e6), calls: 10 + i, models, subagentRuns: i % 11, subagentCost: +(rnd()).toFixed(3)};
  });
  const runs = Array.from({length: n.runs}, (_, i) => ({id: 'sub-' + i, task: 'Subtask ' + i, agent: pick(agents), durationSec: i * 13, status: pick(['succeeded', 'failed', 'running']), timestamp: new Date(now - i * 60000).toISOString()}));
  const data = {schemaVersion: 2, botName: 'Perf Fixture', botEmoji: '🧪', timezone: 'Asia/Singapore', lastRefresh: iso, lastRefreshMs: now,
    runtimeTarget: {mode: 'native'}, gateway: {status: 'online', port: 18789, pid: 4242, uptime: '3h', memory: '120MB'}, runtimeInfo: {pid: 4242, uptimeMs: 3600000}, compactionMode: 'safeguard',
    gatewayHealth: {ok: true, ts: now},
    runtimeHealth: {runtimeVersion: '2026.9.2', processMemory: {rssBytes: 104857600}, eventLoop: {degraded: false, delayP99Ms: 2}, taskAudit: {warnings: 0, errors: 0}, degradedPluginsCount: 0, degradedSecretOwnersCount: 0},
    collections: Object.fromEntries(['channels', 'configuration', 'crons', 'diagnostics', 'memory', 'memoryIndex', 'modelReadiness', 'pluginInventory', 'runtimeHealth', 'runtimeInfo', 'gatewayHealth', 'sessions', 'skillInventory', 'tasks', 'usageToday', 'usage7d', 'usage30d', 'usageAll']
      .map(k => [k, collection(k === 'sessions' ? 'gateway.sessions.list' : k === 'runtimeHealth' ? 'gateway.status' : 'fixture.' + k)])),
    alerts: Array.from({length: 20}, (_, i) => ({severity: pick(['low', 'medium', 'high']), icon: '⚠️', message: 'Alert ' + i})),
    totalCostToday: 12.5, totalCostAllTime: 1234.5, projectedMonthly: 375,
    costBreakdown: modelNames.map((m, i) => ({model: m, cost: +(1 + i * 0.37).toFixed(2)})),
    dailyChart, sessionCount: n.sessions, sessionTotal: n.sessions, sessions, crons,
    tasks: Array.from({length: n.tasks}, (_, i) => ({id: 'task-' + i, task: 'Task ' + i, agent: pick(agents), runtime: i % 2 ? 'cron' : 'subagent', status: i % 3 ? 'succeeded' : 'running', ownerKey: sessions[i % sessions.length]?.key, runId: 'run-' + i, progressSummary: 'Step ' + i, deliveryStatus: 'delivered', durationSec: i})),
    subagentRuns: runs, subagentRunsToday: runs.slice(0, 50), subagentRuns7d: runs.slice(0, 200), subagentRuns30d: runs,
    memory: agents.map(a => ({agentId: a, provider: 'fixture', embedding: {checked: true, ok: true}, dreaming: {enabled: true, shortTermCount: 3, promotedToday: 1, phases: {deep: {nextRunAtMs: now + 3600000}, light: {nextRunAtMs: now + 600000}}}})),
    memoryIndex: agents.map(a => ({agentId: a, status: {model: 'embed', files: 100, chunks: 1000, dirty: false}})),
    channels: Array.from({length: 20}, (_, i) => ({channel: pick(['telegram', 'slack', 'discord']), accountId: 'acct-' + i, enabled: true, configured: true, running: true, connected: i % 4 !== 0, healthState: 'healthy', probe: {ok: true}, hasError: false})),
    pluginInventory: Array.from({length: 40}, (_, i) => ({id: i ? 'plugin-' + i : 'workboard', version: '1.' + i, origin: 'bundled', enabled: true, status: 'loaded', toolNames: ['tool.a', 'tool.b']})),
    skillInventory: Array.from({length: 200}, (_, i) => ({agentId: pick(agents), name: 'skill-' + i, eligible: i % 3 !== 0, modelVisible: i % 2 === 0, disabled: false, missing: i % 3 ? {} : {bins: ['tool-' + i]}})),
    modelReadiness: modelNames.map((m, i) => ({agentId: agents[i % agents.length], modelId: 'fixture/' + m, allowed: true, default: i === 0, catalogName: m, authKind: 'api-key', missingProviderInUse: false})),
    diagnostics: {backup: {enabled: true, lastAttemptAt: iso, lastSuccessAt: iso}},
    availableModels: modelNames.map((m, i) => ({name: m, provider: 'provider-' + (i % 12), status: i % 4 ? 'available' : 'active'})),
    gitLog: Array.from({length: 20}, (_, i) => ({hash: 'h' + i, message: 'Commit ' + i, ago: i + 'h ago'})),
    agentConfig: {agents: agents.map((a, i) => ({id: a, role: 'Role ' + a, model: modelNames[i], modelId: 'fixture/' + modelNames[i], workspace: '/ws/' + a, fallbacks: [modelNames[i + 1]], context1m: i % 2 === 0, isDefault: i === 0})),
      primaryModel: 'fixture/model-0', fallbacks: ['fixture/model-1', 'fixture/model-2'], imageModel: 'fixture/model-3', streamMode: 'off',
      compaction: {mode: 'safeguard', reserveTokensFloor: 1000, memoryFlush: {enabled: true}, softThresholdTokens: 2000}, telegramDmPolicy: 'allowlist', telegramGroups: 3, channels: ['telegram', 'slack', 'discord'],
      search: {provider: 'fixture', maxResults: 5, cacheTtlMinutes: 10}, gateway: {port: 18789, mode: 'local', bind: 'loopback', authMode: 'token', tailscale: 'off'},
      hooks: Array.from({length: 10}, (_, i) => ({name: 'hook-' + i, enabled: i % 2 === 0})), plugins: Array.from({length: 10}, (_, i) => ({name: 'plugin-' + i, enabled: true})), skills: Array.from({length: 30}, (_, i) => ({name: 'skill-' + i, enabled: i % 2 === 0})),
      tts: false, diagnostics: true, bindings: [{agentId: 'main', kind: 'default'}], subagentConfig: {maxSpawnDepth: 2, maxChildrenPerAgent: 4, maxConcurrent: 8}, modelPricing: {}},
    logConfig: {logTailLines: 1000, logFastRefreshMs: 3000, logRefreshIntervalMs: 15000, errorWindowHours: 24}};
  for (const suffix of ['Today', '7d', '30d', 'All']) {
    data['usage' + suffix] = {...usage};
    data[suffix === 'All' ? 'tokenUsage' : 'tokenUsage' + suffix] = usageTable();
    data[suffix === 'All' ? 'subagentUsage' : 'subagentUsage' + suffix] = usageTable().slice(0, 50);
  }
  const sources = ['gateway', 'cron', 'session', 'subagent'], sev = ['info', 'info', 'info', 'warn', 'error'];
  const logs = {entries: Array.from({length: n.logs}, (_, i) => {
    const ts = now - (n.logs - i) * 1000;
    return {source: sources[i % 4], severity: sev[i % 5], timestamp: ts, seenAt: new Date(ts).toISOString(), line: '', message: 'request ' + i + ' handled in ' + (i % 997) + 'ms' + (i % 13 === 0 ? ' timeout contacting upstream <b>x</b>' : '')};
  }), collectionSource: 'fixture', truncated: false};
  const errors = {items: Array.from({length: n.errors}, (_, i) => ({source: sources[i % 4], severity: i % 3 ? 'error' : 'warn', count: 1 + (i % 40), signature: 'sig-' + i, sampleMessage: 'Error ' + i + ' failed to connect <x>',
    firstSeen: now - 86400000 + i, lastSeen: now - i * 1000, lastOccurrences: Array.from({length: 5}, (_, k) => ({timestamp: now - i * 1000 - k * 60000, message: 'occurrence ' + k}))}))};
  const system = {cpu: {percent: 20}, ram: {totalBytes: 1073741824, usedBytes: 536870912, percent: 50}, swap: {percent: 0, totalBytes: 0, usedBytes: 0}, disk: {totalBytes: 10737418240, usedBytes: 5368709120, percent: 50},
    versions: {openclaw: '2026.9.2', gateway: {status: 'online', version: '2026.9.2', pid: 4242, uptime: '3h', memory: '120MB'}}, openclaw: {gateway: {live: true, ready: true, healthEndpointOk: true, uptimeMs: 3600000}, status: {currentVersion: '2026.9.2'}}, pollSeconds: 5};
  return {data, system, logs, errors, now};
};
