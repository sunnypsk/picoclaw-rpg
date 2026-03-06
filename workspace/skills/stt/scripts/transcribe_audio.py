#!/usr/bin/env python3

import argparse
import base64
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

DEFAULT_API_BASE = "https://cpa.littlething.uk/v1"
DEFAULT_MODEL = "google/gemini-3-flash-preview"
ACCEPTED_FORMATS = {
    ".mp3": "mp3",
    ".wav": "wav",
    ".ogg": "ogg",
    ".m4a": "m4a",
    ".flac": "flac",
    ".aac": "aac",
    ".wma": "wma",
    ".opus": "opus",
}
BASE_PROMPT = (
    "Transcribe the provided audio exactly as spoken. Return only the raw transcription in the original "
    "language. Do not translate, summarize, add Markdown, code fences, or any extra explanation. Use "
    "[inaudible] only for genuinely unclear segments."
)


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
    parser = argparse.ArgumentParser(description="Transcribe a local audio file with the CPA endpoint.")
    parser.add_argument("audio_path", help="Path to a local audio file")
    parser.add_argument("--timeout", type=float, default=120.0, help="HTTP timeout in seconds (default: 120)")
    parser.add_argument("--speaker-labels", action="store_true", help="Request speaker labels when possible")
    parser.add_argument("--timestamps", action="store_true", help="Request timestamps when possible")
    return parser.parse_args()


def get_env(name: str, default: str = "") -> str:
    return os.getenv(name, default).strip()


def resolve_config() -> tuple[str, str, str]:
    api_key = get_env("CPA_API_KEY")
    if not api_key:
        raise RuntimeError("CPA_API_KEY is required")

    api_base = get_env("CPA_API_BASE", DEFAULT_API_BASE).rstrip("/")
    model = get_env("CPA_STT_MODEL", DEFAULT_MODEL)
    return api_key, api_base, model


def resolve_audio(path_arg: str) -> tuple[Path, str]:
    audio_path = Path(path_arg).expanduser().resolve()
    if not audio_path.exists():
        raise RuntimeError(f"audio file not found: {audio_path}")
    if not audio_path.is_file():
        raise RuntimeError(f"audio path is not a file: {audio_path}")

    suffix = audio_path.suffix.lower()
    audio_format = ACCEPTED_FORMATS.get(suffix)
    if not audio_format:
        supported = ", ".join(sorted(ACCEPTED_FORMATS))
        raise RuntimeError(f"unsupported audio format {suffix or '<none>'}; supported: {supported}")

    return audio_path, audio_format


def read_audio_base64(audio_path: Path) -> str:
    return base64.b64encode(audio_path.read_bytes()).decode("ascii")


def build_prompt(include_speaker_labels: bool, include_timestamps: bool) -> str:
    parts = [BASE_PROMPT]

    if include_speaker_labels:
        parts.append(
            "Include speaker labels only if multiple speakers are distinguishable from the audio; otherwise omit labels."
        )
    else:
        parts.append("Do not add speaker labels.")

    if include_timestamps:
        parts.append(
            "Include timestamps only when they can be estimated reasonably from the audio; otherwise omit timestamps."
        )
    else:
        parts.append("Do not add timestamps.")

    return " ".join(parts)


def build_payload(
    model: str,
    audio_data: str,
    audio_format: str,
    include_speaker_labels: bool,
    include_timestamps: bool,
) -> dict[str, Any]:
    prompt = build_prompt(include_speaker_labels, include_timestamps)
    return {
        "model": model,
        "temperature": 0,
        "messages": [
            {
                "role": "system",
                "content": "You are a precise speech-to-text transcription system.",
            },
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": prompt},
                    {
                        "type": "input_audio",
                        "input_audio": {
                            "data": audio_data,
                            "format": audio_format,
                        },
                    },
                ],
            },
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


def extract_text(value: Any) -> str:
    if isinstance(value, str):
        return value.strip()

    if isinstance(value, list):
        parts = []
        for item in value:
            text = extract_text(item)
            if text:
                parts.append(text)
        return "\n".join(parts).strip()

    if isinstance(value, dict):
        for key in ("text", "output_text"):
            text_value = value.get(key)
            if isinstance(text_value, str) and text_value.strip():
                return text_value.strip()

        content_value = value.get("content")
        text = extract_text(content_value)
        if text:
            return text

        parts_value = value.get("parts")
        text = extract_text(parts_value)
        if text:
            return text

    return ""


def extract_transcript(response_data: dict[str, Any]) -> str:
    choices = response_data.get("choices")
    if isinstance(choices, list) and choices:
        message = choices[0].get("message", {})
        transcript = extract_text(message.get("content"))
        if transcript:
            return transcript

    output_text = response_data.get("output_text")
    if isinstance(output_text, str) and output_text.strip():
        return output_text.strip()

    output = response_data.get("output")
    transcript = extract_text(output)
    if transcript:
        return transcript

    candidates = response_data.get("candidates")
    transcript = extract_text(candidates)
    if transcript:
        return transcript

    raise RuntimeError("CPA API response did not include transcript text")


def main() -> int:
    args = parse_args()

    try:
        load_persistent_env()
        api_key, api_base, model = resolve_config()
        audio_path, audio_format = resolve_audio(args.audio_path)
        audio_data = read_audio_base64(audio_path)
        payload = build_payload(model, audio_data, audio_format, args.speaker_labels, args.timestamps)
        response_data = send_request(api_key, api_base, payload, args.timeout)
        transcript = extract_transcript(response_data)
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print(transcript)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

