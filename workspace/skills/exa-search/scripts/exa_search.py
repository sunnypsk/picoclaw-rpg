#!/usr/bin/env python3

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

DEFAULT_API_BASE = "https://api.exa.ai"
SEARCH_TYPES = ["auto", "fast", "neural", "deep", "deep-reasoning", "instant"]
CATEGORIES = ["company", "research paper", "news", "tweet", "personal site", "financial report", "people"]


def get_picoclaw_home() -> Path:
    configured = os.getenv("PICOCLAW_HOME", "").strip()
    if configured:
        return Path(configured).expanduser().resolve()
    return (Path.home() / ".picoclaw").resolve()


def load_persistent_env() -> None:
    env_path = get_picoclaw_home() / ".env"
    if not env_path.is_file():
        return

    for raw_line in env_path.read_text(encoding="utf-8-sig").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue

        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if not key:
            continue

        if len(value) >= 2 and ((value[0] == '"' and value[-1] == '"') or (value[0] == "'" and value[-1] == "'")):
            value = value[1:-1]

        os.environ.setdefault(key, value)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Search the web with Exa AI.")
    parser.add_argument("query", help="Search query")
    parser.add_argument("--num-results", type=int, default=5, help="Number of results to request (default: 5)")
    parser.add_argument("--type", choices=SEARCH_TYPES, default="auto", help="Exa search type (default: auto)")
    parser.add_argument("--category", choices=CATEGORIES, help="Restrict to an Exa category")
    parser.add_argument("--user-location", help="Optional 2-letter country code for userLocation")
    parser.add_argument("--max-age-hours", type=int, help="Only return results from the last N hours")
    parser.add_argument("--include-domain", action="append", default=[], help="Limit results to this domain")
    parser.add_argument("--exclude-domain", action="append", default=[], help="Exclude this domain")
    parser.add_argument("--include-text", action="append", default=[], help="Only include results containing this text")
    parser.add_argument("--exclude-text", action="append", default=[], help="Exclude results containing this text")
    parser.add_argument("--start-published-date", help="Published date lower bound in ISO format")
    parser.add_argument("--end-published-date", help="Published date upper bound in ISO format")
    parser.add_argument("--start-crawl-date", help="Crawl date lower bound in ISO format")
    parser.add_argument("--end-crawl-date", help="Crawl date upper bound in ISO format")
    parser.add_argument("--highlights", action="store_true", help="Request highlights for each result")
    parser.add_argument(
        "--highlights-max-characters",
        type=int,
        default=2000,
        help="Maximum highlight characters when --highlights is used (default: 2000)",
    )
    parser.add_argument("--highlights-query", help="Optional highlight-specific query")
    parser.add_argument("--summary", action="store_true", help="Request result summaries")
    parser.add_argument("--summary-query", help="Optional summary-specific query")
    parser.add_argument("--text", action="store_true", help="Request page text")
    parser.add_argument(
        "--text-max-characters",
        type=int,
        default=10000,
        help="Maximum text characters when --text is used (default: 10000)",
    )
    parser.add_argument(
        "--output-schema-file",
        help="Optional JSON schema file for deep search structured output",
    )
    parser.add_argument("--timeout", type=float, default=120.0, help="HTTP timeout in seconds (default: 120)")
    return parser.parse_args()


def get_env(name: str, default: str = "") -> str:
    return os.getenv(name, default).strip()


def resolve_config() -> tuple[str, str]:
    api_key = get_env("EXA_API_KEY")
    if not api_key:
        raise RuntimeError("EXA_API_KEY is required")

    api_base = get_env("EXA_API_BASE", DEFAULT_API_BASE).rstrip("/")
    return api_key, api_base


def dedupe(values: list[str]) -> list[str]:
    items: list[str] = []
    seen: set[str] = set()
    for value in values:
        item = value.strip()
        if not item or item in seen:
            continue
        seen.add(item)
        items.append(item)
    return items


def read_json_file(path_arg: str) -> Any:
    path = Path(path_arg).expanduser().resolve()
    if not path.exists():
        raise RuntimeError(f"JSON file not found: {path}")
    if not path.is_file():
        raise RuntimeError(f"JSON path is not a file: {path}")

    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"invalid JSON in {path}: {exc}") from exc


def validate_args(args: argparse.Namespace) -> None:
    if not args.query.strip():
        raise RuntimeError("query must not be empty")

    if args.num_results < 1:
        raise RuntimeError("--num-results must be at least 1")
    if args.num_results > 100:
        raise RuntimeError("--num-results must be 100 or less")

    if len(args.include_text) > 1:
        raise RuntimeError("--include-text supports at most one value")
    if len(args.exclude_text) > 1:
        raise RuntimeError("--exclude-text supports at most one value")

    if args.highlights and args.highlights_max_characters < 1:
        raise RuntimeError("--highlights-max-characters must be at least 1")
    if args.text and args.text_max_characters < 1:
        raise RuntimeError("--text-max-characters must be at least 1")
    if args.max_age_hours is not None and args.max_age_hours < 0:
        raise RuntimeError("--max-age-hours must be 0 or greater")

    if args.user_location:
        location = args.user_location.strip()
        if len(location) != 2 or not location.isalpha():
            raise RuntimeError("--user-location must be a 2-letter country code")

    if args.category in {"company", "people"}:
        unsupported = []
        if args.start_published_date or args.end_published_date:
            unsupported.append("published date filters")
        if args.start_crawl_date or args.end_crawl_date:
            unsupported.append("crawl date filters")
        if args.include_text:
            unsupported.append("--include-text")
        if args.exclude_text:
            unsupported.append("--exclude-text")
        if args.exclude_domain:
            unsupported.append("--exclude-domain")
        if unsupported:
            raise RuntimeError(
                f"category '{args.category}' does not support {', '.join(unsupported)}"
            )

    if args.category == "people":
        invalid_domains = [domain for domain in args.include_domain if "linkedin." not in domain.lower()]
        if invalid_domains:
            raise RuntimeError("people category only supports LinkedIn domains in --include-domain")

    if args.output_schema_file and not args.type.startswith("deep"):
        raise RuntimeError("--output-schema-file requires --type deep or --type deep-reasoning")


