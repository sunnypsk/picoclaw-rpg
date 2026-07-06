# TOOL RUNTIME

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`pkg/tools` contains the LLM-exposed tool interfaces, registries, filesystem/shell guards, async
subagents, MCP wrappers, web tools, media tools, skills, cron, and game/tool helpers.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Tool contract | `base.go`, `result.go`, `registry.go` | `Tool`, `AsyncTool`, `ToolResult`, registry execution. |
| Filesystem safety | `filesystem.go`, `edit.go`, `send_file.go` | Workspace restriction and symlink checks. |
| Shell safety | `shell.go`, `shell_process_*.go` | Deny patterns, path guards, OS-specific process handling. |
| Cron/reminders | `cron.go` | Bridges cron jobs to agent execution and message bus. |
| Web tools | `web.go` | Search/fetch behavior and hidden intermediate output. |
| Image/presentation | `generate_image.go`, `generate_presentation.go` | CPA/env handling and generated artifacts. |
| Skills/MCP/subagents | `skills_*.go`, `mcp_tool.go`, `subagent.go`, `spawn.go` | Registry and external tool execution. |

## CONVENTIONS
- `ToolResult.ForLLM` is required; `ForUser` is optional; `Silent` suppresses user delivery.
- `ExecTool` hides raw command output by default; set `show_output` only when user asked for it.
- Filesystem tools must validate absolute paths, relative paths, and symlink targets against the
  workspace when restricted.
- Default shell deny patterns block destructive commands, pipe-to-shell, env substitution,
  privilege escalation, global installs, `docker run/exec`, `git push`, and SSH targets.
- Live image tests use `-tags=realimage` and need `CPA_API_KEY` or `PICOCLAW_HOME`.

## ANTI-PATTERNS
- Do not bypass `validatePath`, `getSafeRelPath`, or `guardCommand`.
- Do not make binary files readable through text file tools.
- Do not expose intermediate web/shell output unless the tool contract says to.
- Do not change `presentation_assets/anime.umd.min.js` as handwritten source; treat it as vendored.
- Do not print real CPA keys or private endpoints while debugging.

## TESTS
```bash
go test ./pkg/tools -v
go test ./pkg/tools -run TestExec -v
go test ./pkg/tools -run TestFilesystem -v
go test ./pkg/tools -run TestGenerateImage -v
go test -tags=realimage ./pkg/tools
```
