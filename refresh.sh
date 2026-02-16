#!/bin/bash
# OpenClaw Dashboard — Data Refresh Script
# Generates data.json with all dashboard data

set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
OPENCLAW_PATH="${OPENCLAW_HOME:-$HOME/.openclaw}"
OPENCLAW_PATH="${OPENCLAW_PATH/#\~/$HOME}"

echo "Dashboard dir: $DIR"
echo "OpenClaw path: $OPENCLAW_PATH"

if [ ! -d "$OPENCLAW_PATH" ]; then
  echo "❌ OpenClaw not found at $OPENCLAW_PATH"
  exit 1
fi

PYTHON=$(command -v python3 || command -v python)
if [ -z "$PYTHON" ]; then
  echo "❌ Python not found"
  exit 1
fi

$PYTHON - "$DIR" "$OPENCLAW_PATH" << 'PYEOF' > "$DIR/data.json.tmp"
import json, glob, os, sys, subprocess, time
from collections import defaultdict
from datetime import datetime, timezone, timedelta

dashboard_dir = sys.argv[1]
openclaw_path = sys.argv[2]

local_tz = timezone(timedelta(hours=8))  # GMT+8
now = datetime.now(local_tz)
today_str = now.strftime('%Y-%m-%d')

base = os.path.join(openclaw_path, "agents")
config_path = os.path.join(openclaw_path, "openclaw.json")
cron_path = os.path.join(openclaw_path, "cron/jobs.json")

# ── Bot config ──
bot_name = "OpenClaw Dashboard"
bot_emoji = "⚡"
dc_path = os.path.join(dashboard_dir, "config.json")
if os.path.exists(dc_path):
    try:
        dc = json.load(open(dc_path))
        bot_name = dc.get('bot', {}).get('name', bot_name)
        bot_emoji = dc.get('bot', {}).get('emoji', bot_emoji)
    except: dc = {}
else:
    dc = {}

# ── Alert thresholds (configurable via config.json) ──
alert_cfg = dc.get('alerts', {})
COST_THRESHOLD_HIGH = alert_cfg.get('dailyCostHigh', 50)
COST_THRESHOLD_WARN = alert_cfg.get('dailyCostWarn', 20)
CONTEXT_THRESHOLD = alert_cfg.get('contextPct', 80)
MEMORY_THRESHOLD_KB = alert_cfg.get('memoryMb', 500) * 1024

# ── Gateway health ──
gateway = {"status": "offline", "pid": None, "uptime": "", "memory": "", "rss": 0}
try:
    result = subprocess.run("ps aux | grep 'openclaw-gateway' | grep -v grep | awk '{print $2}'",
                          shell=True, capture_output=True, text=True)
    pids = result.stdout.strip().split('\n')
    if pids and pids[0]:
        pid = pids[0]
        gateway["pid"] = int(pid)
        gateway["status"] = "online"
        ps = subprocess.run(['ps', '-p', pid, '-o', 'etime=,rss='], capture_output=True, text=True)
        parts = ps.stdout.strip().split()
        if len(parts) >= 2:
            gateway["uptime"] = parts[0].strip()
            rss_kb = int(parts[1])
            gateway["rss"] = rss_kb
            if rss_kb > 1048576: gateway["memory"] = f"{rss_kb/1048576:.1f} GB"
            elif rss_kb > 1024: gateway["memory"] = f"{rss_kb/1024:.0f} MB"
            else: gateway["memory"] = f"{rss_kb} KB"
except: pass

