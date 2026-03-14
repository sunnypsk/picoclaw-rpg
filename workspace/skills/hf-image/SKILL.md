---
name: hf-image
description: Generate and edit images with Hugging Face Spaces, using Nano Banana first for text-to-image, image-to-image, photo restyling, and multimodal image workflows.
homepage: https://huggingface.co/spaces
metadata: {"nanobot":{"emoji":"🖼️","requires":{"bins":["python3"]}}}
---

# HF Image

Use this skill for image-focused workflows on Hugging Face Spaces.

## When to use

Use it when the user asks to:
- generate an image
- edit, restyle, or transform an image
- do image-to-image or prompt-plus-image generation
- use Nano Banana or another image generation Space URL

## Space selection

- If the user provides a specific Space URL or `owner/space` slug, use that Space.
- Otherwise use `multimodalart/nano-banana` as the first/default choice for general multimodal image generation and editing.

## Setup

- For fallback mode, set `CPA_API_BASE`, `CPA_API_KEY`, and `CPA_IMAGE_MODEL` in `~/.picoclaw/.env` (or `$PICOCLAW_HOME/.env`).
- Never print, echo, or expose `CPA_API_KEY`.

## Implementation

This workflow skill delegates execution to the base `huggingface-spaces` skill and supports CPA fallback:

- Inspect API:

```bash
python3 workspace/skills/huggingface-spaces/scripts/inspect_space.py "multimodalart/nano-banana"
```

- Generate/edit with fallback wrapper (recommended):

```bash
python3 workspace/skills/hf-image/scripts/generate_with_fallback.py \
  --space "multimodalart/nano-banana" \
  --payload-file payload.json \
  --timeout 300
```

Fallback behavior:
- It tries the selected Hugging Face Space first.
- If that fails, or no image output is detected, it sends a fallback request to `CPA_API_BASE/images/generations` using `CPA_API_KEY` and `CPA_IMAGE_MODEL`.

## Payload guidance

- For text-only generation, send a prompt.
- For image editing, send the prompt plus uploaded image files using `{"$file":"path/to/image.png"}`.
- Always inspect the endpoint first unless the payload shape is already known.

Example payload:

```json
{
  "prompt": "Turn this into a cinematic watercolor poster",
  "image": {"$file": "input.png"}
}
```

## Response guidance

- Return whether output came from the Space or CPA fallback.
- Return generated file URLs or paths when present.
- Keep the result concise and mention if the Space timed out, produced no image output, or required fallback.
