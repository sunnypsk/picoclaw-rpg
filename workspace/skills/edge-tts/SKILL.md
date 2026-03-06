---
name: edge-tts
description: Generate spoken audio from text using the public Hugging Face Space innoai/Edge-TTS-Text-to-Speech. Use when the user asks for text-to-speech, TTS, spoken audio generation, a voiceover, read-aloud audio, or specifically mentions Edge TTS or this Space URL.
homepage: https://huggingface.co/spaces/innoai/Edge-TTS-Text-to-Speech
metadata: {"nanobot":{"emoji":"🔊","requires":{"bins":["python3"]}}}
---

# Edge TTS

Use this skill for text-to-speech workflows on the `innoai/Edge-TTS-Text-to-Speech` Hugging Face Space.

## Space

- Default Space: `innoai/Edge-TTS-Text-to-Speech`
- Public Space; `HF_TOKEN` is usually optional, but can still be used if needed.
- The underlying Hugging Face helpers auto-load `~/.picoclaw/.env` by default, or `$PICOCLAW_HOME/.env` when `PICOCLAW_HOME` is set.
- In the default Docker Compose setup, that persistent env file path is `docker/data/.env` on the host.

## Implementation

This workflow skill delegates execution to the base `huggingface-spaces` skill.

- Inspect API first:

```bash
python3 workspace/skills/huggingface-spaces/scripts/inspect_space.py "innoai/Edge-TTS-Text-to-Speech"
```

- Call API with a positional payload list:

```bash
python3 workspace/skills/huggingface-spaces/scripts/call_space.py \
  "innoai/Edge-TTS-Text-to-Speech" \
  --payload-file payload.json
```

Use `--timeout 300` for slow starts.

## Payload guidance

Send a JSON list with positional inputs in this order:

1. text
2. voice
3. rate
4. pitch

Rules:

- `voice` is required; inspect first to discover valid choices.
- If the user does not specify a voice, choose one that matches the requested language/accent after inspecting the available options.
- `rate` is an integer from `-100` to `100`; default to `0` unless the user asks for faster/slower speech.
- `pitch` is an integer from `-100` to `100`; default to `0` unless the user asks for higher/lower pitch.

Example payload:

```json
[
  "Hello! This is a short demo.",
  "en-US-AvaMultilingualNeural - en-US (Female)",
  0,
  0
]
```

## Response guidance

- Return the generated audio file path or URL.
- If the Space also returns a warning/message, include it only when it is non-empty and relevant.
- Mention the selected voice when that helps the user verify the output.