# ── OpenClaw config ──
skills = []
available_models = []
compaction_mode = "unknown"
if os.path.exists(config_path):
    try:
        with open(config_path) as cf:
            oc = json.load(cf)
        # Compaction
        compaction_mode = oc.get('agents', {}).get('defaults', {}).get('compaction', {}).get('mode', 'auto')
        # Skills
        for name, conf in oc.get('skills', {}).get('entries', {}).items():
            enabled = conf.get('enabled', True) if isinstance(conf, dict) else True
            skills.append({'name': name, 'active': enabled, 'type': 'builtin'})
        # Models
        primary = oc.get('agents', {}).get('defaults', {}).get('model', {}).get('primary', '')
        for mid, mconf in oc.get('agents', {}).get('defaults', {}).get('models', {}).items():
            provider = mid.split('/')[0] if '/' in mid else 'unknown'
            available_models.append({
                'provider': provider.title(),
                'name': mconf.get('alias', mid),
                'id': mid,
                'status': 'active' if mid == primary else 'available'
            })
    except: pass

# ── Sessions ──
known_sids = {}
sessions_list = []
for store_file in glob.glob(os.path.join(base, '*/sessions/sessions.json')):
    try:
        store = json.load(open(store_file))
        agent_name = store_file.split('/agents/')[1].split('/')[0]
        for key, val in store.items():
            sid = val.get('sessionId', '')
            if not sid: continue
            # Skip cron run sessions (duplicates of parent cron)
            if ':run:' in key: continue
            if 'cron:' in key: stype = 'cron'
            elif 'subagent:' in key: stype = 'subagent'
            elif 'group:' in key: stype = 'group'
            elif 'telegram' in key: stype = 'telegram'
            elif key.endswith(':main'): stype = 'main'
            else: stype = 'other'
            known_sids[sid] = stype

            # Build session info for active sessions panel
            ctx_tokens = val.get('contextTokens', 0)
            total_tokens = val.get('totalTokens', 0)
            ctx_pct = round(total_tokens / ctx_tokens * 100, 1) if ctx_tokens > 0 else 0
            updated = val.get('updatedAt', 0)
            if updated > 0:
                try:
                    updated_dt = datetime.fromtimestamp(updated/1000, tz=local_tz)
                    updated_str = updated_dt.strftime('%H:%M:%S')
                    age_min = (now - updated_dt).total_seconds() / 60
                except: updated_str = ''; age_min = 9999
            else: updated_str = ''; age_min = 9999

            # Only include recently active sessions (last 24h)
            if age_min < 1440:
                label = val.get('origin', {}).get('label', key)
                sessions_list.append({
                    'name': label[:50],
                    'key': key,
                    'agent': agent_name,
                    'model': val.get('model', 'unknown'),
                    'contextPct': min(ctx_pct, 100),
                    'lastActivity': updated_str,
                    'updatedAt': updated,
                    'totalTokens': total_tokens,
                    'type': stype
                })
    except: pass

sessions_list.sort(key=lambda x: -x.get('updatedAt', 0))
sessions_list = sessions_list[:20]  # Top 20 most recent

# ── Cron jobs ──
crons = []
if os.path.exists(cron_path):
    try:
        jobs = json.load(open(cron_path)).get('jobs', [])
        for job in jobs:
            sched = job.get('schedule', {})
            kind = sched.get('kind', '')
            if kind == 'cron': schedule_str = sched.get('expr', '')
            elif kind == 'every':
                ms = sched.get('everyMs', 0)
                if ms >= 86400000: schedule_str = f"Every {ms//86400000}d"
                elif ms >= 3600000: schedule_str = f"Every {ms//3600000}h"
                elif ms >= 60000: schedule_str = f"Every {ms//60000}m"
                else: schedule_str = f"Every {ms}ms"
            elif kind == 'at': schedule_str = sched.get('at', '')[:16]
            else: schedule_str = str(sched)

            state = job.get('state', {})
            last_status = state.get('lastStatus', 'none')
            last_run_ms = state.get('lastRunAtMs', 0)
            next_run_ms = state.get('nextRunAtMs', 0)
            duration_ms = state.get('lastDurationMs', 0)

            last_run_str = ''
            if last_run_ms:
                try: last_run_str = datetime.fromtimestamp(last_run_ms/1000, tz=local_tz).strftime('%Y-%m-%d %H:%M')
                except: pass
            next_run_str = ''
            if next_run_ms:
                try: next_run_str = datetime.fromtimestamp(next_run_ms/1000, tz=local_tz).strftime('%Y-%m-%d %H:%M')
                except: pass

            crons.append({
                'name': job.get('name', 'Unknown'),
                'schedule': schedule_str,
                'enabled': job.get('enabled', True),
                'lastRun': last_run_str,
                'lastStatus': last_status,
                'lastDurationMs': duration_ms,
                'nextRun': next_run_str,
                'model': job.get('payload', {}).get('model', '')
            })
    except: pass

