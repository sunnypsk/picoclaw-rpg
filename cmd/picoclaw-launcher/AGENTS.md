# WEB LAUNCHER

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`cmd/picoclaw-launcher` is a standalone web launcher for config editing, auth, and process control.
Its README explicitly says the API is temporary and not stable.

## STRUCTURE
```text
cmd/picoclaw-launcher/
|-- main.go
|-- README.md
|-- internal/
|   |-- server/             # HTTP APIs for config/auth/process/logs
|   `-- ui/index.html       # embedded frontend, i18n strings, forms
`-- winres/                 # Windows resource inputs
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Config API | `internal/server/server.go` | `GET/PUT /api/config`. |
| Auth API | `internal/server/auth_handlers.go` | Device code, token, browser OAuth flows. |
| Auth config mutation | `internal/server/auth_config.go` | Writes provider auth settings. |
| Process control | `internal/server/process.go` | Starts/stops PicoClaw process. |
| UI fields/i18n | `internal/ui/index.html` | Single embedded file; no frontend build step. |

## CONVENTIONS
- Default UI address is `http://localhost:18800`; `-public` listens on all interfaces.
- Request/response JSON is documented in `README.md`; update docs when API shape changes.
- The frontend manages model availability, required/optional fields, and language/theme state in
  one HTML file.
- Provider auth supports `openai`, `anthropic`, and `google-antigravity` paths.
- Secrets shown in docs must stay placeholders; never copy real token values into examples.

## ANTI-PATTERNS
- Do not treat launcher HTTP APIs as stable public contracts without updating the warning/docs.
- Do not split the frontend into a Node build unless the task explicitly changes build strategy.
- Do not log or echo provider tokens, device codes beyond intended user flow, or OAuth secrets.
- Do not make config writes partial unless server-side code preserves the full config object safely.

## TESTS
```bash
go test ./cmd/picoclaw-launcher/... -v
go build ./cmd/picoclaw-launcher
```
