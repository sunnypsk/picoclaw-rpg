#!/usr/bin/env python3

import argparse
import base64
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

DEFAULT_SPACE = "multimodalart/nano-banana"
HF_CALL_SCRIPT = Path("workspace/skills/huggingface-spaces/scripts/call_space.py")


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
    parser = argparse.ArgumentParser(
        description="Generate/edit images with hf-image default Space and fallback to CPA image endpoint."
    )
    parser.add_argument("--payload-file", required=True, help="Path to JSON payload used for image generation/editing")
    parser.add_argument("--space", default=DEFAULT_SPACE, help=f"Hugging Face Space slug/URL (default: {DEFAULT_SPACE})")
    parser.add_argument("--api-name", help="Optional Gradio API name")
    parser.add_argument("--fn-index", type=int, help="Optional Gradio fn_index")
    parser.add_argument("--timeout", type=float, default=300.0, help="Timeout in seconds for HF and CPA requests")
    return parser.parse_args()


def read_payload(path: str) -> Any:
    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def run_hf_call(args: argparse.Namespace) -> tuple[dict[str, Any] | None, str | None]:
    command = [
        "python3",
        str(HF_CALL_SCRIPT),
        args.space,
        "--payload-file",
        args.payload_file,
        "--timeout",
        str(args.timeout),
    ]
    if args.api_name:
        command.extend(["--api-name", args.api_name])
    if args.fn_index is not None:
        command.extend(["--fn-index", str(args.fn_index)])

    try:
        proc = subprocess.run(command, check=False, capture_output=True, text=True)
    except Exception as exc:
        return None, f"failed to run HF helper: {exc}"

    if proc.returncode != 0:
        message = proc.stderr.strip() or proc.stdout.strip() or f"hf helper exited with status {proc.returncode}"
        return None, message

    try:
        parsed = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return None, "hf helper returned non-JSON output"

    return parsed, None


def collect_image_candidates(value: Any) -> list[str]:
    candidates: list[str] = []

    if isinstance(value, str):
        text = value.strip()
        lower = text.lower()
        if text and (
            lower.startswith("http://")
            or lower.startswith("https://")
            or lower.endswith((".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"))
        ):
            candidates.append(text)
        return candidates

    if isinstance(value, dict):
        for key in ("url", "path", "image", "image_url", "file", "name"):
            if key in value:
                candidates.extend(collect_image_candidates(value[key]))
        for item in value.values():
            candidates.extend(collect_image_candidates(item))
        return candidates

    if isinstance(value, list):
        for item in value:
            candidates.extend(collect_image_candidates(item))

    return candidates


def extract_prompt(payload: Any) -> str:
    if isinstance(payload, str):
        return payload.strip()
    if isinstance(payload, dict):
        for key in ("prompt", "text", "instruction", "query"):
            value = payload.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
    return ""


def find_input_image(payload: Any) -> Path | None:
    if isinstance(payload, dict):
        if set(payload.keys()) == {"$file"} and isinstance(payload["$file"], str):
            return Path(payload["$file"]).expanduser().resolve()
        for value in payload.values():
            found = find_input_image(value)
            if found:
                return found
    elif isinstance(payload, list):
        for item in payload:
            found = find_input_image(item)
            if found:
                return found
    return None


def read_image_data_url(path: Path) -> str:
    suffix = path.suffix.lower().lstrip(".") or "png"
    media_type = f"image/{'jpeg' if suffix == 'jpg' else suffix}"
    data = base64.b64encode(path.read_bytes()).decode("ascii")
    return f"data:{media_type};base64,{data}"


def resolve_cpa_config() -> tuple[str, str, str]:
    api_key = os.getenv("CPA_API_KEY", "").strip()
    if not api_key:
        raise RuntimeError("CPA_API_KEY is required for fallback")

    api_base = os.getenv("CPA_API_BASE", "").strip().rstrip("/")
    if not api_base:
        raise RuntimeError("CPA_API_BASE is required for fallback")

    model = os.getenv("CPA_IMAGE_MODEL", "").strip()
    if not model:
        raise RuntimeError("CPA_IMAGE_MODEL is required for fallback")

    return api_key, api_base, model


def send_cpa_request(
    api_key: str,
    api_base: str,
    model: str,
    prompt: str,
    input_image: Path | None,
    timeout: float,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "model": model,
        "prompt": prompt,
    }

    if input_image and input_image.is_file():
        data_url = read_image_data_url(input_image)
        payload["image"] = data_url
        payload["input_image"] = data_url
        payload["input_images"] = [data_url]

    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url=f"{api_base}/images/generations",
        data=body,
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {api_key}",
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


def main() -> int:
    args = parse_args()

    try:
        load_persistent_env()
        payload = read_payload(args.payload_file)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    hf_result, hf_error = run_hf_call(args)
    if hf_result:
        images = collect_image_candidates(hf_result.get("result"))
        if images:
            output = {
                "provider": "huggingface-space",
                "space": hf_result.get("space", args.space),
                "images": images,
                "raw": hf_result,
            }
            json.dump(output, sys.stdout, indent=2, ensure_ascii=False)
            sys.stdout.write("\n")
            return 0
        hf_error = "HF call succeeded but no image output detected"

    try:
        api_key, api_base, model = resolve_cpa_config()
        prompt = extract_prompt(payload)
        if not prompt:
            raise RuntimeError("payload does not include a prompt; cannot run CPA image fallback")
        input_image = find_input_image(payload)
        cpa_result = send_cpa_request(api_key, api_base, model, prompt, input_image, args.timeout)
    except Exception as exc:
        print(f"error: hf-image failed ({hf_error}); fallback failed ({exc})", file=sys.stderr)
        return 1

    images = collect_image_candidates(cpa_result)
    output = {
        "provider": "cpa-fallback",
        "model": model,
        "images": images,
        "raw": cpa_result,
    }
    if hf_error:
        output["fallback_reason"] = hf_error

    json.dump(output, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
