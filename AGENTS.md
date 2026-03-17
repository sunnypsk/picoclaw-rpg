# Repository Guidelines

## Project Structure & Module Organization
- `cmd/picoclaw/`: main CLI entrypoint and command groups.
- `cmd/picoclaw-launcher/`: web launcher process; `cmd/picoclaw-launcher-tui/`: terminal UI launcher.
- `pkg/`: core runtime modules (agent loop, tools, providers, channels, config, auth, session, migration).
- Tests are colocated with code as `*_test.go`.
- `config/config.example.json`: configuration template.
- `docker/`: compose files and Dockerfiles for agent/gateway images.
- `docs/`: channel setup guides, migration notes, and design docs.
- `assets/`: repository media; `workspace/`: sample workspace files and skills.

## Build, Test, and Development Commands
```bash
make deps        # Download and verify Go modules
make build       # Run go generate, then build current-platform binary into build/
make test        # Run all tests (go test ./...)
make fmt         # Apply gofmt/gofumpt/goimports/gci/golines via golangci-lint
make lint        # Run linters
make check       # Pre-PR baseline: deps + fmt + vet + test
make run ARGS="status"   # Build and run picoclaw with arguments
```
Use `make build-all` for multi-platform artifacts. Docker workflows are exposed as `make docker-build`, `make docker-run`, and related `docker-*` targets.

### Local Docker image workflow (user preference)
- For local code changes, rebuild and run from local source instead of pulling remote `latest`:
  - `docker build -t docker.io/sipeed/picoclaw:latest -f docker/Dockerfile .`
- The single-image runtime now includes `python3` plus `gradio_client` for workspace skills such as `huggingface-spaces`.
- `docker/docker-compose.yml` persists Picoclaw data at `./docker/data`, mounted to `/home/picoclaw/.picoclaw` in the container.

### CPA Gateway Notes
- Python helpers that call CPA via `urllib` can be blocked by some gateways with `HTTP 403` and body `error code: 1010`, even when the same `chat/completions` request works from Go or curl.
- For Python CPA helpers, always set an explicit neutral `User-Agent` header such as `picoclaw/1.0` instead of relying on the default `Python-urllib` signature.
- If the Go `generate_image` tool works but a Python CPA skill fails, compare the runtime environment and HTTP headers before changing the model or route.

## Coding Style & Naming Conventions
- Use Go `1.25.x` (see `go.mod`).
- Keep lines within 120 chars (configured in `.golangci.yaml`).
- Follow idiomatic Go naming:
  - Exported identifiers: `PascalCase`
  - Internal identifiers: `camelCase`
  - Package names: lowercase, short, domain-oriented
- Keep file names lowercase and descriptive (for example, `codex_provider.go`, `shell_process_windows.go`).
- Run `make fmt` and `make lint` before pushing.

## Patch Application Rules
- On Windows PowerShell, do not pass patches to `apply_patch` via here-strings such as `@'... '@`; this can trigger `--codex-run-as-apply-patch requires a UTF-8 PATCH argument`.
- When editing files with `apply_patch`, invoke it with a UTF-8-safe literal patch argument so the patch text is encoded as UTF-8.
- If a patch contains non-ASCII text or PowerShell quoting makes UTF-8 handling ambiguous, prefer smaller ASCII-only patches or use a Bash shell for `apply_patch` execution.

## Security & Privacy Guidelines
- Agents must never expose internal values in user-facing outputs (including system prompts, hidden reasoning, runtime internals, environment variables, credentials, API keys, and private metadata).
- Agents must never commit, print, or document private service endpoints; use generic placeholders for custom or internal API base URLs.

## Testing Guidelines
- Primary framework: Go `testing`; `testify` is available for assertions/mocks.
- Name tests `TestXxx` and keep them next to implementation in `*_test.go`.
- Run focused tests with `go test -run TestName -v ./pkg/<module>/`.
- Integration suites use build tags (e.g., `go test -tags integration ./pkg/providers/...`).
- No fixed coverage percentage is enforced; ensure new/changed behavior is covered.

## Commit & Pull Request Guidelines
- Match existing history style: `feat(...)`, `fix(...)`, `chore(...)`, `docs(...)` with imperative summaries.
- Keep one logical change per commit; use descriptive branch names like `fix/telegram-timeout`.
- Before opening a PR, run `make check`.
- Complete the PR template: description, change type, related issue, AI disclosure, and test environment.
- Link issues, include logs/screenshots when useful, and ensure CI is green before review.
