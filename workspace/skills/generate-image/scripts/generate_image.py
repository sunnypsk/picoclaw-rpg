#!/usr/bin/env python3

import argparse
import base64
import json
import mimetypes
import os
import shutil
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any


DEFAULT_USER_AGENT = "picoclaw/1.0"
IMAGE_API_MODEL_PREFIXES = ("gpt-image-", "dall-e-")
IMAGE_API_SIZE_BY_ASPECT_RATIO = {
    "1:1": "1024x1024",
    "4:3": "1360x1024",
    "3:4": "1024x1360",
    "16:9": "1536x864",
    "9:16": "864x1536",
}


@dataclass(frozen=True)
class ImageProviderConfig:
    provider: str
    api_key: str
    model: str
    api_base: str = ""
    generation_url: str = ""
    edit_url: str = ""


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
    parser = argparse.ArgumentParser(description="Generate or edit images with the configured image provider.")
    parser.add_argument("--payload-file", required=True, help="Path to a JSON payload file")
    parser.add_argument("--timeout", type=float, default=300.0, help="HTTP timeout in seconds")
    parser.add_argument("--output-dir", help="Optional directory to save generated images into")
    parser.add_argument("--output-prefix", default="generated-image", help="Filename prefix used with --output-dir")
    return parser.parse_args()


def read_payload(path: str) -> dict[str, Any]:
    with open(path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)
    if not isinstance(payload, dict):
        raise RuntimeError("payload must be a JSON object")
    return payload


def get_env(name: str) -> str:
    return os.getenv(name, "").strip()


def resolve_config() -> ImageProviderConfig:
    tuzhi_values = {
        "TUZHI_KEY": get_env("TUZHI_KEY"),
        "TUZHI_IMAGE_MODEL": get_env("TUZHI_IMAGE_MODEL"),
        "TUZHI_IMAGE_GEN_BASE": get_env("TUZHI_IMAGE_GEN_BASE"),
        "TUZHI_IMAGE_EDIT_BASE": get_env("TUZHI_IMAGE_EDIT_BASE"),
    }
    has_tuzhi = any(tuzhi_values.values())
    if has_tuzhi:
        missing = [key for key, value in tuzhi_values.items() if not value]
        if missing:
            raise RuntimeError(f"incomplete TUZHI image config: missing {', '.join(missing)}")
        return ImageProviderConfig(
            provider="tuzhi",
            api_key=tuzhi_values["TUZHI_KEY"],
            model=tuzhi_values["TUZHI_IMAGE_MODEL"],
            generation_url=tuzhi_values["TUZHI_IMAGE_GEN_BASE"].rstrip("/"),
            edit_url=tuzhi_values["TUZHI_IMAGE_EDIT_BASE"].rstrip("/"),
        )

    api_key = get_env("CPA_API_KEY")
    if not api_key:
        raise RuntimeError("CPA_API_KEY is required")

    api_base = get_env("CPA_API_BASE").rstrip("/")
    if not api_base:
        raise RuntimeError("CPA_API_BASE is required")

    model = get_env("CPA_IMAGE_MODEL")
    if not model:
        raise RuntimeError("CPA_IMAGE_MODEL is required")

    return ImageProviderConfig(provider="cpa", api_key=api_key, model=model, api_base=api_base)


