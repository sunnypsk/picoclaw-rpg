# TUI LAUNCHER

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`cmd/picoclaw-launcher-tui` is the terminal launcher built with `tview`/`tcell`.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| App entry | `main.go` | Calls `ui.Run()`. |
| UI state and navigation | `internal/ui/app.go`, `internal/ui/menu.go` | `appState` owns pages, dirty state, config, process. |
| Config load/save | `internal/config/store.go` | Uses `pkg/config`. |
| Gateway process control | `internal/ui/gateway_*.go` | Platform-specific build tags for Windows vs POSIX. |

## CONVENTIONS
- Keep platform behavior behind `gateway_windows.go` and `gateway_posix.go`.
- TUI state tracks original config bytes, backup path, dirty state, log path, and gateway process.
- Prefer small menu/action changes over broad UI rewrites; there is no snapshot UI test harness.
- Reuse `pkg/config` load/save semantics rather than creating parallel config parsing.

## ANTI-PATTERNS
- Do not break Windows/POSIX separation when changing process launch behavior.
- Do not write config directly from multiple places without updating `dirty`/backup handling.
- Do not assume ANSI rendering is enough; this tree depends on `tview` widgets.

## TESTS
```bash
go test ./cmd/picoclaw-launcher-tui/... -v
go build ./cmd/picoclaw-launcher-tui
```
