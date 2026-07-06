# CHANNEL DOCUMENTATION

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`docs/channels` contains user-facing setup guides for channel integrations. It mirrors many
packages under `pkg/channels`.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Telegram docs | `telegram/README.zh.md` | Match `pkg/channels/telegram` config fields. |
| Discord docs | `discord/README.zh.md` | Match `pkg/channels/discord`. |
| WeCom docs | `wecom/wecom_*` | App, bot, and AI bot modes differ. |
| WhatsApp docs | `whatsapp/README.md` | Bridge/native behavior must match config. |
| Other platforms | `<platform>/README.zh.md` | DingTalk, Feishu, LINE, MaixCam, OneBot, QQ, Slack. |

## CONVENTIONS
- Keep config keys aligned with `pkg/config/config.go` and `config/config.example.json`.
- Secret fields in docs must be placeholders such as `YOUR_CLIENT_SECRET`; never include real
  tokens, webhook URLs, corp secrets, or private endpoints.
- When channel code changes required fields, update the matching doc in the same change.
- Prefer platform-specific setup steps over generic channel prose.

## ANTI-PATTERNS
- Do not document deprecated config paths as preferred setup.
- Do not let docs drift from channel package names and config field names.
- Do not copy live console credentials or screenshots containing secrets into docs.
- Do not update `docs/channels` alone when runtime behavior or config schema also changed.

## VALIDATION
```bash
rg -n "app_secret|client_secret|channel_secret|corp_secret|token|webhook" docs/channels config/config.example.json
go test ./pkg/channels/... -v
go test ./pkg/config -v
```
