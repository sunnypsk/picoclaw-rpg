# PROVIDERS

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`pkg/providers` maps config models to concrete LLM providers, protocol adapters, CLI-backed
providers, fallback chains, media support, and error classification.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Runtime provider creation | `legacy_provider.go` | `CreateProvider(cfg)` entry used by gateway. |
| `model_list` factory | `factory_provider.go` | `CreateProviderFromConfig`. |
| Legacy/inferred routing | `factory.go` | Provider selection and fallback from old config fields. |
| Fallback/cooldown | `fallback.go`, `cooldown.go` | Model failover behavior. |
| Provider contracts | `types.go`, `protocoltypes/` | `LLMProvider`, message/tool-call response types. |
| OpenAI-compatible path | `http_provider.go`, `openai_compat/` | API-key/base/proxy adapters. |
| CLI-backed providers | `codex_cli_provider.go`, `claude_cli_provider.go` | Workspace-sensitive provider paths. |

## CONVENTIONS
- Prefer `model_list` for new model config; legacy `providers` remains compatibility input.
- `agents.defaults.model_name` resolves to `model_list[].model_name`; do not treat it as always
  equal to provider model ID.
- OpenRouter-style provider-prefixed model IDs such as `openrouter/...` are meaningful.
- CLI integration tests are opt-in: `-tags=integration` or `PICOCLAW_INTEGRATION_TESTS=1`.
- `github_copilot_provider.go` currently has a known gap for unimplemented `stdio` mode.

## ANTI-PATTERNS
- Do not remove old provider fallback behavior without migration tests.
- Do not print API keys, OAuth tokens, client secrets, or private API bases in errors/logs/docs.
- Do not add a provider path without tests for missing credentials and protocol extraction.
- Do not assume all providers support message media or tool calls equally; check interfaces/tests.

## TESTS
```bash
go test ./pkg/providers -v
go test ./pkg/providers -run TestCreateProvider -v
go test ./pkg/providers -run TestE2E -v
go test ./pkg/providers -bench=. -benchmem
go test -tags=integration ./pkg/providers/...
```
