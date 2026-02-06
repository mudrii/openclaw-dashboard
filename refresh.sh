#!/bin/bash
# OpenClaw Dashboard — Data Refresh Script
# Parses JSONL logs and generates data.json

set -e

# Get script directory
DIR="$(cd "$(dirname "$0")" && pwd)"

# Load config if exists
OPENCLAW_PATH="${OPENCLAW_HOME:-$HOME/.openclaw}"
TIMEZONE_OFFSET=0  # Default UTC

if [ -f "$DIR/config.json" ]; then
  CONFIG_PATH=$(python3 -c "import json; c=json.load(open('$DIR/config.json')); print(c.get('openclawPath','~/.openclaw').replace('~','$HOME'))" 2>/dev/null || echo "$HOME/.openclaw")
  OPENCLAW_PATH="${CONFIG_PATH:-$OPENCLAW_PATH}"
  TIMEZONE_OFFSET=$(python3 -c "import json; c=json.load(open('$DIR/config.json')); print(c.get('timezoneOffset', 0))" 2>/dev/null || echo "0")
fi

# Expand ~ if present
OPENCLAW_PATH="${OPENCLAW_PATH/#\~/$HOME}"

echo "Dashboard dir: $DIR"
echo "OpenClaw path: $OPENCLAW_PATH"

# Check if OpenClaw exists
if [ ! -d "$OPENCLAW_PATH" ]; then
  echo "❌ OpenClaw not found at $OPENCLAW_PATH"
  echo "   Set OPENCLAW_HOME or update config.json"
  exit 1
fi

# Find Python
PYTHON=$(command -v python3 || command -v python)
if [ -z "$PYTHON" ]; then
  echo "❌ Python not found"
  exit 1
fi

$PYTHON - "$DIR" "$OPENCLAW_PATH" "$TIMEZONE_OFFSET" << 'PYEOF' > "$DIR/data.json.tmp"
import json, glob, os, sys
from collections import defaultdict
from datetime import datetime, timezone, timedelta

dashboard_dir = sys.argv[1]
openclaw_path = sys.argv[2]
tz_offset = int(sys.argv[3]) if len(sys.argv) > 3 else 0

# Use configured timezone or system local
if tz_offset != 0:
    local_tz = timezone(timedelta(hours=tz_offset))
else:
    # Use system local timezone
    local_tz = datetime.now().astimezone().tzinfo

now = datetime.now(local_tz)
today_str = now.strftime('%Y-%m-%d')

base = os.path.join(openclaw_path, "agents")
config_path = os.path.join(openclaw_path, "openclaw.json")
cron_path = os.path.join(openclaw_path, "cron/jobs.json")

# Load bot config from dashboard config
bot_name = "OpenClaw Bot"
bot_emoji = "🦞"
dashboard_config = os.path.join(dashboard_dir, "config.json")
if os.path.exists(dashboard_config):
    try:
        dc = json.load(open(dashboard_config))
        bot_name = dc.get('bot', {}).get('name', bot_name)
        bot_emoji = dc.get('bot', {}).get('emoji', bot_emoji)
    except: pass

# Load session stores to identify which JSONLs belong to known sessions
known_sids = {}
for store_file in glob.glob(os.path.join(base, '*/sessions/sessions.json')):
    try:
        store = json.load(open(store_file))
        for key, val in store.items():
            sid = val.get('sessionId', '')
            if not sid: continue
            if 'cron:' in key: stype = 'cron'
            elif 'group:' in key: stype = 'group'
            elif 'whatsapp' in key: stype = 'whatsapp'
            elif 'telegram' in key: stype = 'telegram'
            elif key.endswith(':main'): stype = 'main'
            else: stype = 'other'
            known_sids[sid] = stype
    except: pass

def model_name(model):
    ml = model.lower()
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
    elif 'k2p5' in ml or 'kimi' in ml: return 'Kimi K2.5'
    elif 'gpt-5' in ml: return 'GPT-5'
    elif 'gpt-4o' in ml: return 'GPT-4o'
    elif 'gpt-4' in ml: return 'GPT-4'
    elif 'o1' in ml: return 'O1'
    elif 'o3' in ml: return 'O3'
    else: return model

def new_bucket():
    return {'calls':0,'input':0,'output':0,'cacheRead':0,'totalTokens':0,'cost':0.0}

# Main counters
models_all = defaultdict(new_bucket)
models_today = defaultdict(new_bucket)
subagent_all = defaultdict(new_bucket)
subagent_today = defaultdict(new_bucket)

