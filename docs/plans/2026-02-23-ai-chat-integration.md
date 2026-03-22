# AI Chat Integration Historical Note

This feature is implemented and maintained in the Go codebase only.

## Current authoritative files

- `chat.go`
- `server.go`
- `config.go`
- `index.html`
- `chat_test.go`
- `server_test.go`

## Current behavior

- `POST /api/chat` is served by the Go binary.
- The handler reads `data.json`, builds a prompt, and forwards requests to the
  OpenClaw gateway's OpenAI-compatible chat completions endpoint.
- Authentication uses `OPENCLAW_GATEWAY_TOKEN` from the configured dotenv file.
- Configuration lives in `config.json`.

## Why this file is short

The earlier implementation plan in this path described a retired Python server.
That content no longer reflects the repository and was removed to keep the docs
Go-only and avoid conflicting implementation guidance. Git history retains the
full migration-era plan if it is ever needed for archaeology.