def build_contents(args: argparse.Namespace) -> dict[str, Any]:
    contents: dict[str, Any] = {}

    if args.highlights:
        highlight_options: dict[str, Any] = {"maxCharacters": args.highlights_max_characters}
        if args.highlights_query:
            highlight_options["query"] = args.highlights_query.strip()
        contents["highlights"] = highlight_options

    if args.summary:
        if args.summary_query:
            contents["summary"] = {"query": args.summary_query.strip()}
        else:
            contents["summary"] = True

    if args.text:
        contents["text"] = {"maxCharacters": args.text_max_characters}

    return contents


def build_payload(args: argparse.Namespace) -> dict[str, Any]:
    include_domains = dedupe(args.include_domain)
    exclude_domains = dedupe(args.exclude_domain)
    include_text = dedupe(args.include_text)
    exclude_text = dedupe(args.exclude_text)

    payload: dict[str, Any] = {
        "query": args.query.strip(),
        "type": args.type,
        "numResults": args.num_results,
    }

    if args.category:
        payload["category"] = args.category
    if include_domains:
        payload["includeDomains"] = include_domains
    if exclude_domains:
        payload["excludeDomains"] = exclude_domains
    if include_text:
        payload["includeText"] = include_text[0]
    if exclude_text:
        payload["excludeText"] = exclude_text[0]
    if args.start_published_date:
        payload["startPublishedDate"] = args.start_published_date
    if args.end_published_date:
        payload["endPublishedDate"] = args.end_published_date
    if args.start_crawl_date:
        payload["startCrawlDate"] = args.start_crawl_date
    if args.end_crawl_date:
        payload["endCrawlDate"] = args.end_crawl_date
    if args.max_age_hours is not None:
        payload["maxAgeHours"] = args.max_age_hours
    if args.user_location:
        payload["userLocation"] = {"country": args.user_location.strip().upper()}

    contents = build_contents(args)
    if contents:
        payload["contents"] = contents

    if args.output_schema_file:
        payload["outputSchema"] = read_json_file(args.output_schema_file)

    return payload


def send_request(api_key: str, api_base: str, payload: dict[str, Any], timeout: float) -> dict[str, Any]:
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url=f"{api_base}/search",
        data=body,
        headers={
            "Content-Type": "application/json",
            "x-api-key": api_key,
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            response_body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Exa API error {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to reach Exa API: {exc.reason}") from exc

    try:
        return json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise RuntimeError("Exa API returned non-JSON response") from exc


def get_value(data: dict[str, Any], *keys: str) -> Any:
    for key in keys:
        if key in data:
            return data[key]
    return None


def normalize_result(item: dict[str, Any]) -> dict[str, Any]:
    normalized = {
        "title": get_value(item, "title"),
        "url": get_value(item, "url"),
    }

    for source_key, output_key in [
        ("id", "id"),
        ("publishedDate", "published_date"),
        ("published_date", "published_date"),
        ("author", "author"),
        ("score", "score"),
        ("summary", "summary"),
        ("highlights", "highlights"),
        ("highlightScores", "highlight_scores"),
        ("highlight_scores", "highlight_scores"),
        ("text", "text"),
        ("image", "image"),
        ("favicon", "favicon"),
    ]:
        value = get_value(item, source_key)
        if value not in (None, "", [], {}):
            normalized[output_key] = value

    return normalized


def normalize_response(response_data: dict[str, Any]) -> dict[str, Any]:
    results = response_data.get("results")
    if not isinstance(results, list):
        results = []

    normalized = {
        "request_id": get_value(response_data, "requestId", "request_id"),
        "search_type": get_value(response_data, "searchType", "resolvedSearchType", "search_type"),
        "results": [normalize_result(item) for item in results if isinstance(item, dict)],
    }

    output = get_value(response_data, "output")
    if output not in (None, "", [], {}):
        normalized["output"] = output

    cost = get_value(response_data, "costDollars", "cost_dollars")
    if cost is not None:
        normalized["cost_dollars"] = cost

    return normalized


def main() -> int:
    args = parse_args()

    try:
        load_persistent_env()
        validate_args(args)
        api_key, api_base = resolve_config()
        payload = build_payload(args)
        response_data = send_request(api_key, api_base, payload, args.timeout)
        normalized = normalize_response(response_data)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    json.dump(normalized, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
