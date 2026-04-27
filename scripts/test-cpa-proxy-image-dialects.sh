#!/bin/sh
set -eu

# Compare ClipProxyAPI/OpenAI-compatible image-generation dialects against the
# same configured CPA endpoint without printing credentials.
#
# Default usage on the Docker host:
#   CONTAINER=picoclaw-rpg REPEAT=1 scripts/test-cpa-proxy-image-dialects.sh
#
# To run from inside the container instead:
#   PICOCLAW_TEST_INSIDE=1 scripts/test-cpa-proxy-image-dialects.sh

CONTAINER="${CONTAINER:-picoclaw-rpg}"
PROMPT="${PROMPT:-A simple smoke-test image: a small red cube centered on a plain white background, clean composition, low detail.}"
REPEAT="${REPEAT:-1}"
RESPONSES_TEXT_MODEL="${RESPONSES_TEXT_MODEL:-gpt-5.5}"
CASE_FILTER="${CASE_FILTER:-all}"
CURL_MAX_TIME="${CURL_MAX_TIME:-360}"

if [ "${PICOCLAW_TEST_INSIDE:-0}" != "1" ]; then
  if ! command -v docker >/dev/null 2>&1; then
    echo "docker is required on the host. To run inside the container, set PICOCLAW_TEST_INSIDE=1." >&2
    exit 1
  fi

  docker exec \
    -i \
    -e PICOCLAW_TEST_INSIDE=1 \
    -e PROMPT="$PROMPT" \
    -e REPEAT="$REPEAT" \
    -e RESPONSES_TEXT_MODEL="$RESPONSES_TEXT_MODEL" \
    -e CASE_FILTER="$CASE_FILTER" \
    -e CURL_MAX_TIME="$CURL_MAX_TIME" \
    "$CONTAINER" sh -s < "$0"
  exit $?
fi

ENV_FILE="${PICOCLAW_ENV_FILE:-/home/picoclaw/.picoclaw/.env}"
if [ ! -f "$ENV_FILE" ]; then
  echo "Picoclaw env file not found: $ENV_FILE" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

: "${CPA_API_BASE:?CPA_API_BASE is required}"
: "${CPA_API_KEY:?CPA_API_KEY is required}"
: "${CPA_IMAGE_MODEL:?CPA_IMAGE_MODEL is required}"

BASE="${CPA_API_BASE%/}"
IMAGE_MODEL="$CPA_IMAGE_MODEL"
OUT_DIR="${OUT_DIR:-/tmp/cpa-proxy-image-dialects-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$OUT_DIR"

echo "out_dir=$OUT_DIR"
echo "image_model=$IMAGE_MODEL"
echo "responses_text_model=$RESPONSES_TEXT_MODEL"
echo "repeat=$REPEAT"
echo "case_filter=$CASE_FILTER"
echo

export OUT_DIR IMAGE_MODEL PROMPT RESPONSES_TEXT_MODEL

python3 - <<'PY'
import json
import os

out = os.environ["OUT_DIR"]
prompt = os.environ["PROMPT"]
image_model = os.environ["IMAGE_MODEL"]
responses_text_model = os.environ["RESPONSES_TEXT_MODEL"]

payloads = {
    "images_generations_official": {
        "model": image_model,
        "prompt": prompt,
        "quality": "low",
        "background": "auto",
        "size": "1024x1024",
    },
    "responses_official_tool_text_model": {
        "model": responses_text_model,
        "input": "Generate an image: " + prompt,
        "tools": [
            {
                "type": "image_generation",
                "size": "1024x1024",
                "quality": "low",
                "background": "auto",
            }
        ],
        "tool_choice": {"type": "image_generation"},
    },
    "responses_direct_image_model": {
        "model": image_model,
        "input": "Generate an image: " + prompt,
        "size": "1024x1024",
        "quality": "low",
        "background": "auto",
    },
    "responses_tool_image_model": {
        "model": image_model,
        "input": "Generate an image: " + prompt,
        "tools": [
            {
                "type": "image_generation",
                "size": "1024x1024",
                "quality": "low",
                "background": "auto",
            }
        ],
        "tool_choice": {"type": "image_generation"},
    },
    "chat_direct_image_model_minimal": {
        "model": image_model,
        "messages": [
            {"role": "user", "content": "Generate an image: " + prompt},
        ],
    },
    "chat_direct_image_model_modalities": {
        "model": image_model,
        "messages": [
            {"role": "user", "content": "Generate an image: " + prompt},
        ],
        "modalities": ["image"],
        "size": "1024x1024",
        "quality": "low",
        "background": "auto",
    },
    "chat_tool_image_model": {
        "model": image_model,
        "messages": [
            {"role": "user", "content": "Generate an image: " + prompt},
        ],
        "tools": [
            {
                "type": "image_generation",
                "size": "1024x1024",
                "quality": "low",
                "background": "auto",
            }
        ],
        "tool_choice": {"type": "image_generation"},
    },
}

for name, payload in payloads.items():
    with open(os.path.join(out, name + ".json"), "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
PY

case_enabled() {
  case "$CASE_FILTER" in
    all) return 0 ;;
    *"$1"*) return 0 ;;
    *) return 1 ;;
  esac
}

