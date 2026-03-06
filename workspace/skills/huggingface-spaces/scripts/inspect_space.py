#!/usr/bin/env python3

import argparse
import json
import os
import re
import sys
from pathlib import Path
from urllib.parse import urlparse


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


def normalize_space_id(value: str) -> str:
    value = value.strip()
    parsed = urlparse(value)
    if parsed.scheme in {"http", "https"}:
        if parsed.netloc not in {"huggingface.co", "www.huggingface.co"}:
            raise ValueError(f"unsupported host: {parsed.netloc}")
        parts = [part for part in parsed.path.split("/") if part]
        if len(parts) >= 3 and parts[0] == "spaces":
            return f"{parts[1]}/{parts[2]}"
        raise ValueError("expected a Hugging Face Space URL like https://huggingface.co/spaces/owner/space")

    parts = [part for part in value.split("/") if part]
    if len(parts) == 2:
        return f"{parts[0]}/{parts[1]}"

    raise ValueError("expected a Space slug like owner/space or a full Hugging Face Space URL")


def make_json_safe(value):
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, dict):
        return {str(key): make_json_safe(item) for key, item in value.items()}
    if isinstance(value, (list, tuple, set)):
        return [make_json_safe(item) for item in value]
    if hasattr(value, "__dict__"):
        return make_json_safe(vars(value))
    return str(value)


def extract_endpoints_from_stdout(text: str):
    endpoints = []
    for line in text.splitlines():
        api_name_match = re.search(r'api_name="([^"]+)"', line)
        if api_name_match:
            endpoints.append({"name": api_name_match.group(1), "summary": line.strip()})
            continue

        fn_index_match = re.search(r'fn_index=(\d+)', line)
        if fn_index_match:
            endpoints.append({"fn_index": int(fn_index_match.group(1)), "summary": line.strip()})
    return endpoints


def inspect_api(client):
    api_info = client.view_api(print_info=False, return_format="dict")
    api_stdout = client.view_api(print_info=False, return_format="str") or ""
    return api_info, api_stdout.strip()


def extract_endpoints(api_info, api_stdout: str):
    endpoints = []

    if isinstance(api_info, dict):
        named_endpoints = api_info.get("named_endpoints")
        if isinstance(named_endpoints, dict):
            for name, endpoint in named_endpoints.items():
                item = dict(endpoint) if isinstance(endpoint, dict) else {"value": endpoint}
                item.setdefault("name", name)
                endpoints.append(item)

        unnamed_endpoints = api_info.get("unnamed_endpoints")
        if isinstance(unnamed_endpoints, dict):
            for fn_index, endpoint in unnamed_endpoints.items():
                item = dict(endpoint) if isinstance(endpoint, dict) else {"value": endpoint}
                item.setdefault("fn_index", int(fn_index))
                endpoints.append(item)

    if endpoints:
        return endpoints
    return extract_endpoints_from_stdout(api_stdout)


def load_client(space_id: str, timeout_seconds: float):
    try:
        from gradio_client import Client
    except ImportError as exc:
        raise RuntimeError(
            "gradio_client is required. Install it with: python3 -m pip install gradio_client"
            " (or py -m pip install gradio_client on Windows)"
        ) from exc

    token = os.getenv("HF_TOKEN") or None
    return Client(space_id, token=token, verbose=False, httpx_kwargs={"timeout": timeout_seconds})


def inspect_space(space_id: str, timeout_seconds: float):
    client = load_client(space_id, timeout_seconds)
    api_info, api_stdout = inspect_api(client)

    result = {
        "space": space_id,
        "api": make_json_safe(api_info),
    }
    if api_stdout:
        result["view_api_stdout"] = api_stdout

    endpoints = extract_endpoints(api_info, api_stdout)
    if endpoints:
        result["endpoints"] = make_json_safe(endpoints)
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description="Inspect a Hugging Face Space API with gradio_client.")
    parser.add_argument("space", help="Space slug (owner/space) or full Hugging Face Space URL")
    parser.add_argument("--timeout", type=float, default=120.0, help="HTTP timeout in seconds (default: 120)")
    args = parser.parse_args()

    try:
        load_persistent_env()
        space_id = normalize_space_id(args.space)
        result = inspect_space(space_id, args.timeout)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    json.dump(result, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

