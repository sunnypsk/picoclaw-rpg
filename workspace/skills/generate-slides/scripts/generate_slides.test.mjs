import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { __test__ } from "./generate_slides.mjs";

const {
  REPO_ROOT,
  WORKSPACE_ROOT,
  TEMPLATE_PRESETS,
  buildPresentation,
  buildDensityWarnings,
  defineDefaultMaster,
  getDefaultVariantForPreset,
  getPreset,
  normalizeSpec,
  renderBulletsSlide,
  renderClosingSlide,
  renderImageSlide,
  renderTitleSlide,
  resolveActivePreset,
  resolveSafeOutputPath
} = __test__;

const WIDE_METRICS = { width: 13.333, height: 7.5 };
const TINY_PNG_BASE64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/aRsAAAAASUVORK5CYII=";

function createRecordingSlide() {
  const texts = [];
  const shapes = [];
  const images = [];

  return {
    texts,
    shapes,
    images,
    addText(text, options) {
      texts.push({ text, options });
    },
    addShape(type, options) {
      shapes.push({ type, options });
    },
    addImage(options) {
      images.push(options);
    }
  };
}

function createDeckSpec(overrides = {}) {
  const deck = {
    title: "Deck",
    subtitle: "Q2 2026",
    layout: "wide",
    theme: "classic",
    templatePreset: "",
    activePreset: "classic",
    outputPath: path.join(WORKSPACE_ROOT, "generated-slides", "deck-test.pptx"),
    author: "Picoclaw",
    company: "",
    subject: "",
    lang: "en-US",
    notes: "",
    sources: [],
    slides: [],
    warnings: [],
    ...overrides
  };

  deck.activePreset = overrides.activePreset || deck.templatePreset || deck.theme || "classic";
  return deck;
}

function createSlide(type, overrides = {}) {
  return {
    type,
    notes: "",
    sources: [],
    ...overrides
  };
}

async function writeTinyPng(filePath) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  await fs.writeFile(filePath, Buffer.from(TINY_PNG_BASE64, "base64"));
  return filePath;
}

test("resolveSafeOutputPath rejects outputs that escape workspace through a junction", async t => {
  const suffix = `${process.pid}-${Date.now()}`;
  const linkName = `tmp-generate-slides-link-${suffix}`;
  const outsideDir = path.join(REPO_ROOT, `tmp-generate-slides-outside-${suffix}`);
  const linkPath = path.join(WORKSPACE_ROOT, linkName);

  await fs.mkdir(outsideDir, { recursive: true });
  await fs.symlink(outsideDir, linkPath, "junction");

  t.after(async () => {
    await fs.rm(linkPath, { force: true, recursive: true });
    await fs.rm(outsideDir, { force: true, recursive: true });
  });

  assert.throws(
    () => resolveSafeOutputPath(path.join("workspace", linkName, "deck.pptx"), "", "Deck"),
    /output path must stay within the workspace root/
  );
});

test("resolveSafeOutputPath keeps regular workspace outputs valid", () => {
  const { outputPath, fallbackUsed } = resolveSafeOutputPath("workspace/generated-slides/regression-deck", "", "Deck");

  assert.equal(fallbackUsed, false);
  assert.equal(outputPath, path.join(WORKSPACE_ROOT, "generated-slides", "regression-deck.pptx"));
});

test("normalizeSpec defaults to the classic preset family and legacy variants", async () => {
  const spec = await normalizeSpec({
    title: "Deck",
    slides: [
      { type: "title", title: "Deck" },
      { type: "section", title: "Story" },
      { type: "bullets", title: "Agenda", bullets: ["One", "Two"] },
      { type: "closing", title: "Questions" }
    ]
  });

  assert.equal(spec.theme, "classic");
  assert.equal(spec.templatePreset, "");
  assert.equal(spec.activePreset, "classic");
  assert.deepEqual(
    spec.slides.map(slide => slide.variant),
    ["hero-left", "divider", "content-aside", "card"]
  );
});

test("normalizeSpec accepts valid template_preset values", async () => {
  for (const presetName of Object.keys(TEMPLATE_PRESETS)) {
    const spec = await normalizeSpec({
      title: `Deck ${presetName}`,
      template_preset: presetName,
      slides: [{ type: "title", title: `Deck ${presetName}` }]
    });

    assert.equal(spec.templatePreset, presetName);
    assert.equal(spec.activePreset, presetName);
  }
});

test("normalizeSpec rejects unknown template_preset values", async () => {
  await assert.rejects(
    normalizeSpec({
      title: "Deck",
      template_preset: "retro-futurist",
      slides: [{ type: "title", title: "Deck" }]
    }),
    /template_preset must be one of:/
  );
});

