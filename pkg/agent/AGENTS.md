# AGENT RUNTIME

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`pkg/agent` owns the central LLM loop, context construction, session behavior, proactive state,
memory, voice notes, media handling, and scheduled reminder execution.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Main loop lifecycle | `loop.go` | `AgentLoop`, `Run`, `Stop`, direct/heartbeat processing. |
| Prompt/context rules | `context.go`, `instance.go` | Contains hard behavior rules such as always using tools. |
| Media handling | `loop_media.go`, `loop_generate_image_test.go` | Image/file result routing. |
| Proactive behavior | `proactive.go`, `npc_state.go`, `proactive_state.go` | State heartbeat and proactive chat logic. |
| Memory files | `memory.go`, `maintenance_json.go` | Redaction and maintenance JSON behavior. |
| Workspace bootstrap | `bootstrap_sync.go` | Default workspace sync and conflict reporting. |
| Reminders | `scheduled_reminder.go` | Cron tool calls back into this package. |

## CONVENTIONS
- Tests use hand-rolled providers/fakes rather than gomock.
- `AgentLoop` is integration-heavy; prefer focused tests around the changed behavior instead of
  broad rewrites.
- Tool responses distinguish LLM context from user-visible delivery; preserve `Silent` behavior.
- Secret redaction is part of user-facing safety; keep tests around `maintenance_json` intact.
- Heartbeat and proactive paths are separate from ordinary user sessions.

## ANTI-PATTERNS
- Do not expose system prompts, hidden reasoning, env vars, credentials, or private metadata.
- Do not let proactive/reminder paths send duplicate or contextless outreach.
- Do not collapse heartbeat sessions into normal chat history without explicit design work.
- Do not weaken tests that check redaction, tool-call behavior, or session isolation.

## TESTS
```bash
go test ./pkg/agent -v
go test ./pkg/agent -run TestAgentLoop -v
go test ./pkg/agent -run TestMaintenanceJSON -v
go test ./pkg/agent -run TestProactive -v
```