# ── Token usage from JSONL ──
def model_name(model):
    # Strip provider prefix (e.g. "openai-codex/gpt-5.3-codex" -> "gpt-5.3-codex")
    ml = model.lower()
    if '/' in ml:
        ml = ml.split('/', 1)[1]
    if 'opus-4-6' in ml: return 'Claude Opus 4.6'
    elif 'opus' in ml: return 'Claude Opus 4.5'
    elif 'sonnet' in ml: return 'Claude Sonnet'
    elif 'haiku' in ml: return 'Claude Haiku'
    elif 'grok-4-fast' in ml: return 'Grok 4 Fast'
    elif 'grok-4' in ml or 'grok4' in ml: return 'Grok 4'
    elif 'gemini-2.5-pro' in ml or 'gemini-pro' in ml: return 'Gemini 2.5 Pro'
    elif 'gemini-3-flash' in ml: return 'Gemini 3 Flash'
    elif 'gemini-2.5-flash' in ml: return 'Gemini 2.5 Flash'
    elif 'gemini' in ml or 'flash' in ml: return 'Gemini Flash'
    elif 'minimax-m2.5' in ml: return 'MiniMax M2.5'
    elif 'minimax-m2' in ml or 'minimax' in ml: return 'MiniMax'
    elif 'glm-5' in ml: return 'GLM-5'
    elif 'glm-4' in ml: return 'GLM-4'
    elif 'k2p5' in ml or 'kimi' in ml: return 'Kimi K2.5'
    elif 'gpt-5.3-codex' in ml: return 'GPT-5.3 Codex'
    elif 'gpt-5' in ml: return 'GPT-5'
    elif 'gpt-4o' in ml: return 'GPT-4o'
    elif 'gpt-4' in ml: return 'GPT-4'
    elif 'o1' in ml: return 'O1'
    elif 'o3' in ml: return 'O3'
    else: return model

def new_bucket():
    return {'calls':0,'input':0,'output':0,'cacheRead':0,'totalTokens':0,'cost':0.0}

models_all = defaultdict(new_bucket)
models_today = defaultdict(new_bucket)
models_7d = defaultdict(new_bucket)
models_30d = defaultdict(new_bucket)
subagent_all = defaultdict(new_bucket)
subagent_today = defaultdict(new_bucket)
subagent_7d = defaultdict(new_bucket)
subagent_30d = defaultdict(new_bucket)

# Daily cost/token tracking for charts
daily_costs = defaultdict(lambda: defaultdict(float))  # date -> model -> cost
daily_tokens = defaultdict(lambda: defaultdict(int))    # date -> model -> tokens
daily_calls = defaultdict(lambda: defaultdict(int))     # date -> model -> calls
daily_subagent_costs = defaultdict(float)               # date -> cost
daily_subagent_count = defaultdict(int)                 # date -> run count

date_7d = (now - timedelta(days=7)).strftime('%Y-%m-%d')
date_30d = (now - timedelta(days=30)).strftime('%Y-%m-%d')

# Sub-agent activity tracking
subagent_runs = []

