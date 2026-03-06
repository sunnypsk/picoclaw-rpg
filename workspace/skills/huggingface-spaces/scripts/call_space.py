#!/usr/bin/env python3

import argparse
import json
import os
import re
import sys
from pathlib import Path
from typing import Optional
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


def load_handle_file():
    try:
        from gradio_client import handle_file
    except ImportError as exc:
        raise RuntimeError(
            "gradio_client is required. Install it with: python3 -m pip install gradio_client"
            " (or py -m pip install gradio_client on Windows)"
        ) from exc
    return handle_file


def read_payload(args) -> object:
    if args.payload_json is not None:
        return json.loads(args.payload_json)
    if args.payload_file is None:
        return []

    if args.payload_file == "-":
        return json.load(sys.stdin)

    with open(args.payload_file, "r", encoding="utf-8") as handle:
        return json.load(handle)


def replace_file_markers(value, handle_file):
    if isinstance(value, dict):
        if set(value.keys()) == {"$file"}:
            return handle_file(value["$file"])
        return {key: replace_file_markers(item, handle_file) for key, item in value.items()}
    if isinstance(value, list):
        return [replace_file_markers(item, handle_file) for item in value]
    return value


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


def inspect_api(client):
    api_info = client.view_api(print_info=False, return_format="dict")
    api_stdout = client.view_api(print_info=False, return_format="str") or ""
    return api_info, api_stdout.strip()


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


def extract_endpoints(api_info, api_stdout: str):
    endpoints = []

    if isinstance(api_info, dict):
        named_endpoints = api_info.get("named_endpoints")
        if isinstance(named_endpoints, dict):
            for name, endpoint in named_endpoints.items():
                if isinstance(endpoint, dict):
                    item = dict(endpoint)
                    item.setdefault("name", name)
                else:
                    item = {"name": name, "value": endpoint}
                endpoints.append(item)

        unnamed_endpoints = api_info.get("unnamed_endpoints")
        if isinstance(unnamed_endpoints, dict):
            for fn_index, endpoint in unnamed_endpoints.items():
                if isinstance(endpoint, dict):
                    item = dict(endpoint)
                    item.setdefault("fn_index", int(fn_index))
                else:
                    item = {"fn_index": int(fn_index), "value": endpoint}
                endpoints.append(item)

        if endpoints:
            return endpoints

    return extract_endpoints_from_stdout(api_stdout)


def resolve_endpoint(requested_api_name: Optional[str], requested_fn_index: Optional[int], endpoints):
    if requested_api_name and requested_fn_index is not None:
        raise ValueError("pass either --api-name or --fn-index, not both")

    if requested_api_name:
        return {"api_name": requested_api_name}

    if requested_fn_index is not None:
        return {"fn_index": requested_fn_index}

    if len(endpoints) == 1:
        endpoint = endpoints[0]
        if endpoint.get("name"):
            return {"api_name": endpoint["name"]}
        if endpoint.get("fn_index") is not None:
            return {"fn_index": endpoint["fn_index"]}

    raise ValueError(
        "could not determine endpoint automatically; inspect the Space first or pass --api-name/--fn-index"
    )


def build_call_args(payload, handle_file):
    converted = replace_file_markers(payload, handle_file)
    if isinstance(converted, list):
        return converted, {}
    if isinstance(converted, dict):
        return [], converted
    return [converted], {}


def call_space(
    space_id: str,
    api_name: Optional[str],
    fn_index: Optional[int],
    payload,
    timeout_seconds: float,
):
    client = load_client(space_id, timeout_seconds)
    api_info, api_stdout = inspect_api(client)
    endpoints = extract_endpoints(api_info, api_stdout)
    resolved_endpoint = resolve_endpoint(api_name, fn_index, endpoints)

    handle_file = load_handle_file()
    call_args, call_kwargs = build_call_args(payload, handle_file)

    job = client.submit(*call_args, **resolved_endpoint, **call_kwargs)
    result = job.result()

    output = {
        "space": space_id,
        "result": make_json_safe(result),
    }
    if resolved_endpoint.get("api_name"):
        output["api_name"] = resolved_endpoint["api_name"]
    if resolved_endpoint.get("fn_index") is not None:
        output["fn_index"] = resolved_endpoint["fn_index"]
    if api_stdout:
        output["view_api_stdout"] = api_stdout
    if endpoints:
        output["endpoints"] = make_json_safe(endpoints)
    return output


def main() -> int:
    parser = argparse.ArgumentParser(description="Call a Hugging Face Space with gradio_client.")
    parser.add_argument("space", help="Space slug (owner/space) or full Hugging Face Space URL")
    parser.add_argument("--api-name", help="Gradio API name such as /predict")
    parser.add_argument("--fn-index", type=int, help="Unnamed Gradio endpoint index such as 0")
    parser.add_argument("--timeout", type=float, default=120.0, help="HTTP timeout in seconds (default: 120)")

    payload_group = parser.add_mutually_exclusive_group()
    payload_group.add_argument("--payload-json", help="Inline JSON payload")
    payload_group.add_argument("--payload-file", help="Path to a JSON payload file, or - to read stdin")

    args = parser.parse_args()

    try:
        load_persistent_env()
        space_id = normalize_space_id(args.space)
        payload = read_payload(args)
        result = call_space(space_id, args.api_name, args.fn_index, payload, args.timeout)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    json.dump(result, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

