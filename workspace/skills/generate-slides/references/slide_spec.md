# Slide Spec

Use this reference when building the JSON input for
`scripts/generate_slides.mjs`.

## Top-level fields

```json
{
  "title": "Client Growth Plan",
  "subtitle": "Q2 2026 proposal",
  "filename": "client-growth-plan",
  "layout": "wide",
  "template_preset": "consulting-proposal",
  "lang": "en-US",
  "author": "Picoclaw",
  "company": "Example Co",
  "subject": "Client growth proposal",
  "notes": "Open with the executive summary, then pause before the recommendation slide.",
  "sources": [
    "CRM export - 2026-07-01",
    "Quarterly pipeline review - 2026-06-30"
  ],
  "slides": [
    {
      "type": "title",
      "title": "Client Growth Plan",
      "subtitle": "Q2 2026 proposal"
    }
  ]
}
```

- `title`: required string
- `subtitle`: optional string
- `filename`: optional output filename stem; `.pptx` is added automatically
- `layout`: optional `"wide"` or `"standard"`; defaults to `"wide"`
- `template_preset`: optional preset family selector. Supported values:
  `"classic"`, `"editorial"`, `"contrast"`, `"academic"`, `"brand-design"`,
  `"consulting-proposal"`, `"market-research"`, `"pitch-deck"`, `"project-kickoff"`
- `theme`: optional legacy alias for `"classic"`, `"editorial"`, or `"contrast"`
- `lang`: optional language tag such as `"en-US"` or `"zh-TW"`; applied to slide text
- `author`, `company`, `subject`: optional metadata strings
- `notes`: optional deck-level speaker notes
- `sources`: optional deck-level source strings
- `slides`: required array with one or more supported slide objects

Precedence rules:
- If `template_preset` is present, it overrides `theme`.
- If `template_preset` is omitted, the generator uses `theme`.
- If both are omitted, the generator falls back to the legacy `classic` family.

Deck-level `notes` and `sources` are merged into every slide's speaker notes.
Slide-level `notes` and `sources` are appended after deck-level content.

## Supported slide types

All slide types below also accept these optional fields:
- `notes`: slide-level speaker notes
- `sources`: slide-level source strings
- `variant`: optional layout variant for that slide type; explicit variants always override preset defaults

When `variant` is omitted, the generator chooses a preset-aware default.
For legacy theme-only specs and the `classic`, `editorial`, and `contrast`
families, those defaults remain the same as before.

### `title`

```json
{
  "type": "title",
  "variant": "hero-center",
  "title": "Quarterly Product Update",
  "subtitle": "Q2 2026",
  "kicker": "Board Review",
  "byline": "Prepared by Product and Finance"
}
```

- `title`: required
- `variant`: optional `"hero-left"` or `"hero-center"`
- `subtitle`, `kicker`, `byline`: optional

If `kicker` is omitted, the title slide renders without the kicker pill.

Common defaults:
- `hero-left`: `classic`, `editorial`, `contrast`, `academic`, `consulting-proposal`, `project-kickoff`
- `hero-center`: `brand-design`, `pitch-deck`

### `section`

```json
{
  "type": "section",
  "variant": "divider",
  "label": "SECTION",
  "title": "Wins and Risks",
  "subtitle": "What changed this quarter"
}
```

- `title`: required
- `variant`: optional `"divider"` or `"statement"`
- `label`: optional short divider label; overrides the localized default used by divider sections
- `subtitle`: optional

Common defaults:
- `divider`: most families
- `statement`: `brand-design`, `pitch-deck`

If `label` is omitted on divider sections, the generator uses a localized default
based on the deck `lang` when it recognizes the language, otherwise it falls back
to `SECTION`.

### `bullets`

```json
{
  "type": "bullets",
  "variant": "content-aside",
  "title": "Highlights",
  "body": "Three changes shaped the quarter.",
  "bullets": [
    "Revenue grew 22% year over year",
    "Activation rose after onboarding changes",
    "Enterprise pipeline doubled"
  ],
  "aside_title": "Watchouts",
  "aside_body": "Two issues still need active mitigation.",
  "aside_bullets": [
    "Hiring remains behind plan",
    "Renewal timing is still lumpy"
  ]
}
```