for f in glob.glob(os.path.join(base, '*/sessions/*.jsonl')) + glob.glob(os.path.join(base, '*/sessions/*.jsonl.deleted.*')):
    sid = os.path.basename(f).replace('.jsonl', '')
    session_key = None
    # Find session key for this sid
    for store_file in glob.glob(os.path.join(base, '*/sessions/sessions.json')):
        try:
            store = json.load(open(store_file))
            for k, v in store.items():
                if v.get('sessionId') == sid:
                    session_key = k
                    break
        except: pass
        if session_key: break
    is_subagent = 'subagent:' in (session_key or '') or sid not in known_sids

    session_cost = 0
    session_model = ''
    session_first_ts = None
    session_last_ts = None
    session_task = session_key or sid[:12]

    try:
        with open(f) as fh:
            for line in fh:
                try:
                    obj = json.loads(line)
                    msg = obj.get('message', {})
                    if msg.get('role') != 'assistant': continue
                    usage = msg.get('usage', {})
                    if not usage or usage.get('totalTokens', 0) == 0: continue
                    model = msg.get('model', 'unknown')
                    if 'delivery-mirror' in model: continue

                    name = model_name(model)
                    cost_total = usage.get('cost',{}).get('total',0) if isinstance(usage.get('cost'),dict) else 0
                    inp = usage.get('input',0)
                    out = usage.get('output',0)
                    cr = usage.get('cacheRead',0)
                    tt = usage.get('totalTokens',0)

                    models_all[name]['calls'] += 1
                    models_all[name]['input'] += inp
                    models_all[name]['output'] += out
                    models_all[name]['cacheRead'] += cr
                    models_all[name]['totalTokens'] += tt
                    models_all[name]['cost'] += cost_total

                    if is_subagent:
                        subagent_all[name]['calls'] += 1
                        subagent_all[name]['input'] += inp
                        subagent_all[name]['output'] += out
                        subagent_all[name]['cacheRead'] += cr
                        subagent_all[name]['totalTokens'] += tt
                        subagent_all[name]['cost'] += cost_total
                        session_cost += cost_total
                        session_model = name

                    ts = obj.get('timestamp','')
                    try:
                        msg_dt = datetime.fromisoformat(ts.replace('Z','+00:00')).astimezone(local_tz)
                        msg_date = msg_dt.strftime('%Y-%m-%d')
                        if not session_first_ts: session_first_ts = msg_dt
                        session_last_ts = msg_dt
                    except: msg_date = ''

                    # Daily tracking for charts
                    if msg_date:
                        daily_costs[msg_date][name] += cost_total
                        daily_tokens[msg_date][name] += tt
                        daily_calls[msg_date][name] += 1
                        if is_subagent:
                            daily_subagent_costs[msg_date] += cost_total

                    def add_bucket(bucket, n, i, o, c2, t, ct):
                        bucket[n]['calls'] += 1
                        bucket[n]['input'] += i
                        bucket[n]['output'] += o
                        bucket[n]['cacheRead'] += c2
                        bucket[n]['totalTokens'] += t
                        bucket[n]['cost'] += ct

                    if msg_date == today_str:
                        add_bucket(models_today, name, inp, out, cr, tt, cost_total)
                        if is_subagent:
                            add_bucket(subagent_today, name, inp, out, cr, tt, cost_total)

                    if msg_date >= date_7d:
                        add_bucket(models_7d, name, inp, out, cr, tt, cost_total)
                        if is_subagent:
                            add_bucket(subagent_7d, name, inp, out, cr, tt, cost_total)

                    if msg_date >= date_30d:
                        add_bucket(models_30d, name, inp, out, cr, tt, cost_total)
                        if is_subagent:
                            add_bucket(subagent_30d, name, inp, out, cr, tt, cost_total)
                except: pass
    except: pass

    if is_subagent and session_cost > 0 and session_last_ts:
        duration_s = (session_last_ts - session_first_ts).total_seconds() if session_first_ts and session_last_ts else 0
        subagent_runs.append({
            'task': session_task[:60],
            'model': session_model,
            'cost': round(session_cost, 4),
            'durationSec': int(duration_s),
            'status': 'completed',
            'timestamp': session_last_ts.strftime('%Y-%m-%d %H:%M'),
            'date': session_last_ts.strftime('%Y-%m-%d')
        })