test("template_preset takes precedence over theme", async () => {
  const spec = await normalizeSpec({
    title: "Deck",
    theme: "contrast",
    template_preset: "pitch-deck",
    slides: [
      { type: "title", title: "Deck" },
      { type: "bullets", title: "Story", bullets: ["One", "Two", "Three", "Four"] },
      { type: "closing", title: "Done" }
    ]
  });

  assert.equal(spec.theme, "contrast");
  assert.equal(spec.templatePreset, "pitch-deck");
  assert.equal(spec.activePreset, "pitch-deck");
  assert.deepEqual(spec.slides.map(slide => slide.variant), ["hero-center", "two-column", "minimal"]);
  assert.equal(resolveActivePreset({ theme: spec.theme, templatePreset: spec.templatePreset }), "pitch-deck");
});

test("legacy theme-only specs still normalize correctly", async () => {
  const spec = await normalizeSpec({
    title: "Deck",
    theme: "editorial",
    slides: [
      { type: "title", title: "Deck" },
      { type: "section", title: "Story" },
      { type: "bullets", title: "Agenda", bullets: ["One", "Two"] },
      { type: "closing", title: "Questions" }
    ]
  });

  assert.equal(spec.theme, "editorial");
  assert.equal(spec.templatePreset, "");
  assert.equal(spec.activePreset, "editorial");
  assert.deepEqual(
    spec.slides.map(slide => slide.variant),
    ["hero-left", "divider", "content-aside", "card"]
  );
});

test("normalizeSpec rejects unknown themes", async () => {
  await assert.rejects(
    normalizeSpec({
      title: "Deck",
      theme: "neon",
      slides: [{ type: "title", title: "Deck" }]
    }),
    /theme must be one of: classic, editorial, contrast/
  );
});

test("normalizeSpec rejects unknown variants", async () => {
  await assert.rejects(
    normalizeSpec({
      title: "Deck",
      slides: [{ type: "title", title: "Deck", variant: "mosaic" }]
    }),
    /slides\[0\]\.variant must be one of: hero-left, hero-center/
  );
});

test("normalizeSpec rejects two-column bullet slides with aside content", async () => {
  await assert.rejects(
    normalizeSpec({
      title: "Deck",
      slides: [
        {
          type: "bullets",
          title: "Agenda",
          variant: "two-column",
          bullets: ["One", "Two"],
          aside_title: "Watchouts"
        }
      ]
    }),
    /two-column/
  );
});

test("preset-aware default variants work when variant is omitted", async t => {
  const tempDir = path.join(WORKSPACE_ROOT, "generated-slides", `preset-defaults-${process.pid}-${Date.now()}`);
  const imagePath = path.join(tempDir, "tiny.png");
  await writeTinyPng(imagePath);

  t.after(async () => {
    await fs.rm(tempDir, { force: true, recursive: true });
  });

  const consultingSpec = await normalizeSpec({
    title: "Consulting",
    template_preset: "consulting-proposal",
    slides: [
      { type: "title", title: "Consulting" },
      { type: "section", title: "Summary" },
      { type: "bullets", title: "Agenda", bullets: ["One", "Two"], aside_title: "Focus" },
      { type: "image", title: "Snapshot", image_path: imagePath },
      { type: "closing", title: "Questions" }
    ]
  });

  const academicSpec = await normalizeSpec({
    title: "Academic",
    template_preset: "academic",
    slides: [
      { type: "title", title: "Academic" },
      { type: "section", title: "Outline" },
      { type: "bullets", title: "Findings", bullets: ["One", "Two"] },
      { type: "image", title: "Figure", image_path: imagePath },
      { type: "closing", title: "謝謝" }
    ]
  });

  const pitchDeckNoAside = await normalizeSpec({
    title: "Pitch",
    template_preset: "pitch-deck",
    slides: [
      { type: "bullets", title: "Story", bullets: ["One", "Two", "Three", "Four"] }
    ]
  });

  const pitchDeckWithAside = await normalizeSpec({
    title: "Pitch",
    template_preset: "pitch-deck",
    slides: [
      {
        type: "bullets",
        title: "Story",
        bullets: ["One", "Two", "Three", "Four"],
        aside_title: "Proof",
        aside_bullets: ["Retention is improving"]
      }
    ]
  });

  assert.deepEqual(
    consultingSpec.slides.map(slide => slide.variant),
    ["hero-left", "divider", "content-aside", "image-right", "card"]
  );
  assert.deepEqual(
    academicSpec.slides.map(slide => slide.variant),
    ["hero-left", "divider", "content-aside", "image-left", "minimal"]
  );
  assert.equal(pitchDeckNoAside.slides[0].variant, "two-column");
  assert.equal(pitchDeckWithAside.slides[0].variant, "content-aside");
  assert.equal(getDefaultVariantForPreset("pitch-deck", "closing"), "minimal");
});

