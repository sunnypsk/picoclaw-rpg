# CONFIG

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`pkg/config` defines the PicoClaw config schema, defaults, env mapping, load/save behavior, and
legacy migration behavior.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Top-level schema | `config.go` | `Config`, `Agents`, `Channels`, `Providers`, `Tools`, `ModelConfig`. |
| Defaults | `defaults.go` | Many defaults are test-locked. |
| Model list parsing | `model_config.go`, `model_config_test.go` | Preferred new provider config path. |
| Legacy migration | `migration.go`, `migration_test.go` | Providers-to-model-list compatibility. |
| Public template | `config/config.example.json` | Keep docs/examples aligned with schema. |

## CONVENTIONS
- `model_list` is canonical for new configs; `providers` is deprecated compatibility input.
- `agents.defaults.model_name` must resolve against `model_list[].model_name`.
- Default behavior is heavily tested: persona, heartbeat, web search, hidden intermediate results,
  session DM scope, and workspace path derivation.
- `SaveConfig` preserving legacy fields is intentional; do not remove without changing tests and
  migration story.
- Keep env var tags coherent with JSON field names.

## ANTI-PATTERNS
- Do not add config fields only to Go structs; update example config and relevant docs/tests.
- Do not include real secrets in `config/config.example.json`.
- Do not silently change defaults without tests that explain the compatibility impact.
- Do not resurrect `providers` as the preferred user-facing path.

## TESTS
```bash
go test ./pkg/config -v
go test ./pkg/config -run TestModelConfig -v
go test ./pkg/config -run TestConfig -v
go test ./pkg/config -run TestMigrate -v
```
