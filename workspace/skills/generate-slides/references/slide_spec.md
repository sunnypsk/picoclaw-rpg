# Slide Spec

Use this reference when building the JSON input for
`scripts/generate_slides.mjs`.

## Top-level fields

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
  "notes": "Open with the topline story, then pause for questions after the risks slide.",
  "sources": [
    "Finance dashboard export - 2026-07-01",
    "Product analytics weekly summary - 2026-06-30"
  ],
  "slides": [
    {
      "type": "title",
      "title": "Quarterly Product Update",
      "subtitle": "Q2 2026"
    }
  ]
}
```

- `title`: required string
- `subtitle`: optional string
- `filename`: optional output filename stem; `.pptx` is added automatically
- `layout`: optional `"wide"` or `"standard"`; defaults to `"wide"`
- `theme`: optional `"classic"`, `"editorial"`, or `"contrast"`; defaults to `"classic"`
- `lang`: optional language tag such as `"en-US"` or `"zh-TW"`; applied to slide text
- `author`, `company`, `subject`: optional metadata strings
- `notes`: optional deck-level speaker notes
- `sources`: optional deck-level source strings
- `slides`: required array with one or more supported slide objects

Deck-level `notes` and `sources` are merged into every slide's speaker notes. Slide-level `notes` and `sources` are appended after deck-level content.

## Supported slide types

All slide types below also accept these optional fields:
- `notes`: slide-level speaker notes
- `sources`: slide-level source strings
- `variant`: optional layout variant for that slide type; defaults are listed below

### `title`

```json
{
  "type": "title",
  "variant": "hero-center",
  "title": "Quarterly Product Update",
  "subtitle": "Q2 2026",
  "kicker": "Board Review",
  "byline": "Prepared by Product and Finance",
  "notes": "Use this slide to set context and expected outcomes.",
  "sources": [
    "Board packet draft - 2026-06-28"
  ]
}
```

- `title`: required
- `variant`: optional `"hero-left"` or `"hero-center"`; defaults to `"hero-left"`
- `subtitle`, `kicker`, `byline`: optional

### `section`

```json
{
  "type": "section",
  "variant": "statement",
  "title": "Wins and Risks",
  "subtitle": "What changed this quarter",
  "notes": "Pause here before moving into the next detail slide."
}
```

- `title`: required
- `variant`: optional `"divider"` or `"statement"`; defaults to `"divider"`
- `subtitle`: optional

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
  ],
  "notes": "Spend more time on the enterprise pipeline if the audience is sales-heavy.",
  "sources": [
    "Revenue workbook v3",
    "Hiring tracker - June"
  ]
}
```

- `title`: required
- `variant`: optional `"content-aside"` or `"two-column"`; defaults to `"content-aside"`
- `bullets`: required non-empty array of strings
- `body`, `aside_title`, `aside_body`: optional strings
- `aside_bullets`: optional array of strings

Use `variant: "two-column"` only when `aside_title`, `aside_body`, and `aside_bullets` are all omitted.

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
  ],
  "notes": "Call out the new navigation affordance in the top right.",
  "sources": [
    "Prototype v17 screenshot"
  ]
}
```

- `title`: required
- `variant`: optional `"image-left"` or `"image-right"`; defaults to `"image-left"`
- `image_path`: required local file path
- `image_fit`: optional `"cover"` or `"contain"`; defaults to `"cover"`
- `caption`: optional string
- `bullets`: optional array of strings

Use `image_fit: "contain"` for screenshots, charts, and UI mockups that should not be cropped.

### `closing`

```json
{
  "type": "closing",
  "variant": "minimal",
  "title": "Thank You",
  "subtitle": "Questions and discussion",
  "notes": "Hold here for open discussion."
}
```

- `title`: required
- `variant`: optional `"card"` or `"minimal"`; defaults to `"card"`
- `subtitle`: optional

## Full English example

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
      "subtitle": "What moved, what stalled, what changed",
      "notes": "Set up the next two slides as proof points."
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
      ],
      "sources": [
        "Q2 revenue workbook",
        "Sales pipeline snapshot - 2026-06-29"
      ]
    },
    {
      "type": "image",
      "variant": "image-right",
      "title": "Prototype Snapshot",
      "image_path": "workspace/assets/prototype.png",
      "image_fit": "contain",
      "caption": "The checkout redesign tested best with repeat buyers.",
      "bullets": [
        "The CTA moved above the fold",
        "Price transparency improved trust"
      ],
      "notes": "Contain mode keeps the screenshot readable without cropping."
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

## Traditional Chinese example

```json
{
  "title": "產品季度更新",
  "subtitle": "2026 年第二季",
  "filename": "產品季度更新",
  "layout": "wide",
  "lang": "zh-TW",
  "author": "Picoclaw",
  "notes": "先講結論，再進入風險與下季需求。",
  "sources": [
    "營運儀表板匯出 - 2026-07-01",
    "產品分析週報 - 2026-06-30"
  ],
  "slides": [
    {
      "type": "title",
      "title": "產品季度更新",
      "subtitle": "2026 年第二季",
      "kicker": "董事會簡報",
      "byline": "產品與營運團隊"
    },
    {
      "type": "bullets",
      "title": "本季重點",
      "body": "本季成長主要來自新手引導優化與企業客戶需求增加。",
      "bullets": [
        "營收年增 22%",
        "新手引導調整後啟動率提升",
        "企業管道數量翻倍"
      ],
      "aside_title": "注意事項",
      "aside_bullets": [
        "招募進度仍落後計畫",
        "續約時程仍有波動"
      ],
      "notes": "若觀眾偏商務團隊，可多講企業客戶管道的來源。"
    },
    {
      "type": "closing",
      "title": "謝謝",
      "subtitle": "歡迎提問"
    }
  ]
}
```