test("explicit variants still override preset defaults", async t => {
  const tempDir = path.join(WORKSPACE_ROOT, "generated-slides", `preset-explicit-${process.pid}-${Date.now()}`);
  const imagePath = path.join(tempDir, "tiny.png");
  await writeTinyPng(imagePath);

  t.after(async () => {
    await fs.rm(tempDir, { force: true, recursive: true });
  });

  const spec = await normalizeSpec({
    title: "Pitch Override",
    template_preset: "pitch-deck",
    slides: [
      { type: "title", title: "Pitch Override", variant: "hero-left" },
      { type: "section", title: "Plan", variant: "divider" },
      { type: "bullets", title: "Story", variant: "content-aside", bullets: ["One", "Two"] },
      { type: "image", title: "Mockup", variant: "image-left", image_path: imagePath },
      { type: "closing", title: "Done", variant: "card" }
    ]
  });

  assert.deepEqual(
    spec.slides.map(slide => slide.variant),
    ["hero-left", "divider", "content-aside", "image-left", "card"]
  );
});

test("defineDefaultMaster applies distinct preset styling", () => {
  const classic = { defineSlideMaster(config) { this.master = config; } };
  const consulting = { defineSlideMaster(config) { this.master = config; } };
  const pitch = { defineSlideMaster(config) { this.master = config; } };

  defineDefaultMaster(classic, WIDE_METRICS, "classic");
  defineDefaultMaster(consulting, WIDE_METRICS, "consulting-proposal");
  defineDefaultMaster(pitch, WIDE_METRICS, "pitch-deck");

  assert.equal(classic.master.background.color, "F7F4EE");
  assert.equal(consulting.master.background.color, "F7F9FC");
  assert.equal(pitch.master.background.color, "FFF7F0");
  assert.notEqual(classic.master.slideNumber.color, consulting.master.slideNumber.color);
  assert.notDeepEqual(consulting.master.objects, pitch.master.objects);
  assert.equal(getPreset("consulting-proposal").fonts.title, "Aptos Display");
});

test("renderTitleSlide changes geometry for hero-center", () => {
  const heroLeftSlide = createRecordingSlide();
  const heroCenterSlide = createRecordingSlide();
  const deckSpec = createDeckSpec({ subtitle: "Q2 2026" });

  renderTitleSlide(
    heroLeftSlide,
    createSlide("title", {
      variant: "hero-left",
      title: "Deck",
      subtitle: "",
      kicker: "Board Review",
      byline: "Prepared by Product"
    }),
    deckSpec,
    WIDE_METRICS
  );
  renderTitleSlide(
    heroCenterSlide,
    createSlide("title", {
      variant: "hero-center",
      title: "Deck",
      subtitle: "",
      kicker: "Board Review",
      byline: "Prepared by Product"
    }),
    deckSpec,
    WIDE_METRICS
  );

  const leftTitle = heroLeftSlide.texts.find(entry => entry.text === "Deck");
  const centerTitle = heroCenterSlide.texts.find(entry => entry.text === "Deck");

  assert.equal(leftTitle.options.x, 0.8);
  assert.equal(centerTitle.options.align, "center");
  assert.notEqual(centerTitle.options.x, leftTitle.options.x);
});

test("renderBulletsSlide splits lists into two columns when requested", () => {
  const asideSlide = createRecordingSlide();
  const twoColumnSlide = createRecordingSlide();
  const deckSpec = createDeckSpec();
  const bullets = ["Revenue up 22%", "Activation improved", "Pipeline doubled", "Renewals stabilized"];

  renderBulletsSlide(
    asideSlide,
    createSlide("bullets", {
      variant: "content-aside",
      title: "Highlights",
      body: "",
      bullets,
      asideTitle: "Watchouts",
      asideBody: "",
      asideBullets: []
    }),
    deckSpec,
    WIDE_METRICS
  );
  renderBulletsSlide(
    twoColumnSlide,
    createSlide("bullets", {
      variant: "two-column",
      title: "Highlights",
      body: "",
      bullets,
      asideTitle: "",
      asideBody: "",
      asideBullets: []
    }),
    deckSpec,
    WIDE_METRICS
  );

  const asideBulletCalls = asideSlide.texts.filter(entry => Array.isArray(entry.text));
  const twoColumnBulletCalls = twoColumnSlide.texts.filter(entry => Array.isArray(entry.text));

  assert.equal(asideBulletCalls.length, 1);
  assert.equal(twoColumnBulletCalls.length, 2);
  assert.equal(asideSlide.shapes[0].options.w, 3.55);
  assert.equal(twoColumnSlide.shapes[0].options.w, 0.02);
  assert.notEqual(twoColumnBulletCalls[0].options.x, twoColumnBulletCalls[1].options.x);
});