summarize_response() {
  response="$1"
  image_prefix="$2"

  python3 - "$response" "$image_prefix" <<'PY'
import base64
import json
import os
import re
import sys

path = sys.argv[1]
image_prefix = sys.argv[2]

if not os.path.exists(path):
    print("response_missing=true")
    raise SystemExit(0)

raw = open(path, "rb").read()
print(f"response_bytes={len(raw)}")

try:
    data = json.loads(raw.decode("utf-8", errors="replace"))
except Exception as exc:
    preview = raw[:220].decode("utf-8", errors="replace").replace("\n", " ")
    print(f"json_parse=failed:{type(exc).__name__}")
    print(f"response_preview={preview}")
    raise SystemExit(0)

if isinstance(data, dict):
    print("top_level_keys=" + ",".join(sorted(data.keys())[:30]))
    if "error" in data:
        err = data["error"]
        if isinstance(err, dict):
            msg = str(err.get("message") or err.get("code") or err)[:260].replace("\n", " ")
        else:
            msg = str(err)[:260].replace("\n", " ")
        print("error=" + msg)

image_calls = 0
b64_candidates = []
url_candidates = []


def maybe_image_b64(value):
    if not isinstance(value, str) or len(value) < 500:
        return None
    s = value
    if s.startswith("data:image/"):
        comma = s.find(",")
        if comma >= 0:
            s = s[comma + 1:]
    s = re.sub(r"\s+", "", s)
    try:
        sample = base64.b64decode(s[:4096] + ("=" * ((4 - len(s[:4096]) % 4) % 4)), validate=False)
    except Exception:
        return None
    if sample.startswith(b"\x89PNG\r\n\x1a\n"):
        return ("png", s)
    if sample.startswith(b"\xff\xd8\xff"):
        return ("jpg", s)
    if sample.startswith(b"RIFF") and b"WEBP" in sample[:16]:
        return ("webp", s)
    return None


def walk(obj, path="$"):
    global image_calls
    if isinstance(obj, dict):
        if obj.get("type") == "image_generation_call":
            image_calls += 1
        for k, v in obj.items():
            key = str(k).lower()
            p = path + "." + str(k)
            if isinstance(v, str):
                found = maybe_image_b64(v)
                if found:
                    b64_candidates.append((p, found[0], found[1]))
                if key in ("url", "image_url") and v.startswith(("http://", "https://", "data:image/")):
                    url_candidates.append((p, v[:120]))
            walk(v, p)
    elif isinstance(obj, list):
        for i, v in enumerate(obj):
            walk(v, f"{path}[{i}]")


walk(data)

print(f"image_generation_calls={image_calls}")
print(f"image_base64_candidates={len(b64_candidates)}")
print(f"image_url_candidates={len(url_candidates)}")

if url_candidates:
    print("first_image_url_path=" + url_candidates[0][0])
    print("first_image_url_preview=" + url_candidates[0][1])

if b64_candidates:
    _, ext, b64 = b64_candidates[0]
    out = image_prefix + "." + ext
    with open(out, "wb") as f:
        f.write(base64.b64decode(b64 + ("=" * ((4 - len(b64) % 4) % 4))))
    print("image_saved=" + out)
PY
}

run_case() {
  name="$1"
  endpoint="$2"
  payload="$3"

  out="$OUT_DIR/$name.response.json"
  meta="$OUT_DIR/$name.meta.txt"

  echo "=== $name ==="
  echo "endpoint=$endpoint"
  date

  curl_status=0
  curl_output="$(curl -sS --connect-timeout 20 --max-time "$CURL_MAX_TIME" \
    -o "$out" \
    -w "HTTP=%{http_code}\nTIME=%{time_total}\n" \
    -H "Authorization: Bearer $CPA_API_KEY" \
    -H "Content-Type: application/json" \
    -H "User-Agent: picoclaw/1.0" \
    "$BASE$endpoint" \
    --data-binary "@$payload" 2>&1)" || curl_status="$?"

  printf "%s\n" "$curl_output" | tee "$meta"
  echo "curl_exit=$curl_status"
  summarize_response "$out" "$OUT_DIR/$name.image"
  echo "payload_saved=$payload"
  echo "response_saved=$out"
  date
  echo
}

i=1
while [ "$i" -le "$REPEAT" ]; do
  echo "######## repeat $i/$REPEAT ########"

  if case_enabled images; then
    run_case "r${i}_images_generations_official" "/images/generations" "$OUT_DIR/images_generations_official.json"
  fi
  if case_enabled responses_official; then
    run_case "r${i}_responses_official_tool_text_model" "/responses" "$OUT_DIR/responses_official_tool_text_model.json"
  fi
  if case_enabled responses_direct; then
    run_case "r${i}_responses_direct_image_model" "/responses" "$OUT_DIR/responses_direct_image_model.json"
  fi
  if case_enabled responses_tool; then
    run_case "r${i}_responses_tool_image_model" "/responses" "$OUT_DIR/responses_tool_image_model.json"
  fi
  if case_enabled chat_minimal; then
    run_case "r${i}_chat_direct_image_model_minimal" "/chat/completions" "$OUT_DIR/chat_direct_image_model_minimal.json"
  fi
  if case_enabled chat_modalities; then
    run_case "r${i}_chat_direct_image_model_modalities" "/chat/completions" "$OUT_DIR/chat_direct_image_model_modalities.json"
  fi
  if case_enabled chat_tool; then
    run_case "r${i}_chat_tool_image_model" "/chat/completions" "$OUT_DIR/chat_tool_image_model.json"
  fi

  i=$((i + 1))
done

echo "Done. Full responses are in: $OUT_DIR"
