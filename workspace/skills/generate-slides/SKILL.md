---
name: generate-slides
description: Generate new PowerPoint slide decks (.pptx) from a prompt, outline, or structured slide spec using bundled Node.js helpers powered by PptxGenJS. Use when the user asks to create a presentation, turn notes into slides, make a slide deck, or generate a local PowerPoint file.
metadata: {"nanobot":{"requires":{"bins":["node","npm"]}}}
---

# Generate Slides

Use this skill to create new slide decks with PptxGenJS.

## When to use

Use it when the user asks to:
- create a presentation or slide deck
- turn an outline, notes, or bullets into slides
- generate a local `.pptx` file
- make a PowerPoint deck for a meeting, report, pitch, class, or update

Do not use this skill to edit an existing `.pptx` file. This skill generates new decks only.

## Runtime

- This skill requires Node.js 18+ and `npm`.
- Prefer the repo's full Docker runtime because the default image does not include Node.js:

```bash
docker compose -f docker/docker-compose.full.yml run --rm picoclaw-agent sh
```

- On a host machine, only use this skill when `node` and `npm` are already available.

## Setup

Install the bundled dependency before first use:

```bash
npm ci --prefix workspace/skills/generate-slides
```

On Windows PowerShell:

```powershell
npm ci --prefix .\workspace\skills\generate-slides
```

## Workflow

1. Turn the user request into a compact JSON slide spec that follows
   `workspace/skills/generate-slides/references/slide_spec.md`.
2. Keep decks readable.
   Prefer 3-10 slides unless the user asks for more.
   Prefer one idea per slide.
   Use short bullets instead of dense paragraphs.
   Use local image paths only.
3. Include optional fields when they help:
   `lang`, `notes`, `sources`, and for image slides `image_fit`.
4. Run the helper.

Default output:
- If `--output` is omitted, the helper writes to
  `workspace/generated-slides/<safe-stem>.pptx`.

Explicit output:
- If `--output` is provided, the path may be absolute or relative to the repo root.
- The resolved output path must stay under `workspace/` or the helper fails.

Example:

```bash
node workspace/skills/generate-slides/scripts/generate_slides.mjs \
  --spec-file workspace/generated-slides/q2-product-update.json \
  --output workspace/generated-slides/q2-product-update.pptx
```

## Spec rules

- Generate JSON only. Do not ask the agent to write raw JavaScript.
- Use only the supported slide types from the reference file:
  `title`, `section`, `bullets`, `image`, `closing`
- Set a top-level `title`.
- Use `layout: "wide"` unless the user explicitly wants a standard 4:3 deck.
- Use `image_fit: "cover"` by default for image slides.
- Use `image_fit: "contain"` for screenshots, charts, or UI mockups when cropping would hurt readability.
- Use `lang` for non-English decks, especially CJK content.
- Use `notes` and `sources` when the user wants speaker notes, provenance, or presenter guidance.

## Output

- The helper writes a local `.pptx` file.
- Deck-level and slide-level `notes`/`sources` are written into speaker notes.
- The helper prints normalized JSON to stdout with:
  `ok`, `title`, `slide_count`, `output_path`, and `warnings`
- `warnings` flags likely dense slides such as long titles, too many bullets, crowded asides, or heavy image-panel text.

## Notes

- The built-in theme is intentionally opinionated and remains the default.
- Save the generated deck locally; this skill does not return the file through chat channels.
- If `node` is unavailable, explain that this skill must run in the full Docker image or another Node-capable runtime.
