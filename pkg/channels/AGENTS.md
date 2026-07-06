# CHANNELS

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`pkg/channels` is the platform integration framework for Telegram, Discord, Slack, LINE, WeCom,
DingTalk, Feishu, WhatsApp, QQ, OneBot, Pico, MaixCam, and related message/media flows.

## STRUCTURE
```text
pkg/channels/
|-- base.go, interfaces.go, media.go, webhook.go
|-- registry.go             # RegisterFactory/getFactory
|-- manager.go              # queues, retries, shared HTTP, placeholders, media
|-- split.go                # long-message splitting
`-- <platform>/             # platform implementation plus init.go factory
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add a channel | `<platform>/init.go`, `<platform>/<platform>.go` | Register with `channels.RegisterFactory`. |
| Shared behavior | `base.go`, `interfaces.go` | Optional capabilities are interface-based. |
| Outbound orchestration | `manager.go` | Rate limit, retry, typing/reaction/placeholder cleanup. |
| Message bus structs | `pkg/bus/` | `InboundMessage`, `OutboundMessage`, media/reaction messages. |
| Media lifecycle | `pkg/media/` | File media store and TTL cleanup. |
| Identity matching | `pkg/identity/` | Canonical `platform:id` helpers. |
| Channel docs | `docs/channels/<platform>/` | Keep user setup docs aligned. |

## CONVENTIONS
- Each platform package should be isolated and import the parent `channels` package for shared
  contracts.
- Optional features are discovered by type assertion: `MediaSender`, `TypingCapable`,
  `ReactionCapable`, `PlaceholderCapable`, `MessageEditor`, `WebhookHandler`, `HealthChecker`.
- Manager queue size is `16`; default rate limit is `10 msg/s`, with channel-specific overrides.
- Feishu has architecture build tags; WhatsApp native requires `whatsapp_native`.
- Use `httptest` and local fake servers in tests instead of external platform calls.

## ANTI-PATTERNS
- Do not add manager `switch` chains for new channels; use factory registration.
- Do not start separate HTTP servers per webhook channel unless the architecture changes.
- Do not bury routing identity in unstructured metadata when first-class bus fields exist.
- Do not ignore sentinel errors such as `ErrRateLimit` and `ErrTemporary`; retry policy depends on them.

## TESTS
```bash
go test ./pkg/channels -v
go test ./pkg/channels/... -v
go test ./pkg/channels -run TestManager -v
go test -tags=whatsapp_native ./pkg/channels/whatsapp_native -v
```
