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
- In Docker images built from this repo's standard runtime, `node` and `npm` should already be available.
- On older deployments or host machines, verify availability with `node -v` and `npm -v` or quote the exact command error before saying the environment is missing them.

## Setup

Install the bundled dependency in the current workspace before first use, or whenever
`workspace/skills/generate-slides/node_modules/pptxgenjs` is missing:

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
3. Verify the runtime with `node -v` and `npm -v` when the environment is uncertain.
4. If `workspace/skills/generate-slides/node_modules/pptxgenjs` is missing, run:

```bash
npm ci --prefix workspace/skills/generate-slides
```

5. Include optional fields when they help:
   `theme`, `variant`, `lang`, `notes`, `sources`, and for image slides `image_fit`.
6. Run the helper.

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
- Choose a top-level `theme` explicitly unless the user asks for the legacy default look.
- Map theme choice to deck intent:
  `classic` for formal updates and status decks,
  `editorial` for product narratives and showcases,
  `contrast` for keynotes, workshops, and high-emphasis decks.
- Vary slide `variant` values across the deck when it improves pacing instead of reusing one layout everywhere.
- Keep `classic` plus default variants when the user wants continuity with the existing house style.
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

- The generator now supports multiple built-in themes and per-slide layout variants.
- Omitted `theme` and `variant` fields fall back to the legacy `classic` look.
- Save the generated deck locally; this skill does not return the file through chat channels.
- If `node` is unavailable, explain that this skill must run in a Node-capable runtime.
- Do not blame an LLM/API/timeout failure on missing `node` unless a runtime check or command failure has confirmed that separately.
