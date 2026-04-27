---
name: generate-image
description: Generate or edit images with the configured TUZHI image APIs and send the result back to the user in chat. Use when the user asks for image generation, image editing, restyling, prompt-plus-image transforms, or wants the finished image returned on Telegram or WhatsApp.
---

# Generate Image

Use this skill for image creation and image editing through the configured TUZHI image APIs.

## When to use

Use it when the user asks to:
- generate an image
- edit, transform, or restyle an image
- do prompt-plus-image generation
- get the generated image sent back in the current chat

## Setup

- Set `TUZHI_KEY`, `TUZHI_IMAGE_MODEL`, `TUZHI_IMAGE_GEN_BASE`, and `TUZHI_IMAGE_EDIT_BASE` in `~/.picoclaw/.env` or `$PICOCLAW_HOME/.env`.
- Legacy `CPA_API_BASE`, `CPA_API_KEY`, and `CPA_IMAGE_MODEL` are used only when no TUZHI image variables are configured.
- Never print, echo, or expose `TUZHI_KEY` or `CPA_API_KEY`.

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

## Prompt Construction

Do not pass chatty user wording directly when a clearer visual prompt would be safer. Convert the request into a detailed, image-native prompt before calling `generate_image`.

For complex scenes, keep the user's meaningful details. Organize long requests into visual priorities instead of shortening them by default. Include specific text, logos, signs, materials, place and era cues, composition, lighting, character details, and background elements when they matter to the request.

Use this detailed structure when helpful:
- Main subject: the primary thing to draw.
- Style/medium: photo, illustration, 3D render, product shot, poster, etc.
- Composition: close-up, wide shot, top-down, centered, negative space, etc.
- Lighting/mood: soft studio light, warm film look, dramatic rain, playful, minimal, etc.
- Key details: the important details that define the image, including requested text, logos, signs, labels, props, materials, setting, and era.
- Constraints: preserve supplied subject, include required text/logos/signage, avoid modern objects, etc.

Specificity policy:
- If the user's prompt is already detailed and specific, preserve its detail and only normalize formatting.
- If the prompt is vague, add useful visual specificity.
- If the prompt is long or overloaded, restructure it into a clear visual prompt while keeping the user's requested details.
- Do not invent extra characters or side stories that conflict with the user's request. Real text, logos, brands, slogans, signs, and labels are allowed when the user asks for them or they naturally fit the requested scene.

For edits:
- State the edit target clearly.
- Repeat invariants in the prompt, for example `keep the cake shape, color, logo plaque, and toppings unchanged; only replace the background`.
- Use exactly one source image. Treat it as the edit target unless the user explicitly says it is only a style reference.

Detailed prompt handling:
- Keep requested readable labels, exact text blocks, dense annotations, legends, map keys, shop signs, storefront text, logos, and brand cues when they matter.
- Preserve detailed people, objects, and scene micro-details when they help the image.
- For multiple competing styles, make the relationship explicit, for example primary style, secondary influence, or split-scene treatment.

Quality guidance:
- Omit `quality` for ordinary chat image requests unless the user asks for a specific quality level.
- Do not use `quality: low`.
- Use `quality: high` when the user asks for high quality, print/detail, or a premium ad/poster result.
- If the user asks for a specific non-low quality level such as `medium`, `high`, or `auto`, pass it through.

Timeout handling:
- Do not simplify a prompt preemptively only because it is detailed.
- If `generate_image` returns a provider timeout such as `408`, `504`, or `524`, do not immediately repeat the same prompt.
- Retry at most once with a more focused but still detailed prompt, `background: auto`, and a common ratio such as `1:1`. Do not downgrade to `quality: low`.
- Do not drop user-required text, logos, signage, identity details, or scene requirements during a timeout retry.
- If the retry also times out, explain that the upstream image service timed out and offer a smaller-scope prompt that still preserves the user's required details; do not keep retrying in the same turn.

Prompt organization examples:
- Instead of: `extremely complex fantasy city map with thousands of labels, icons, legends, dense annotations`
- Use: `detailed fantasy city map illustration in old parchment style, winding roads, rivers, city walls, district boundaries, decorative border, readable labels for the main districts, a clear legend, icons for markets, temples, docks, gates, and towers`
- Instead of: `90s Hong Kong mall with many readable shop signs and crowded detailed storefronts`
- Use: `detailed retro indoor shopping mall atrium inspired by 1990s East Asia, warm film photo look, escalators, marble floor, crowded storefronts, readable shop signs, illuminated logos, hanging banners, directory boards, shoppers, and period-accurate decor`
- Instead of: `Realistic 1980s Hong Kong street documentary photograph, Mong Kok at midday, bright overhead sunlight, old double-decker bus, red taxi, crowded pavement, dense hanging signboards, weathered buildings, faded color film grain, no modern objects`
- Use: `realistic 1980s Hong Kong street documentary photograph in Mong Kok at midday, bright overhead sunlight, old double-decker bus, red taxi, crowded pavement, dense hanging signboards with readable text, weathered shopfronts, period-accurate logos, faded color film grain, no modern objects`

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
