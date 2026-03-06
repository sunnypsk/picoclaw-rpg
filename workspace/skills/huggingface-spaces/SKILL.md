---
name: huggingface-spaces
description: Call Gradio-based Hugging Face Spaces with HF_TOKEN using bundled Python helpers; inspect endpoints, send multimodal inputs, and return JSON/file outputs.
metadata: {"nanobot":{"emoji":"ðŸ¤—","requires":{"bins":["python3"]}}}
---

# Hugging Face Spaces

Use this skill to call Gradio-based Hugging Face Spaces programmatically.

## When to use

Use it when the user asks to:
- call a Hugging Face Space
- use a specific Space URL or `owner/space` slug
- send text, image, audio, video, or file inputs to a Gradio Space
- inspect a Space API before calling it

## Setup

- Set `HF_TOKEN` before use
- The helpers auto-load `~/.picoclaw/.env` by default, or `$PICOCLAW_HOME/.env` when `PICOCLAW_HOME` is set.
- In the default Docker Compose setup, that persistent env file path is `docker/data/.env` on the host.
- Install `gradio_client`:

```bash
python3 -m pip install gradio_client
```

On Windows, use `py -m pip install gradio_client` if `python3` is unavailable.

- Never print, echo, or expose `HF_TOKEN`

## Workflow

1. Resolve the Space URL or slug.
2. Inspect the API first:

```bash
python3 workspace/skills/huggingface-spaces/scripts/inspect_space.py "https://huggingface.co/spaces/multimodalart/nano-banana"
```

Use `--timeout 300` for slow-starting Spaces.

3. Build a payload JSON that matches the endpoint schema.
4. Call the endpoint:

```bash
python3 workspace/skills/huggingface-spaces/scripts/call_space.py \
  "multimodalart/nano-banana" \
  --api-name "/predict" \
  --payload-file payload.json
```

For unnamed endpoints, pass `--fn-index` instead:

```bash
python3 workspace/skills/huggingface-spaces/scripts/call_space.py \
  "owner/space" \
  --fn-index 0 \
  --payload-file payload.json
```

## Payload rules

- A JSON list is treated as positional inputs
- A JSON object is treated as named inputs
- Use `{"$file":"path/to/file"}` for file uploads
- File markers can be nested inside arrays and objects

Example payload:

```json
{
  "prompt": "Turn this into a watercolor painting",
  "image": {"$file": "input.png"}
}
```

## Output

- The helper prints normalized JSON to stdout
- File-like outputs keep returned paths and URLs
- `inspect_space.py` returns structured `api` metadata plus parsed endpoint selectors
- If endpoint selection is omitted and the Space exposes exactly one named or unnamed endpoint, the helper uses it automatically

## Notes

- Always inspect first unless the endpoint is already known
- For private or gated Spaces, `HF_TOKEN` must have access
- Prefer `call_space.py` over hand-written curl for queue handling and file uploads