subagent_runs.sort(key=lambda x: x.get('timestamp',''), reverse=True)
subagent_runs_today = [r for r in subagent_runs if r.get('date') == today_str]
subagent_runs_7d = [r for r in subagent_runs if r.get('date','') >= date_7d]
subagent_runs_30d = [r for r in subagent_runs if r.get('date','') >= date_30d]

# Count subagent runs per day
for r in subagent_runs:
    d = r.get('date','')
    if d: daily_subagent_count[d] += 1

# ── Build daily chart data (last 30 days) ──
chart_dates = [(now - timedelta(days=i)).strftime('%Y-%m-%d') for i in range(29, -1, -1)]
all_chart_models = set()
for d in chart_dates:
    all_chart_models.update(daily_costs.get(d, {}).keys())
# Sort by total cost descending, keep top 6 + "Other"
model_totals_30d = defaultdict(float)
for d in chart_dates:
    for m, c in daily_costs.get(d, {}).items():
        model_totals_30d[m] += c
top_chart_models = sorted(model_totals_30d.keys(), key=lambda m: -model_totals_30d[m])[:6]

daily_chart = []
for d in chart_dates:
    day_models = daily_costs.get(d, {})
    day_tokens_map = daily_tokens.get(d, {})
    day_calls_map = daily_calls.get(d, {})
    entry = {
        'date': d,
        'label': d[5:],  # MM-DD
        'total': round(sum(day_models.values()), 2),
        'tokens': sum(day_tokens_map.values()),
        'calls': sum(day_calls_map.values()),
        'subagentCost': round(daily_subagent_costs.get(d, 0), 2),
        'subagentRuns': daily_subagent_count.get(d, 0),
        'models': {}
    }
    for m in top_chart_models:
        entry['models'][m] = round(day_models.get(m, 0), 4)
    other = sum(c for m, c in day_models.items() if m not in top_chart_models)
    if other > 0:
        entry['models']['Other'] = round(other, 4)
    daily_chart.append(entry)


def fmt(n):
    if n >= 1_000_000: return f'{n/1_000_000:.1f}M'
    if n >= 1_000: return f'{n/1_000:.1f}K'
    return str(n)

def to_list(d):
    return [{'model':k,'calls':v['calls'],'input':fmt(v['input']),'output':fmt(v['output']),
             'cacheRead':fmt(v['cacheRead']),'totalTokens':fmt(v['totalTokens']),'cost':round(v['cost'],2),
             'inputRaw':v['input'],'outputRaw':v['output'],'cacheReadRaw':v['cacheRead'],'totalTokensRaw':v['totalTokens']}
            for k,v in sorted(d.items(), key=lambda x:-x[1]['cost'])]

# ── Git log ──
git_log = []
try:
    result = subprocess.run(['git', '-C', openclaw_path, 'log', '--oneline', '-5', '--format=%h|%s|%ar'],
                          capture_output=True, text=True)
    for line in result.stdout.strip().split('\n'):
        if '|' in line:
            parts = line.split('|', 2)
            git_log.append({'hash': parts[0], 'message': parts[1], 'ago': parts[2] if len(parts)>2 else ''})
except: pass

# ── Alerts ──
alerts = []
total_cost_today = sum(v['cost'] for v in models_today.values())
total_cost_all = sum(v['cost'] for v in models_all.values())

if total_cost_today > COST_THRESHOLD_HIGH:
    alerts.append({'type': 'warning', 'icon': '💰', 'message': f'High daily cost: ${total_cost_today:.2f}', 'severity': 'high'})
elif total_cost_today > COST_THRESHOLD_WARN:
    alerts.append({'type': 'info', 'icon': '💵', 'message': f'Daily cost above ${COST_THRESHOLD_WARN}: ${total_cost_today:.2f}', 'severity': 'medium'})

for c in crons:
    if c.get('lastStatus') == 'error':
        alerts.append({'type': 'error', 'icon': '❌', 'message': f'Cron failed: {c["name"]}', 'severity': 'high'})