test("renderImageSlide mirrors the image panel for image-right", () => {
  const leftSlide = createRecordingSlide();
  const rightSlide = createRecordingSlide();
  const deckSpec = createDeckSpec();
  const baseSlide = createSlide("image", {
    title: "Prototype",
    imagePath: "workspace/assets/prototype.png",
    imageFit: "contain",
    caption: "Updated checkout flow",
    bullets: ["One tap checkout"]
  });

  renderImageSlide(leftSlide, { ...baseSlide, variant: "image-left" }, deckSpec, WIDE_METRICS);
  renderImageSlide(rightSlide, { ...baseSlide, variant: "image-right" }, deckSpec, WIDE_METRICS);

  assert.equal(leftSlide.images[0].x, 0.75);
  assert.equal(rightSlide.shapes[0].options.x, 0.75);
  assert.ok(rightSlide.images[0].x > leftSlide.images[0].x);
});

test("renderClosingSlide omits the deck subtitle when the closing slide has none", () => {
  const slide = createRecordingSlide();

  renderClosingSlide(
    slide,
    createSlide("closing", { variant: "card", title: "Questions", subtitle: "" }),
    createDeckSpec({ subtitle: "Q2 2026", theme: "classic", activePreset: "classic", lang: "en" }),
    WIDE_METRICS
  );

  assert.deepEqual(slide.texts.map(call => call.text), ["Questions"]);
});

test("renderClosingSlide minimal variant removes the filled card treatment", () => {
  const cardSlide = createRecordingSlide();
  const minimalSlide = createRecordingSlide();
  const deckSpec = createDeckSpec({ theme: "contrast", activePreset: "contrast" });
  const cardSpec = createSlide("closing", { variant: "card", title: "Questions", subtitle: "Discuss next steps" });
  const minimalSpec = createSlide("closing", { variant: "minimal", title: "Questions", subtitle: "Discuss next steps" });

  renderClosingSlide(cardSlide, cardSpec, deckSpec, WIDE_METRICS);
  renderClosingSlide(minimalSlide, minimalSpec, deckSpec, WIDE_METRICS);

  const cardTitle = cardSlide.texts.find(entry => entry.text === "Questions");
  const minimalTitle = minimalSlide.texts.find(entry => entry.text === "Questions");

  assert.ok(cardTitle.options.fill);
  assert.equal(minimalTitle.options.fill, undefined);
  assert.equal(minimalSlide.shapes.length, 1);
});

test("buildDensityWarnings ignores the deck subtitle for closing slides", () => {
  const warnings = buildDensityWarnings({
    title: "Deck",
    subtitle: "Q".repeat(140),
    slides: [{ type: "closing", title: "Questions", subtitle: "" }]
  });

  assert.equal(warnings.some(warning => warning.includes("closing subtitle")), false);
});

test("buildPresentation writes decks for a subset of presets", async t => {
  const pptxModule = await import("pptxgenjs");
  const PptxGenJS = pptxModule.default || pptxModule;
  const outputDir = path.join(WORKSPACE_ROOT, "generated-slides", `preset-smoke-${process.pid}-${Date.now()}`);

  await fs.mkdir(outputDir, { recursive: true });
  t.after(async () => {
    await fs.rm(outputDir, { force: true, recursive: true });
  });

  const presetsToTest = [
    { name: "classic", raw: { theme: "classic" } },
    { name: "consulting-proposal", raw: { template_preset: "consulting-proposal" } },
    { name: "pitch-deck", raw: { template_preset: "pitch-deck" } },
    { name: "project-kickoff", raw: { template_preset: "project-kickoff" } }
  ];

  for (const entry of presetsToTest) {
    const outputPath = path.join(outputDir, `${entry.name}.pptx`);
    const spec = await normalizeSpec(
      {
        title: `Smoke ${entry.name}`,
        subtitle: "Preset validation",
        ...entry.raw,
        slides: [
          { type: "title", title: `Smoke ${entry.name}` },
          { type: "section", title: "Story", subtitle: "Preset defaults" },
          { type: "bullets", title: "Highlights", bullets: ["One", "Two", "Three", "Four"] },
          { type: "closing", title: "Done", subtitle: "Validation complete" }
        ]
      },
      outputPath
    );

    const presentation = buildPresentation(PptxGenJS, spec);
    await presentation.writeFile({ fileName: outputPath });

    const stats = await fs.stat(outputPath);
    assert.ok(stats.size > 0);
  }
});