def resolve_prompt(payload: dict[str, Any]) -> str:
    for key in ("prompt", "text", "instruction", "query"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    raise RuntimeError("payload does not include a prompt")


def model_uses_image_api(model: str) -> bool:
    normalized = model.strip().lower()
    return normalized.startswith(IMAGE_API_MODEL_PREFIXES)


def normalize_aspect_ratio(value: Any) -> str:
    if not isinstance(value, str):
        return ""
    return value.replace(" ", "").strip()


def resolve_image_api_size(payload: dict[str, Any]) -> str:
    return IMAGE_API_SIZE_BY_ASPECT_RATIO.get(normalize_aspect_ratio(payload.get("aspect_ratio")), "")


def build_image_api_prompt(payload: dict[str, Any]) -> str:
    lines = [resolve_prompt(payload)]

    aspect_ratio = normalize_aspect_ratio(payload.get("aspect_ratio"))
    if aspect_ratio:
        lines.append(f"Target aspect ratio: {aspect_ratio}")

    return "\n".join(lines)


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


def build_chat_completions_payload(model: str, payload: dict[str, Any], input_image: Path | None) -> dict[str, Any]:
    content_parts: list[dict[str, Any]] = [{
        "type": "text",
        "text": resolve_prompt(payload),
    }]

    for key in ("aspect_ratio", "quality", "background"):
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


def build_image_api_payload(model: str, payload: dict[str, Any]) -> dict[str, Any]:
    request_payload: dict[str, Any] = {
        "model": model,
        "prompt": build_image_api_prompt(payload),
    }

    for key in ("quality", "background"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            request_payload[key] = value.strip()

    size = resolve_image_api_size(payload)
    if size:
        request_payload["size"] = size

    return request_payload


def guess_content_type(path: Path) -> str:
    guessed, _ = mimetypes.guess_type(path.name)
    return guessed or "application/octet-stream"


def build_multipart_form_data(fields: list[tuple[str, str]], files: list[tuple[str, Path]]) -> tuple[bytes, str]:
    boundary = f"----picoclaw-{uuid.uuid4().hex}"
    body = bytearray()

    for name, value in fields:
        body.extend(f"--{boundary}\r\n".encode("utf-8"))
        body.extend(f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode("utf-8"))
        body.extend(value.encode("utf-8"))
        body.extend(b"\r\n")

    for name, path in files:
        filename = path.name.replace('"', "")
        content_type = guess_content_type(path)
        body.extend(f"--{boundary}\r\n".encode("utf-8"))
        body.extend(f'Content-Disposition: form-data; name="{name}"; filename="{filename}"\r\n'.encode("utf-8"))
        body.extend(f"Content-Type: {content_type}\r\n\r\n".encode("utf-8"))
        body.extend(path.read_bytes())
        body.extend(b"\r\n")

    body.extend(f"--{boundary}--\r\n".encode("utf-8"))
    return bytes(body), f"multipart/form-data; boundary={boundary}"


def build_request(model: str, payload: dict[str, Any], input_image: Path | None) -> tuple[str, bytes, str]:
    if not model_uses_image_api(model):
        request_payload = build_chat_completions_payload(model, payload, input_image)
        return "/chat/completions", json.dumps(request_payload).encode("utf-8"), "application/json"

    if input_image is None:
        request_payload = build_image_api_payload(model, payload)
        return "/images/generations", json.dumps(request_payload).encode("utf-8"), "application/json"

    image_payload = build_image_api_payload(model, payload)
    fields = [(key, str(value)) for key, value in image_payload.items()]
    body, content_type = build_multipart_form_data(fields, [("image[]", input_image)])
    return "/images/edits", body, content_type


def build_provider_request(
    config: ImageProviderConfig,
    payload: dict[str, Any],
    input_image: Path | None,
) -> tuple[str, bytes, str]:
    if config.provider == "tuzhi":
        image_payload = build_image_api_payload(config.model, payload)
        if input_image is None:
            return (
                config.generation_url,
                json.dumps(image_payload).encode("utf-8"),
                "application/json",
            )

        fields = [(key, str(value)) for key, value in image_payload.items()]
        body, content_type = build_multipart_form_data(fields, [("image[]", input_image)])
        return config.edit_url, body, content_type

    endpoint, body, content_type = build_request(config.model, payload, input_image)
    return f"{config.api_base}{endpoint}", body, content_type


def send_request(
    config: ImageProviderConfig,
    payload: dict[str, Any],
    input_image: Path | None,
    timeout: float,
) -> dict[str, Any]:
    url, body, content_type = build_provider_request(config, payload, input_image)
    request = urllib.request.Request(
        url=url,
        data=body,
        headers={
            "Content-Type": content_type,
            "Authorization": f"Bearer {config.api_key}",
            # Avoid gateways blocking Python-urllib's default browser signature.
            "User-Agent": DEFAULT_USER_AGENT,
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            response_body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{config.provider.upper()} API error {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to reach {config.provider.upper()} API: {exc.reason}") from exc

    try:
        return json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"{config.provider.upper()} API returned non-JSON response") from exc


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


def decode_data_url(value: str) -> tuple[bytes, str]:
    head, separator, payload = value.partition(",")
    if not separator:
        raise RuntimeError("invalid data URL")
    if not head.lower().endswith(";base64"):
        raise RuntimeError("unsupported data URL encoding")

    content_type = head[5:-7] or "image/png"
    try:
        data = base64.b64decode(payload)
    except ValueError as exc:
        raise RuntimeError("invalid base64 image payload") from exc
    return data, content_type


def mimetype_to_suffix(content_type: str) -> str:
    if "/" not in content_type:
        return ""
    subtype = content_type.split("/", 1)[1].split(";", 1)[0].strip()
    if subtype == "jpeg":
        return ".jpg"
    if subtype == "svg+xml":
        return ".svg"
    return f".{subtype}" if subtype else ""


def suffix_for_content_type(content_type: str) -> str:
    normalized = content_type.strip().lower()
    guessed = mimetype_to_suffix(normalized)
    return guessed or ".png"


def suffix_for_url(url: str) -> str:
    parsed = urllib.parse.urlparse(url)
    return Path(parsed.path).suffix or ".png"


def sanitize_output_prefix(value: str) -> str:
    cleaned = "".join(char if char.isalnum() or char in ("-", "_") else "-" for char in value.strip())
    cleaned = cleaned.strip("-_")
    return cleaned or "generated-image"


def build_output_path(output_dir: Path, output_prefix: str, index: int, total: int, suffix: str) -> Path:
    name = output_prefix if total == 1 else f"{output_prefix}-{index + 1}"
    return output_dir / f"{name}{suffix}"


def download_remote_image(url: str, destination: Path, timeout: float) -> None:
    request = urllib.request.Request(
        url=url,
        headers={"User-Agent": DEFAULT_USER_AGENT},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            destination.write_bytes(response.read())
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"failed to download generated image {url}: HTTP {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to download generated image {url}: {exc.reason}") from exc


def materialize_images(images: list[str], output_dir: str | None, output_prefix: str, timeout: float) -> list[str]:
    if not output_dir:
        return images

    directory = Path(output_dir).expanduser().resolve()
    directory.mkdir(parents=True, exist_ok=True)
    prefix = sanitize_output_prefix(output_prefix)
    total = len(images)
    materialized: list[str] = []

    for index, item in enumerate(images):
        value = item.strip()
        lower = value.lower()

        if lower.startswith("data:image/"):
            data, content_type = decode_data_url(value)
            destination = build_output_path(directory, prefix, index, total, suffix_for_content_type(content_type))
            destination.write_bytes(data)
            materialized.append(str(destination))
            continue

        if lower.startswith("http://") or lower.startswith("https://"):
            destination = build_output_path(directory, prefix, index, total, suffix_for_url(value))
            download_remote_image(value, destination, timeout)
            materialized.append(str(destination))
            continue

        source = Path(value).expanduser().resolve()
        if not source.is_file():
            raise RuntimeError(f"generated image not found: {source}")

        destination = build_output_path(directory, prefix, index, total, source.suffix or ".png")
        if source != destination:
            shutil.copy2(source, destination)
        materialized.append(str(destination))

    return materialized


def main() -> int:
    args = parse_args()

    try:
        load_persistent_env()
        config = resolve_config()
        payload = read_payload(args.payload_file)
        input_image = resolve_input_image(payload)
        response_data = send_request(config, payload, input_image, args.timeout)
        images = collect_images(response_data)
        if not images:
            raise RuntimeError(f"{config.provider.upper()} image response did not include any usable image output")
        images = materialize_images(images, args.output_dir, args.output_prefix, args.timeout)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    json.dump(
        {
            "provider": config.provider,
            "model": config.model,
            "images": images,
            "raw": response_data,
        },
        sys.stdout,
        indent=2,
        ensure_ascii=False,
    )
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