for f in glob.glob(os.path.join(base, '*/sessions/*.jsonl')):
    sid = os.path.basename(f).replace('.jsonl', '')
    is_subagent = sid not in known_sids

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

                    ts = obj.get('timestamp','')
                    try:
                        msg_date = datetime.fromisoformat(ts.replace('Z','+00:00')).astimezone(local_tz).strftime('%Y-%m-%d')
                    except: msg_date = ''

                    if msg_date == today_str:
                        models_today[name]['calls'] += 1
                        models_today[name]['input'] += inp
                        models_today[name]['output'] += out
                        models_today[name]['cacheRead'] += cr
                        models_today[name]['totalTokens'] += tt
                        models_today[name]['cost'] += cost_total

                        if is_subagent:
                            subagent_today[name]['calls'] += 1
                            subagent_today[name]['input'] += inp
                            subagent_today[name]['output'] += out
                            subagent_today[name]['cacheRead'] += cr
                            subagent_today[name]['totalTokens'] += tt
                            subagent_today[name]['cost'] += cost_total
                except: pass
    except: pass

def fmt(n):
    if n >= 1_000_000: return f'{n/1_000_000:.1f}M'
    if n >= 1_000: return f'{n/1_000:.1f}K'
    return str(n)

def to_list(d):
    return [{'model':k,'calls':v['calls'],'input':fmt(v['input']),'output':fmt(v['output']),
             'cacheRead':fmt(v['cacheRead']),'totalTokens':fmt(v['totalTokens']),'cost':round(v['cost'],2)}
            for k,v in sorted(d.items(), key=lambda x:-x[1]['cost'])]

# Load skills from openclaw.json
skills = []
available_models = []
if os.path.exists(config_path):
    try:
        with open(config_path) as cf:
            oc = json.load(cf)
        # Skills
        skill_entries = oc.get('skills', {}).get('entries', {})
        for name, conf in skill_entries.items():
            enabled = conf.get('enabled', True) if isinstance(conf, dict) else True
            skills.append({'name': name, 'active': enabled, 'type': 'builtin'})
        # Models
        model_aliases = oc.get('agents', {}).get('defaults', {}).get('models', {})
        primary = oc.get('agents', {}).get('defaults', {}).get('model', {}).get('primary', '')
        for mid, mconf in model_aliases.items():
            status = 'active' if mid == primary else 'available'
            provider = mid.split('/')[0] if '/' in mid else 'unknown'
            available_models.append({
                'provider': provider.title(),
                'name': mconf.get('alias', mid),
                'id': mid,
                'status': status
            })
    except: pass

# Load crons from jobs.json
crons = []
if os.path.exists(cron_path):
    try:
        jobs = json.load(open(cron_path)).get('jobs', [])
        for job in jobs:
            sched = job.get('schedule', {})
            kind = sched.get('kind', '')
            if kind == 'cron':
                schedule_str = sched.get('expr', '')
            elif kind == 'every':
                ms = sched.get('everyMs', 0)
                if ms >= 86400000: schedule_str = f"Every {ms // 86400000}d"
                elif ms >= 3600000: schedule_str = f"Every {ms // 3600000}h"
                elif ms >= 60000: schedule_str = f"Every {ms // 60000}m"
                else: schedule_str = f"Every {ms}ms"
            elif kind == 'at':
                schedule_str = sched.get('at', '')[:16]
            else:
                schedule_str = str(sched)
            
            crons.append({
                'name': job.get('name', job.get('id', 'Unknown')),
                'schedule': schedule_str,
                'enabled': job.get('enabled', True),
                'next': '',
                'lastRun': ''
            })
    except: pass

# Load existing data.json to preserve manual entries (kanban tasks)
existing = {}
data_path = os.path.join(dashboard_dir, 'data.json')
if os.path.exists(data_path):
    try:
        with open(data_path) as ef:
            existing = json.load(ef)
    except: pass

# Build output - preserve kanban tasks from existing
output = {
    'botName': bot_name,
    'botEmoji': bot_emoji,
    'workingOn': existing.get('workingOn', 'Ready'),
    'workingOnMeta': existing.get('workingOnMeta', ''),
    'sessionsMeta': f'{len(known_sids)} tracked',
    'cronsMeta': f'{len(crons)} scheduled',
    'skillCount': len([s for s in skills if s.get('active')]),
    'skillsMeta': f'{len(skills)} total',
    'nextScheduled': '—',
    'nextScheduledMeta': '',
    # Preserve kanban tasks
    'inProgress': existing.get('inProgress', []),
    'queue': existing.get('queue', []),
    'waiting': existing.get('waiting', []),
    'doneToday': existing.get('doneToday', []),
    'sessions': [],
    'crons': crons,
    'models': [],
    'availableModels': available_models,
    'skills': skills,
    'tokenUsage': to_list(models_all),
    'tokenUsageToday': to_list(models_today),
    'subagentUsage': to_list(subagent_all),
    'subagentUsageToday': to_list(subagent_today),
    'lastRefresh': now.strftime('%Y-%m-%d %H:%M:%S %Z'),
    'totalCostAllTime': round(sum(v['cost'] for v in models_all.values()), 2),
    'totalCostToday': round(sum(v['cost'] for v in models_today.values()), 2),
    'subagentCostAllTime': round(sum(v['cost'] for v in subagent_all.values()), 2),
    'subagentCostToday': round(sum(v['cost'] for v in subagent_today.values()), 2)
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
