---
name: exa-search
description: Search the web with Exa AI using the bundled Python helper. Use when the user asks to use Exa, Exa AI, Exa search, current web search, domain-filtered search, or high-signal web results with highlights, summaries, or full text.
homepage: https://exa.ai/docs/reference/search
metadata: {"nanobot":{"emoji":"🔎","requires":{"bins":["python3"]}}}
---

# Exa Search

Use this skill to search the web with Exa's Search API.

## When to use

Use it when the user asks to:
- use Exa or Exa AI
- search the web for current information
- search within or exclude specific domains
- get token-efficient highlights first, then request full text only if needed
- run focused news, company, people, or research-paper searches

## Setup

- Set `EXA_API_KEY` before use.
- Optional: set `EXA_API_BASE` to override the default `https://api.exa.ai`.
- The helper auto-loads `~/.picoclaw/.env` by default, or `$PICOCLAW_HOME/.env` when `PICOCLAW_HOME` is set.
- In the default Docker Compose setup, that persistent env file path is `docker/data/.env` on the host.
- Never print, echo, or expose `EXA_API_KEY`.

Example `docker/data/.env` entries:

```dotenv
EXA_API_KEY=exa_xxx
# Optional:
# EXA_API_BASE=https://api.exa.ai
```

## Workflow

1. Start with highlights for fast, token-efficient search:

```bash
python3 workspace/skills/exa-search/scripts/exa_search.py \
  "latest AI agent frameworks" \
  --num-results 5 \
  --highlights
```

On Windows, use `py` instead of `python3` if needed.

2. Add domain filters or category filters when the user wants tighter scope:

```bash
python3 workspace/skills/exa-search/scripts/exa_search.py \
  "vision-language model papers" \
  --category "research paper" \
  --include-domain arxiv.org \
  --include-domain paperswithcode.com \
  --num-results 8 \
  --highlights
```

3. Ask for very fresh results when recency matters:

```bash
python3 workspace/skills/exa-search/scripts/exa_search.py \
  "latest announcements from OpenAI" \
  --include-domain openai.com \
  --max-age-hours 1 \
  --highlights
```

4. Request full text only when highlights are not enough:

```bash
python3 workspace/skills/exa-search/scripts/exa_search.py \
  "detailed analysis of transformer architecture innovations" \
  --text \
  --text-max-characters 12000 \
  --num-results 5
```

## Smoke Test

From the repo root, reuse `docker/data/.env` without copying secrets anywhere else:

```powershell
$env:PICOCLAW_HOME=(Resolve-Path .\docker\data).Path; py -3 .\workspace\skills\exa-search\scripts\exa_search.py "latest announcements from OpenAI" --include-domain openai.com --max-age-hours 24 --highlights
```

On Linux or macOS:

```bash
PICOCLAW_HOME=./docker/data python3 workspace/skills/exa-search/scripts/exa_search.py "latest announcements from OpenAI" --include-domain openai.com --max-age-hours 24 --highlights
```

## Output

- The helper prints normalized JSON to stdout.
- `results` includes `title`, `url`, `published_date`, `author`, `score`, and any requested `highlights`, `summary`, or `text`.
- `search_type` shows the resolved Exa search type when available.
- `output` is included for deep search variants.
- `cost_dollars` is included when Exa returns cost info.

## Notes

- Prefer `--highlights` first for agentic workflows.
- Use `--category news` or `--category "research paper"` when the user signals those content types.
- Use `--max-age-hours` when the user cares about very recent content.
- `company` and `people` category searches support fewer filters; if stricter filtering is needed, run a broader search instead.
