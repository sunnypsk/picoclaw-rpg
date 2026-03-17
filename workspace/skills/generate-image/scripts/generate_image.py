#!/usr/bin/env python3

import argparse
import base64
import json
import os
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_USER_AGENT = "picoclaw/1.0"


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
    parser = argparse.ArgumentParser(description="Generate or edit images with CPA chat completions.")
    parser.add_argument("--payload-file", required=True, help="Path to a JSON payload file")
    parser.add_argument("--timeout", type=float, default=300.0, help="HTTP timeout in seconds")
    return parser.parse_args()


def read_payload(path: str) -> dict[str, Any]:
    with open(path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)
    if not isinstance(payload, dict):
        raise RuntimeError("payload must be a JSON object")
    return payload


def get_env(name: str) -> str:
    return os.getenv(name, "").strip()


def resolve_config() -> tuple[str, str, str]:
    api_key = get_env("CPA_API_KEY")
    if not api_key:
        raise RuntimeError("CPA_API_KEY is required")

    api_base = get_env("CPA_API_BASE").rstrip("/")
    if not api_base:
        raise RuntimeError("CPA_API_BASE is required")

    model = get_env("CPA_IMAGE_MODEL")
    if not model:
        raise RuntimeError("CPA_IMAGE_MODEL is required")

    return api_key, api_base, model


def resolve_prompt(payload: dict[str, Any]) -> str:
    for key in ("prompt", "text", "instruction", "query"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    raise RuntimeError("payload does not include a prompt")


def file_marker_to_path(value: Any) -> Path | None:
    if isinstance(value, dict) and set(value.keys()) == {"$file"} and isinstance(value["$file"], str):
        path = Path(value["$file"]).expanduser().resolve()
        if not path.is_file():
            raise RuntimeError(f"input image not found: {path}")
        return path
    if isinstance(value, str) and value.strip():
        path = Path(value).expanduser().resolve()
        if not path.is_file():
            raise RuntimeError(f"input image not found: {path}")
        return path
    return None


def resolve_input_image(payload: dict[str, Any]) -> Path | None:
    candidates: list[Path] = []

    for key in ("image", "input_image"):
        candidate = file_marker_to_path(payload.get(key))
        if candidate is not None:
            candidates.append(candidate)

    if "input_images" in payload:
        raw = payload["input_images"]
        if not isinstance(raw, list):
            raise RuntimeError("input_images must be a JSON array")
        for item in raw:
            candidate = file_marker_to_path(item)
            if candidate is not None:
                candidates.append(candidate)

    if not candidates:
        return None
    if len(candidates) > 1:
        raise RuntimeError("multiple explicit input images provided; keep only one")
    return candidates[0]


def encode_data_url(path: Path) -> str:
    suffix = path.suffix.lower().lstrip(".") or "png"
    media_type = f"image/{'jpeg' if suffix == 'jpg' else suffix}"
    data = base64.b64encode(path.read_bytes()).decode("ascii")
    return f"data:{media_type};base64,{data}"


def build_request_payload(model: str, payload: dict[str, Any], input_image: Path | None) -> dict[str, Any]:
    content_parts: list[dict[str, Any]] = [{
        "type": "text",
        "text": resolve_prompt(payload),
    }]

    for key in ("size", "aspect_ratio", "quality", "style", "background"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            content_parts.append({
                "type": "text",
                "text": f"{key}: {value.strip()}",
            })

    if input_image is not None:
        data_url = encode_data_url(input_image)
        content_parts.append({
            "type": "image_url",
            "image_url": {"url": data_url},
        })

    return {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": content_parts,
            }
        ],
    }


def send_request(api_key: str, api_base: str, payload: dict[str, Any], timeout: float) -> dict[str, Any]:
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url=f"{api_base}/chat/completions",
        data=body,
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {api_key}",
            # Avoid CPA gateway blocking Python-urllib's default browser signature.
            "User-Agent": DEFAULT_USER_AGENT,
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            response_body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"CPA API error {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to reach CPA API: {exc.reason}") from exc

    try:
        return json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise RuntimeError("CPA API returned non-JSON response") from exc


def collect_images(value: Any) -> list[str]:
    images: list[str] = []

    if isinstance(value, str):
        text = value.strip()
        lower = text.lower()
        if lower.startswith("http://") or lower.startswith("https://") or lower.startswith("data:image/"):
            images.append(text)
        elif Path(text).suffix.lower() in {".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"}:
            images.append(text)
        return images

    if isinstance(value, dict):
        raw_b64 = value.get("b64_json")
        if isinstance(raw_b64, str) and raw_b64.strip():
            images.append(write_temp_image(base64.b64decode(raw_b64.strip()), ".png"))
        for key in ("url", "image_url", "path", "file", "name", "image"):
            if key in value:
                images.extend(collect_images(value[key]))
        for item in value.values():
            images.extend(collect_images(item))
        return images

    if isinstance(value, list):
        for item in value:
            images.extend(collect_images(item))

    return images


def write_temp_image(data: bytes, suffix: str) -> str:
    handle = tempfile.NamedTemporaryFile(prefix="picoclaw_image_", suffix=suffix, delete=False)
    with handle:
        handle.write(data)
    return handle.name


def main() -> int:
    args = parse_args()

    try:
        load_persistent_env()
        api_key, api_base, model = resolve_config()
        payload = read_payload(args.payload_file)
        input_image = resolve_input_image(payload)
        request_payload = build_request_payload(model, payload, input_image)
        response_data = send_request(api_key, api_base, request_payload, args.timeout)
        images = collect_images(response_data)
        if not images:
            raise RuntimeError("CPA image response did not include any usable image output")
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    json.dump(
        {
            "provider": "cpa",
            "model": model,
            "images": images,
            "raw": response_data,
        },
        sys.stdout,
        indent=2,
        ensure_ascii=False,
    )
    sys.stdout.write("\\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

