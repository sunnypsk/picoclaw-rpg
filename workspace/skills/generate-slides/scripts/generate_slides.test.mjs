import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { __test__ } from "./generate_slides.mjs";

const {
  REPO_ROOT,
  WORKSPACE_ROOT,
  buildPresentation,
  buildDensityWarnings,
  defineDefaultMaster,
  normalizeSpec,
  renderBulletsSlide,
  renderClosingSlide,
  renderImageSlide,
  renderTitleSlide,
  resolveSafeOutputPath
} = __test__;

const WIDE_METRICS = { width: 13.333, height: 7.5 };

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
  return {
    title: "Deck",
    subtitle: "Q2 2026",
    layout: "wide",
    theme: "classic",
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
}

function createSlide(type, overrides = {}) {
  return {
    type,
    notes: "",
    sources: [],
    ...overrides
  };
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

test("normalizeSpec defaults to the classic theme and default variants", async () => {
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

test("defineDefaultMaster applies distinct theme styling", () => {
  const classic = { defineSlideMaster(config) { this.master = config; } };
  const editorial = { defineSlideMaster(config) { this.master = config; } };
  const contrast = { defineSlideMaster(config) { this.master = config; } };

  defineDefaultMaster(classic, WIDE_METRICS, "classic");
  defineDefaultMaster(editorial, WIDE_METRICS, "editorial");
  defineDefaultMaster(contrast, WIDE_METRICS, "contrast");

  assert.equal(classic.master.background.color, "F7F4EE");
  assert.equal(editorial.master.background.color, "F4F7FB");
  assert.equal(contrast.master.background.color, "0E1520");
  assert.notEqual(classic.master.slideNumber.color, editorial.master.slideNumber.color);
  assert.notDeepEqual(editorial.master.objects, contrast.master.objects);
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
    createDeckSpec({ subtitle: "Q2 2026", theme: "classic", lang: "en" }),
    WIDE_METRICS
  );

  assert.deepEqual(slide.texts.map(call => call.text), ["Questions"]);
});

test("renderClosingSlide minimal variant removes the filled card treatment", () => {
  const cardSlide = createRecordingSlide();
  const minimalSlide = createRecordingSlide();
  const deckSpec = createDeckSpec({ theme: "contrast" });
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

test("buildPresentation writes decks for each supported theme", async t => {
  const pptxModule = await import("pptxgenjs");
  const PptxGenJS = pptxModule.default || pptxModule;
  const outputDir = path.join(WORKSPACE_ROOT, "generated-slides", `theme-smoke-${process.pid}-${Date.now()}`);

  await fs.mkdir(outputDir, { recursive: true });
  t.after(async () => {
    await fs.rm(outputDir, { force: true, recursive: true });
  });

  for (const theme of ["classic", "editorial", "contrast"]) {
    const outputPath = path.join(outputDir, `${theme}.pptx`);
    const presentation = buildPresentation(
      PptxGenJS,
      createDeckSpec({
        title: `Smoke ${theme}`,
        theme,
        outputPath,
        slides: [
          createSlide("title", {
            variant: theme === "classic" ? "hero-left" : "hero-center",
            title: `Smoke ${theme}`,
            subtitle: "Theme validation",
            kicker: "Theme",
            byline: ""
          }),
          createSlide("closing", {
            variant: theme === "contrast" ? "minimal" : "card",
            title: "Done",
            subtitle: "Validation complete"
          })
        ]
      })
    );

    await presentation.writeFile({ fileName: outputPath });

    const stats = await fs.stat(outputPath);
    assert.ok(stats.size > 0);
  }
});
