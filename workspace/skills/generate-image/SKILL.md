---
name: generate-image
description: Generate or edit images with CPA image APIs or chat completions and send the result back to the user in chat. Use when the user asks for image generation, image editing, restyling, prompt-plus-image transforms, or wants the finished image returned on Telegram or WhatsApp.
---

# Generate Image

Use this skill for image creation and image editing through CPA image APIs or chat completions.

## When to use

Use it when the user asks to:
- generate an image
- edit, transform, or restyle an image
- do prompt-plus-image generation
- get the generated image sent back in the current chat

## Setup

- Set `CPA_API_BASE`, `CPA_API_KEY`, and `CPA_IMAGE_MODEL` in `~/.picoclaw/.env` or `$PICOCLAW_HOME/.env`.
- Never print, echo, or expose `CPA_API_KEY`.

## Primary workflow

Use the `generate_image` tool.

Rules:
- Pass `prompt` every time.
- For image editing, pass one explicit source image using `image`, `input_image`, or `input_images` with exactly one item.
- When the user uploaded images in the current turn, prefer the exact `media://...` ref shown in the live prompt.
- `media://current` is only safe when the live prompt shows exactly one current uploaded image.
- The canonical Momonga appearance reference sheet is `skills/generate-image/assets/momonga_refs_sheet.png`.
- For selfie requests or any Momonga image where her visible appearance should stay consistent, pass that path as `image`.
- If the user already supplied an explicit source image, use that image instead of the Momonga reference sheet.
- Do not attach the Momonga reference sheet for scenery-only images or images that do not depict Momonga.
- Do not use Hugging Face Spaces, Nano Banana, or any Hugging Face path for this skill.
- Do not guess between multiple possible source images. Choose one explicit image only.
- Do not combine the Momonga reference sheet with another explicit image. This tool safely supports only one explicit input image.

Example tool input:

```json
{
  "prompt": "Turn this into a cinematic watercolor poster",
  "image": "media://abc123"
}
```

Optional passthrough fields:
- `aspect_ratio`
- `quality`
- `background`
- `timeout_seconds`

Quality guidance:
- Omit `quality` for ordinary chat image requests unless the user asks for a specific quality level.
- Use `quality: low` when speed matters, especially for WhatsApp or casual one-image requests.
- Use `quality: high` only when the user explicitly asks for high quality, print/detail, or a premium ad/poster result.
- Do not repeatedly retry an expensive high-quality request after an endpoint timeout.

Timeout avoidance:
- Prefer concise, image-native prompts. Avoid asking for many tiny readable labels, dense annotations, exact shop-sign text, maps with legends, or large crowds with many individually described objects.
- If the user asks for a complex scene, preserve the main subject and style but compress the prompt before calling `generate_image`.
- For text-heavy or label-heavy requests, ask for visual impressions of signs or labels rather than accurate readable text.
- If `generate_image` returns a provider timeout such as `408`, `504`, or `524`, do not immediately repeat the same prompt. Retry at most once with a shorter prompt, `quality: low`, `background: auto`, and a common ratio such as `1:1`.
- If the simplified retry also times out, explain that the upstream image service timed out and offer a simpler prompt; do not keep retrying in the same turn.

Prompt rewrite examples:
- Instead of: `extremely complex fantasy city map with thousands of labels, icons, legends, dense annotations`
- Use: `fantasy city map illustration, old parchment style, winding roads and rivers, decorative border, a few symbolic district markers, no readable labels`
- Instead of: `90s Hong Kong mall with many readable shop signs and crowded detailed storefronts`
- Use: `retro indoor shopping mall atrium inspired by 1990s East Asia, warm film photo look, escalator, marble floor, a few shoppers, simple shopfront shapes, no readable text`
- If a location-specific mall prompt still times out, remove the exact location and era terms on the retry while preserving the visual mood.

Ratio guidance:
- Use `aspect_ratio` for composition, for example `1:1`, `4:3`, `3:4`, `16:9`, or `9:16`.
- If the user asks for square, portrait, landscape, wallpaper, story, or thumbnail formats, translate that into an explicit ratio.
- The helper will infer an API-compatible output size from common aspect ratios when needed.

Examples:
- square avatar: `aspect_ratio: 1:1`
- portrait selfie: `aspect_ratio: 3:4`
- desktop wallpaper: `aspect_ratio: 16:9`
- phone story: `aspect_ratio: 9:16`

## Sending Back To Chat

- `generate_image` returns media refs. PicoClaw will send those images back through the current channel automatically.
- If the conversation is on Telegram or WhatsApp, treat the attachment as the primary answer.
- Send a short follow-up text only when needed, for example to say what was generated or what edit was applied.
- Keep any follow-up text concise and platform-appropriate.
