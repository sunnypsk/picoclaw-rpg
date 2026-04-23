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
- Use a `media://...` ref from the current conversation when the user uploaded an image in chat.
- The canonical Momonga appearance reference sheet is `skills/generate-image/assets/momonga_refs_sheet.png`.
- For selfie requests or any Momonga image where her visible appearance should stay consistent, pass that path as `image`.
- If the user already supplied an explicit source image, use that image instead of the Momonga reference sheet.
- Do not attach the Momonga reference sheet for scenery-only images or images that do not depict Momonga.
- Do not use Hugging Face Spaces, Nano Banana, or any Hugging Face path for this skill.
- Do not guess between multiple possible source images. Ask or choose one explicit image only.
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
- `size`
- `quality`
- `style`
- `background`
- `timeout_seconds`

Ratio and resolution guidance:
- Use `aspect_ratio` for composition, for example `1:1`, `4:3`, `3:4`, `16:9`, or `9:16`.
- Use `size` for output resolution, for example `1024x1024`, `1536x1024`, or `1024x1536`.
- If both are provided, treat `aspect_ratio` as the framing intent and `size` as the concrete pixel target.
- If the user asks for square, portrait, landscape, wallpaper, story, or thumbnail formats, translate that into an explicit ratio and usually an explicit size.

Examples:
- square avatar: `aspect_ratio: 1:1`, `size: 1024x1024`
- portrait selfie: `aspect_ratio: 3:4`, `size: 1024x1536`
- desktop wallpaper: `aspect_ratio: 16:9`, `size: 1536x1024`
- phone story: `aspect_ratio: 9:16`, `size: 1024x1536`

## Sending Back To Chat

- `generate_image` returns media refs. PicoClaw will send those images back through the current channel automatically.
- If the conversation is on Telegram or WhatsApp, treat the attachment as the primary answer.
- Send a short follow-up text only when needed, for example to say what was generated or what edit was applied.
- Keep any follow-up text concise and platform-appropriate.
