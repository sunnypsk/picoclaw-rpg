---
name: stt
description: Transcribe local audio files into raw speech-to-text by sending them to the CPA OpenAI-compatible endpoint with model google/gemini-3-flash-preview. Use when the user asks to transcribe audio, do speech-to-text, STT, voice-note transcription, or wants the original spoken text only with no translation or summary.
metadata: {"nanobot":{"emoji":"🎙️","requires":{"bins":["python3"]}}}
---

# STT

Use this skill to turn a local audio file into a plain transcript.

## Setup

- Set `CPA_API_KEY` before use.
- Set `CPA_API_BASE` to your CPA OpenAI-compatible endpoint before use.
- Optional: set `CPA_STT_MODEL` to override the default `google/gemini-3-flash-preview`.
- The helper auto-loads `~/.picoclaw/.env` by default, or `$PICOCLAW_HOME/.env` when `PICOCLAW_HOME` is set.
- In the default Docker Compose setup, that persistent env file path is `docker/data/.env` on the host.
- Never print, echo, or expose `CPA_API_KEY`.

## Workflow

1. Resolve the local audio file path.
2. Run the helper script:

```bash
python3 workspace/skills/stt/scripts/transcribe_audio.py "/path/to/audio.ogg"
```

If the user explicitly wants speaker labels and/or timestamps, enable the matching flags:

```bash
python3 workspace/skills/stt/scripts/transcribe_audio.py "/path/to/audio.ogg" --speaker-labels --timestamps
```

On Windows, use `py -3` if `python3` is unavailable.

3. Return the transcript text only.

## Input rules

- v1 supports local files only.
- Accepted suffixes: `.mp3`, `.wav`, `.ogg`, `.m4a`, `.flac`, `.aac`, `.wma`, `.opus`.
- Audio formats are sent through best-effort. If the endpoint rejects a format, report that cleanly.

## Output rules

- By default, return only the transcription.
- Do not translate.
- Do not summarize.
- Add speaker labels only when the user explicitly asks for them and the result is reasonably supported by the audio.
- Add timestamps only when the user explicitly asks for them and they can be estimated reasonably.
- Do not wrap the answer in Markdown or code fences.

## Notes

- The helper already includes the base transcription prompt; use `--speaker-labels` and `--timestamps` when the user asks for those extras.
- If the audio is partly unclear, `[inaudible]` is acceptable only for genuinely unclear segments.


