---
name: presentation
description: Generate elegant animated offline HTML presentations as portable ZIP packages. Use when the user asks for a browser-based deck, pitch deck, teaching deck, report deck, or animated slides that should run on Mac and Windows without PowerPoint.
---

# Presentation

Use this skill to create portable animated HTML presentations.

The output is not PPTX. It is an offline-ready HTML deck package that can be opened in a modern browser on Mac or Windows.

## When to use

Use this skill when the user asks to:
- create a presentation, deck, pitch deck, teaching deck, report deck, or animated slides
- make a portable browser-based presentation
- turn an outline or topic into an elegant visual deck
- generate a ZIP package that can be shared and opened locally

Do not use the removed `generate-slides` skill or any PPTX workflow for this task.

## Workflow

1. Clarify only if the topic, audience, or number of slides is missing and cannot be inferred.
2. Create a concise slide plan before calling the tool.
3. Use short slide text. Each slide should have one clear message.
4. Use the `generate_image` tool first only when an image would materially improve understanding or visual impact.
5. Call `generate_presentation` with a structured slide spec.
6. Check the tool result for `quality_warnings`. If warnings mention long text, too many items, missing alt text, or dense slides, revise the slide spec and generate again.
7. When local browser rendering is available, open or screenshot the generated `index.html` before finalizing. Check for awkward title wraps, overlapping text, missing images, tiny body text, and weak contrast.
8. Tell the user where the ZIP and `index.html` were created, and mention whether a visual check was completed.

## Design rules

- Make the deck elegant, eye-catching, and easy to understand.
- Prefer strong hierarchy, generous spacing, high contrast, and restrained motion.
- Use varied layouts instead of repeated bullet slides.
- Keep text short: titles should be direct, bullets should be scannable.
- For teaching, sports, school, youth, or classroom decks, prefer the `classroom` theme.
- Keep CJK/Cantonese titles especially short, ideally 12 to 18 characters.
- Keep metric slides to 3 items, comparison slides to 2 to 4 items, and timeline slides to 3 to 5 steps.
- Use images only when they carry meaning; decorative images are optional, not required.
- Do not generate arbitrary HTML, JavaScript, or CSS yourself.
- Do not use remote image URLs. Use local workspace files or `media://...` refs.
- Do not depend on CDN, npm, a server, or PowerPoint.

## Tool input

Call `generate_presentation`.

Required:
- `title`
- `slides`

Useful optional fields:
- `theme`: `executive`, `studio`, `signal`, or `classroom`
- `output`: `offline_zip` by default
- `language`
- `slug`

Supported slide layouts:
- `cover`
- `section`
- `title-bullets`
- `two-column`
- `image-hero`
- `comparison`
- `timeline`
- `metrics`
- `quote`
- `closing`

Supported animation presets:
- `auto`
- `none`
- `fade-up`
- `stagger`
- `scale-in`
- `draw-line`
- `count-up`
- `spotlight`

Use `spotlight` for a cover, image hero, or single key idea that should feel more energetic. Use `draw-line` for timeline or process slides, and `count-up` only for numeric metric slides.

## Example

```json
{
  "title": "Q3 Product Review",
  "theme": "executive",
  "output": "offline_zip",
  "slides": [
    {
      "layout": "cover",
      "title": "Q3 Product Review",
      "subtitle": "What changed, what worked, and what we do next"
    },
    {
      "layout": "title-bullets",
      "title": "The story in three points",
      "bullets": [
        "Activation improved after onboarding cleanup",
        "Support volume dropped after clearer product states",
        "The next constraint is reporting clarity"
      ],
      "animation": "stagger"
    },
    {
      "layout": "metrics",
      "title": "Progress became visible",
      "items": [
        {"value": "+18%", "title": "Activation", "body": "Measured against the previous quarter"},
        {"value": "-22%", "title": "Support tickets", "body": "Mostly from fewer setup questions"},
        {"value": "3", "title": "Next bets", "body": "Reporting, speed, and team workflows"}
      ],
      "animation": "count-up"
    },
    {
      "layout": "closing",
      "title": "Make the next quarter easier to explain",
      "subtitle": "A clearer product story will make every review faster."
    }
  ]
}
```
