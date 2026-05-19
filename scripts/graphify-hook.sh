#!/usr/bin/env bash
# Shared graphify hook for Claude Code and Gemini CLI.
set -u

MODE=""
if [ "${1:-}" = "--mode" ] && [ -n "${2:-}" ]; then
  MODE="$2"
else
  exit 1
fi

CTX='graphify: knowledge graph at graphify-out/. For focused questions, run `graphify query "<question>"` (scoped subgraph, usually much smaller than GRAPH_REPORT.md) instead of grepping raw files. Read GRAPH_REPORT.md only for broad architecture context.'

case "$MODE" in
  claude)
    # Extract tool_input.command from JSON on stdin via python3.
    CMD=$(python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('tool_input',d).get('command',''))" 2>/dev/null || true)
    case "$CMD" in
      *grep*|*rg\ *|*ripgrep*|*find\ *|*fd\ *|*ack\ *|*ag\ *)
        if [ -f graphify-out/graph.json ]; then
          CTX="$CTX" python3 -c "import json,os,sys; sys.stdout.write(json.dumps({'hookSpecificOutput':{'hookEventName':'PreToolUse','additionalContext':os.environ['CTX']}}))"
        fi
        ;;
    esac
    exit 0
    ;;
  gemini)
    if [ -f graphify-out/graph.json ]; then
      CTX="$CTX" python3 -c "import json,os,sys; sys.stdout.write(json.dumps({'decision':'allow','additionalContext':os.environ['CTX']}))"
    else
      printf '%s' '{"decision":"allow"}'
    fi
    exit 0
    ;;
  *)
    exit 1
    ;;
esac
