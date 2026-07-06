# DOCKER DELIVERY

Generated: 2026-07-06T03:57:27Z

## OVERVIEW
`docker` contains standard runtime, full MCP runtime, release image, compose profiles, entrypoint,
and Docker validation scripts.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Standard local/runtime image | `Dockerfile`, `docker-compose.yml` | Alpine image with Go-built binary, Python venv, Node/npm. |
| Full MCP image | `Dockerfile.full`, `docker-compose.full.yml` | Node 24 Alpine plus git, Python, uv/uvx, npm cache. |
| Release image | `Dockerfile.goreleaser`, `.goreleaser.yaml` | Uses GoReleaser buildx artifacts. |
| Entrypoint behavior | `entrypoint.sh` | Bootstraps runtime container command. |
| MCP image validation | `scripts/test-docker-mcp.sh`, `make docker-test` | Tests tool availability and quick MCP install. |

## CONVENTIONS
- For local code changes, rebuild from local source:
  `docker build -t docker.io/sipeed/picoclaw:latest -f docker/Dockerfile .`
- `docker/docker-compose.yml` defaults to published image via `PICOCLAW_IMAGE` and persists
  `./data` to `/home/picoclaw/.picoclaw`.
- Standard compose separates `agent` and `gateway` profiles; one-shot agent and long-running
  gateway should not be conflated.
- Full compose builds locally and uses named volumes for workspace and npm cache.
- Standard runtime runs as non-root user `picoclaw`.

## ANTI-PATTERNS
- Do not commit `docker/data` or secrets from mounted config.
- Do not pull remote `latest` to validate local source changes.
- Do not remove `python3`, `gradio_client`, `openpyxl`, or `pypdf` from the standard image without
  checking workspace skill expectations.
- Do not expose gateway beyond `127.0.0.1` unless the task explicitly requires LAN/public access.

## TESTS
```bash
make docker-build
make docker-run
make docker-test
docker build -t docker.io/sipeed/picoclaw:latest -f docker/Dockerfile .
docker compose -f docker/docker-compose.yml --profile gateway up
```