for s in sessions_list:
    if s.get('contextPct', 0) > CONTEXT_THRESHOLD:
        alerts.append({'type': 'warning', 'icon': '⚠️', 'message': f'High context: {s["name"][:30]} ({s["contextPct"]}%)', 'severity': 'medium'})

if gateway['status'] == 'offline':
    alerts.append({'type': 'error', 'icon': '🔴', 'message': 'Gateway is offline', 'severity': 'critical'})

if gateway.get('rss', 0) > MEMORY_THRESHOLD_KB:
    alerts.append({'type': 'warning', 'icon': '🧠', 'message': f'High memory usage: {gateway["memory"]}', 'severity': 'medium'})

# ── Cost breakdown by model (for pie chart) ──
cost_breakdown = []
for name, bucket in sorted(models_all.items(), key=lambda x: -x[1]['cost']):
    if bucket['cost'] > 0:
        cost_breakdown.append({'model': name, 'cost': round(bucket['cost'], 2)})

cost_breakdown_today = []
for name, bucket in sorted(models_today.items(), key=lambda x: -x[1]['cost']):
    if bucket['cost'] > 0:
        cost_breakdown_today.append({'model': name, 'cost': round(bucket['cost'], 2)})

# ── Projected monthly cost ──
day_of_month = now.day
if day_of_month > 0:
    # Simple projection based on days elapsed
    days_in_month = 30
    projected = (total_cost_all / max(day_of_month, 1)) * days_in_month  # rough
    # Better: use today's cost * 30
    projected_from_today = total_cost_today * 30
else:
    projected = 0
    projected_from_today = 0


output = {
    'botName': bot_name,
    'botEmoji': bot_emoji,
    'lastRefresh': now.strftime('%Y-%m-%d %H:%M:%S GMT+8'),
    'lastRefreshMs': int(now.timestamp() * 1000),

    # Gateway health
    'gateway': gateway,
    'compactionMode': compaction_mode,

    # Costs
    'totalCostToday': round(total_cost_today, 2),
    'totalCostAllTime': round(total_cost_all, 2),
    'projectedMonthly': round(projected_from_today, 2),
    'costBreakdown': cost_breakdown,
    'costBreakdownToday': cost_breakdown_today,

    # Sessions
    'sessions': sessions_list,
    'sessionCount': len(known_sids),

    # Crons
    'crons': crons,

    # Sub-agents
    'subagentRuns': subagent_runs[:30],
    'subagentRunsToday': subagent_runs_today[:20],
    'subagentRuns7d': subagent_runs_7d[:50],
    'subagentRuns30d': subagent_runs_30d[:100],
    'subagentCostAllTime': round(sum(v['cost'] for v in subagent_all.values()), 2),
    'subagentCostToday': round(sum(v['cost'] for v in subagent_today.values()), 2),
    'subagentCost7d': round(sum(v['cost'] for v in subagent_7d.values()), 2),
    'subagentCost30d': round(sum(v['cost'] for v in subagent_30d.values()), 2),

    # Token usage
    'tokenUsage': to_list(models_all),
    'tokenUsageToday': to_list(models_today),
    'tokenUsage7d': to_list(models_7d),
    'tokenUsage30d': to_list(models_30d),
    'subagentUsage': to_list(subagent_all),
    'subagentUsageToday': to_list(subagent_today),
    'subagentUsage7d': to_list(subagent_7d),
    'subagentUsage30d': to_list(subagent_30d),

    # Charts (daily breakdown, last 30 days)
    'dailyChart': daily_chart,

    # Models & skills
    'availableModels': available_models,
    'skills': skills,

    # Git log
    'gitLog': git_log,

    # Alerts
    'alerts': alerts,

}

print(json.dumps(output, indent=2))
PYEOF

if [ -s "$DIR/data.json.tmp" ]; then
    mv "$DIR/data.json.tmp" "$DIR/data.json"
    echo "✅ data.json refreshed at $(date '+%Y-%m-%d %H:%M:%S')"
else
    rm -f "$DIR/data.json.tmp"
    echo "❌ refresh failed"
    exit 1
fi
