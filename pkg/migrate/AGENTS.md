# MIGRATION

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`pkg/migrate` plans and executes migrations from external/source installations into PicoClaw.
The current built-in source adapter is OpenClaw.

## STRUCTURE
```text
pkg/migrate/
|-- migrate.go              # instance, planning, execute, confirmation, plan printing
|-- internal/               # options, action/result types, file helpers
`-- sources/openclaw/       # OpenClaw config/workspace adapter
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add source support | `sources/<source>/` | Implement `internal.Operation`. |
| Change planning | `migrate.go`, `internal/` | Preserve dry-run and backup behavior. |
| Convert OpenClaw config | `sources/openclaw/openclaw_config.go` | Auth profiles are intentionally not migrated. |
| CLI flags | `cmd/picoclaw/internal/migrate/` | Cobra wrapper around this package. |

## CONVENTIONS
- `--config-only` and `--workspace-only` are mutually exclusive.
- `--refresh` implies workspace-only behavior.
- Without `--force`, execution prints the plan and asks for confirmation.
- Existing targets are backed up before overwrite when planned as `ActionBackup`.
- Auth profiles/API keys/OAuth tokens are not migrated for security; users set env vars manually.

## ANTI-PATTERNS
- Do not copy secrets from source config into PicoClaw config.
- Do not execute migration actions during dry-run.
- Do not skip backups for overwrite paths unless force semantics are explicitly redesigned.
- Do not add a source adapter without tests for missing source home, config-only, workspace-only,
  dry-run, and force behavior.

## TESTS
```bash
go test ./pkg/migrate -v
go test ./pkg/migrate/internal -v
go test ./pkg/migrate/sources/openclaw -v
```