- `title`: required
- `variant`: optional `"content-aside"` or `"two-column"`
- `bullets`: required non-empty array of strings
- `body`, `aside_title`, `aside_body`: optional strings
- `aside_bullets`: optional array of strings

Notes:
- `content-aside` remains the general default for most presets.
- `pitch-deck` may default to `"two-column"` when no aside content is present and the main bullet list is story-like enough for a split layout.
- Use `variant: "two-column"` only when `aside_title`, `aside_body`, and `aside_bullets` are all omitted.

### `image`

```json
{
  "type": "image",
  "variant": "image-right",
  "title": "Prototype Snapshot",
  "image_path": "workspace/assets/prototype.png",
  "image_fit": "contain",
  "caption": "Early mobile checkout flow",
  "bullets": [
    "One-tap repeat purchase",
    "Clearer delivery timing",
    "Fewer abandoned carts in user tests"
  ]
}
```

- `title`: required
- `variant`: optional `"image-left"` or `"image-right"`
- `image_path`: required local file path
- `image_fit`: optional `"cover"` or `"contain"`; defaults to `"cover"`
- `caption`: optional string
- `bullets`: optional array of strings

Common defaults:
- `image-left`: `classic`, `editorial`, `contrast`, `academic`
- `image-right`: `brand-design`, `consulting-proposal`, `market-research`, `pitch-deck`, `project-kickoff`

Use `image_fit: "contain"` for screenshots, charts, and UI mockups that should
not be cropped.

### `closing`

```json
{
  "type": "closing",
  "variant": "minimal",
  "title": "Thank You",
  "subtitle": "Questions and discussion"
}
```

- `title`: required
- `variant`: optional `"card"` or `"minimal"`
- `subtitle`: optional

Common defaults:
- `card`: `classic`, `editorial`, `contrast`, `consulting-proposal`
- `minimal`: `academic`, `brand-design`, `market-research`, `pitch-deck`, `project-kickoff`

## Example using `template_preset: "consulting-proposal"`

```json
{
  "title": "Client Growth Plan",
  "subtitle": "Q2 2026 proposal",
  "filename": "client-growth-plan",
  "layout": "wide",
  "template_preset": "consulting-proposal",
  "lang": "en-US",
  "author": "Picoclaw",
  "company": "Example Co",
  "subject": "Client growth proposal",
  "notes": "Lead with the recommendation, then support it with the operating facts.",
  "sources": [
    "CRM export - 2026-07-01",
    "Quarterly pipeline review - 2026-06-30"
  ],
  "slides": [
    {
      "type": "title",
      "title": "Client Growth Plan",
      "subtitle": "Q2 2026 proposal",
      "kicker": "Executive Summary",
      "byline": "Strategy and Operations"
    },
    {
      "type": "section",
      "title": "What We Recommend",
      "subtitle": "A focused plan to increase pipeline efficiency"
    },
    {
      "type": "bullets",
      "title": "Three moves for the next quarter",
      "body": "The proposal concentrates resources on the highest-conversion segments.",
      "bullets": [
        "Rebalance spend toward higher-yield partner channels",
        "Tighten follow-up SLAs for mid-market opportunities",
        "Standardize the executive escalation path for late-stage deals"
      ],
      "aside_title": "Impact",
      "aside_bullets": [
        "Higher qualified pipeline coverage",
        "Better forecast confidence",
        "Clearer ownership by team"
      ]
    },
    {
      "type": "closing",
      "title": "Questions",
      "subtitle": "Decision points and next steps"
    }
  ]
}
```

## Example using `template_preset: "pitch-deck"`

