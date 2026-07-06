# PICOCLAW CLI COMMANDS

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`cmd/picoclaw` is the main Cobra CLI and the gateway process entrypoint.

## STRUCTURE
```text
cmd/picoclaw/
|-- main.go                 # root command, banner, subcommand registration
|-- main_test.go            # command tree expectations
`-- internal/
    |-- agent/              # direct one-shot agent command
    |-- auth/               # auth command group
    |-- cron/               # reminder/cron command group
    |-- gateway/            # long-running bot runtime
    |-- migrate/            # migration CLI wrapper
    |-- onboard/            # config/workspace bootstrap and go:generate hook
    |-- skills/             # skill management commands
    |-- status/             # status command
    `-- version/            # version command
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add/remove top-level commands | `main.go` | Update `main_test.go` expectations. |
| Change gateway startup | `internal/gateway/helpers.go` | Runtime bootstrap lives here, not in `main.go`. |
| Change onboard workspace generation | `internal/onboard/generate_workspace.go` | Build tag is `ignore`; invoked through `go generate`. |
| Change migration flags | `internal/migrate/command.go` | Delegates to `pkg/migrate`. |
| Change skill CLI | `internal/skills/` | Has the largest CLI test surface in this tree. |

## CONVENTIONS
- Keep command constructors named `New<Thing>Command()` and return `*cobra.Command`.
- Root command currently registers `onboard`, `agent`, `auth`, `gateway`, `status`, `cron`,
  `migrate`, `skills`, and `version`; tests lock this set.
- `gatewayCmd` wires config loading, provider creation, `AgentLoop`, cron, heartbeat, devices,
  media store, channel manager, health server, and graceful shutdown.
- Gateway imports channel packages with blank imports so their `init()` functions register
  factories. Removing one silently disables that channel family.
- Build metadata is injected with `LDFLAGS` from `Makefile` into `cmd/picoclaw/internal`.

## ANTI-PATTERNS
- Do not move runtime wiring into `main.go`; keep it in command packages.
- Do not add a command without a focused command-shape test.
- Do not bypass `internal.LoadConfig()` for CLI paths that should honor standard config lookup.
- Do not edit generated `cmd/picoclaw/workspace` directly; use `go generate ./...`.

## TESTS
```bash
go test ./cmd/picoclaw/... -v
go test ./cmd/picoclaw -run TestNewPicoclawCommand -v
go test ./cmd/picoclaw/internal/gateway -v
```
