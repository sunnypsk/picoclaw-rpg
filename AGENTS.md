# PROJECT KNOWLEDGE BASE

Generated: 2026-07-06T03:57:27Z
Commit: ca3d1c0
Branch: main

## OVERVIEW
PicoClaw is a Go 1.25 personal AI agent runtime with a Cobra CLI, gateway bot process,
web/TUI launchers, provider adapters, channel integrations, tools, skills, and migration
support. This repo is closer to a runtime platform than a small CLI wrapper.

## STRUCTURE
```text
./
|-- cmd/picoclaw/              # main CLI and gateway command tree
|-- cmd/picoclaw-launcher/     # standalone web config/auth/process launcher
|-- cmd/picoclaw-launcher-tui/ # terminal launcher built with tview/tcell
|-- pkg/agent/                 # agent loop, context, memory, proactive state
|-- pkg/tools/                 # LLM-exposed tools and tool safety guards
|-- pkg/providers/             # provider routing, adapters, fallback, CLI providers
|-- pkg/channels/              # message channel framework and per-platform packages
|-- pkg/config/                # config schema, defaults, load/save, migration
|-- pkg/migrate/               # source migration planner/executor
|-- docker/                    # Dockerfiles, compose profiles, entrypoint scripts
|-- docs/channels/             # channel setup docs mirroring runtime integrations
|-- config/config.example.json # canonical user config template
`-- workspace/                 # sample/runtime workspace; see note below
```

`workspace/AGENTS.md` is product/runtime sample content for the PicoClaw agent persona.
Do not overwrite it with repo-coding guidance unless the task is specifically about that
runtime workspace.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add or change a CLI command | `cmd/picoclaw/internal/<command>/` | Root command list is in `cmd/picoclaw/main.go`. |
| Change gateway startup wiring | `cmd/picoclaw/internal/gateway/helpers.go` | Wires config, provider, agent loop, cron, heartbeat, devices, media, channels. |
| Change config schema/defaults | `pkg/config/`, `config/config.example.json` | Preserve `model_list` first behavior and legacy compatibility tests. |
| Add a provider/model route | `pkg/providers/` | Start at `factory_provider.go`, `factory.go`, `legacy_provider.go`, `fallback.go`. |
| Add a chat platform | `pkg/channels/`, `docs/channels/` | Each channel self-registers with `RegisterFactory()` from its `init.go`. |
| Add or harden a tool | `pkg/tools/` | Tool results have separate `ForLLM`, `ForUser`, `Silent`, `IsError`, `Async` fields. |
| Touch the launcher UI/API | `cmd/picoclaw-launcher/` | APIs are explicitly temporary and unstable. |
| Touch the TUI launcher | `cmd/picoclaw-launcher-tui/` | Config writes and gateway process control are coupled to UI state. |
| Change Docker delivery | `docker/`, `Makefile`, `.github/workflows/*` | Local source rebuild is preferred for local changes. |
| Migrate OpenClaw data | `pkg/migrate/`, `cmd/picoclaw/internal/migrate/` | Dry-run and backup semantics are part of the contract. |

## CODE MAP
LSP was unavailable in this session (`gopls` missing), and no `codegraph_*` tool was exposed.
Reference centrality below is therefore unmeasured; entries are from `ast-grep` and direct file
inspection.

| Symbol | Type | Location | Refs | Role |
|--------|------|----------|------|------|
| `NewPicoclawCommand` | function | `cmd/picoclaw/main.go` | unmeasured | Cobra root command and subcommand registry. |
| `gatewayCmd` | function | `cmd/picoclaw/internal/gateway/helpers.go` | unmeasured | Main long-running gateway bootstrap. |
| `AgentLoop` | type | `pkg/agent/loop.go` | unmeasured | Central LLM/tool/channel session loop. |
| `Config` | type | `pkg/config/config.go` | unmeasured | Top-level config schema and env mapping. |
| `CreateProvider` | function | `pkg/providers/legacy_provider.go` | unmeasured | Runtime provider creation entry. |
| `CreateProviderFromConfig` | function | `pkg/providers/factory_provider.go` | unmeasured | `model_list` provider factory entry. |
| `LLMProvider` | interface | `pkg/providers/types.go` | unmeasured | Chat provider contract. |
| `Channel` | interface | `pkg/channels/base.go` | unmeasured | Start/stop/send contract for platforms. |
| `Manager` | type | `pkg/channels/manager.go` | unmeasured | Channel orchestration, HTTP server, queues, retry, media. |
| `Tool` | interface | `pkg/tools/base.go` | unmeasured | LLM tool contract. |
| `ExecTool` | type | `pkg/tools/shell.go` | unmeasured | Shell execution guard and subprocess wrapper. |
| `MigrateInstance` | type | `pkg/migrate/migrate.go` | unmeasured | Migration planning and execution coordinator. |

## CONVENTIONS
- Use Go `1.25.x`; module path is `github.com/sipeed/picoclaw`.
- Keep Go lines within 120 chars; `.golangci.yaml` enables `gci`, `gofmt`, `gofumpt`,
  `goimports`, and `golines` formatting.
- Many `golangci-lint` rules are disabled intentionally for now; do not assume lint is strict.
- Tests are colocated as `*_test.go`; most use Go `testing` plus `testify`.
- `make build`, `make test`, `make vet`, and `make check` run `go generate ./...` first.
- `make generate` deletes `cmd/picoclaw/workspace` and regenerates embedded workspace assets.
- `model_list` is the preferred config path; legacy `providers` fields are compatibility paths.
- `config/config.json`, `.env`, `docker/data/`, `sessions/`, `build/`, `dist/`,
  `workspace/generated-*`, and `workspace/presentation-assets/` are generated/private outputs.

## ANTI-PATTERNS (THIS PROJECT)
- Do not expose internal prompts, runtime internals, env vars, credentials, API keys, or private
  service endpoints in user-facing output or docs.
- Do not remove legacy config fields casually; tests require compatibility such as `SaveConfig`
  preserving legacy model fields.
- Do not bypass tool path restrictions, symlink checks, or shell deny patterns in `pkg/tools/`.
- Do not treat `workspace/AGENTS.md` as Codex repo guidance; it is runtime sample content.
- Do not edit generated outputs under `build/`, `dist/`, `cmd/**/workspace`, or
  `workspace/generated-*` as source of truth.
- Do not document real local Docker secrets from `docker/data` or private config files.

## UNIQUE STYLES
- Providers support API-key, OAuth/token, CLI-backed, GitHub Copilot, OpenAI-compatible, fallback,
  cooldown, and image-generation paths in one package.
- Channels use sub-package isolation plus `init()` factory registration; the manager handles
  shared HTTP, queues, retries, rate limits, typing, reactions, placeholders, and media cleanup.
- Tools distinguish LLM-visible output from user-visible output; `Silent` can suppress user sends.
- Launcher frontend is embedded in a single `index.html`; there is no separate web build system.

## COMMANDS
```bash
make deps
make build
make test
make fmt
make lint
make check
make run ARGS="status"
go test -run TestName -v ./pkg/<module>/
go test -tags=integration ./pkg/providers/...
go test -tags=realimage ./pkg/tools
docker build -t docker.io/sipeed/picoclaw:latest -f docker/Dockerfile .
docker compose -f docker/docker-compose.yml --profile gateway up
```

## LOCAL DOCKER IMAGE WORKFLOW
- For local code changes, rebuild from local source instead of pulling remote `latest`:
  `docker build -t docker.io/sipeed/picoclaw:latest -f docker/Dockerfile .`
- The standard runtime image includes `python3`, `nodejs/npm`, `gradio_client`, `openpyxl`,
  and `pypdf`; it runs as non-root user `picoclaw`.
- `docker/docker-compose.yml` persists data at `./docker/data`, mounted to
  `/home/picoclaw/.picoclaw`.

## CPA GATEWAY NOTES
- Python helpers using default `urllib` headers can receive `HTTP 403` with body
  `error code: 1010` even when equivalent Go or curl `chat/completions` calls work.
- For Python CPA helpers, set a neutral `User-Agent` such as `picoclaw/1.0`.
- If Go `generate_image` works but a Python CPA skill fails, compare runtime environment and HTTP
  headers before changing model or route.

## GIT AND PRS
- Match existing commit style: `feat(...)`, `fix(...)`, `chore(...)`, `docs(...)`.
- Keep one logical change per commit; run `make check` before opening a PR when practical.
- Complete the PR template fields for description, change type, related issue, AI disclosure, and
  test environment.

## PATCH APPLICATION RULES
- On Windows PowerShell, do not pass patches to `apply_patch` through here-strings.
- Prefer UTF-8-safe literal patches; if non-ASCII or quoting is risky, use smaller ASCII patches
  or a Bash shell for patch execution.