```json
{
  "title": "Why This Market Opens Now",
  "subtitle": "Seed narrative - 2026",
  "filename": "why-this-market-opens-now",
  "layout": "wide",
  "template_preset": "pitch-deck",
  "lang": "en-US",
  "author": "Picoclaw",
  "notes": "Keep the pace fast and the copy concise.",
  "slides": [
    {
      "type": "title",
      "title": "Why This Market Opens Now",
      "subtitle": "Seed narrative - 2026",
      "kicker": "Fundraising Story",
      "byline": "Founding Team"
    },
    {
      "type": "section",
      "title": "The old workflow is breaking",
      "subtitle": "Teams are stitching together too many tools"
    },
    {
      "type": "bullets",
      "title": "Three signals we can build on",
      "bullets": [
        "AI adoption is shifting buyer expectations",
        "Category incumbents are still workflow-fragmented",
        "The distribution channel is already consolidating",
        "Design partners are asking for end-to-end automation"
      ]
    },
    {
      "type": "closing",
      "title": "We're building the default operating layer",
      "subtitle": "Questions, product demo, and next steps"
    }
  ]
}
```

## Legacy theme example

This remains valid for backward compatibility:

```json
{
  "title": "Quarterly Product Update",
  "subtitle": "Q2 2026",
  "filename": "quarterly-product-update",
  "layout": "wide",
  "theme": "editorial",
  "lang": "en-US",
  "author": "Picoclaw",
  "company": "Example Co",
  "subject": "Quarterly business review",
  "notes": "Lead with outcomes, then move into risks and requests.",
  "sources": [
    "Finance dashboard export - 2026-07-01",
    "Product analytics weekly summary - 2026-06-30"
  ],
  "slides": [
    {
      "type": "title",
      "variant": "hero-center",
      "title": "Quarterly Product Update",
      "subtitle": "Q2 2026",
      "kicker": "Board Review",
      "byline": "Prepared by Product and Finance"
    },
    {
      "type": "section",
      "variant": "statement",
      "title": "Topline Story",
      "subtitle": "What moved, what stalled, what changed"
    },
    {
      "type": "bullets",
      "variant": "two-column",
      "title": "Highlights",
      "body": "The quarter was driven by improved onboarding and enterprise demand.",
      "bullets": [
        "Revenue grew 22% year over year",
        "Activation improved after onboarding changes",
        "The enterprise pipeline doubled"
      ]
    },
    {
      "type": "closing",
      "variant": "minimal",
      "title": "Thank You",
      "subtitle": "Questions and discussion"
    }
  ]
}
```

## Traditional Chinese example using `template_preset: "project-kickoff"`

```json
{
  "title": "專案啟動簡報",
  "subtitle": "2026 年第二季",
  "filename": "專案啟動簡報",
  "layout": "wide",
  "template_preset": "project-kickoff",
  "lang": "zh-TW",
  "author": "Picoclaw",
  "notes": "先對齊目標與分工，再進入風險與里程碑。",
  "sources": [
    "啟動會議紀要 - 2026-07-01",
    "專案排程草案 - 2026-06-30"
  ],
  "slides": [
    {
      "type": "title",
      "title": "專案啟動簡報",
      "subtitle": "2026 年第二季",
      "kicker": "跨部門協作",
      "byline": "產品、工程與營運團隊"
    },
    {
      "type": "section",
      "title": "本次啟動要對齊什麼",
      "subtitle": "目標、時程、責任分工"
    },
    {
      "type": "bullets",
      "title": "本週重點",
      "body": "先把決策節點、交付物與風險窗口講清楚。",
      "bullets": [
        "確認專案範圍與成功指標",
        "鎖定第一階段里程碑與依賴項",
        "建立例會節奏與升級機制"
      ],
      "aside_title": "協作提醒",
      "aside_bullets": [
        "需求變更需同步 PM 與 Tech Lead",
        "跨部門阻塞超過兩天即升級"
      ]
    },
    {
      "type": "closing",
      "title": "謝謝",
      "subtitle": "接下來進入 Q&A 與分工確認"
    }
  ]
}
```
