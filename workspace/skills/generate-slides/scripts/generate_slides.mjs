#!/usr/bin/env node

import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const SKILL_DIR = path.resolve(SCRIPT_DIR, "..");
const WORKSPACE_ROOT = path.resolve(SKILL_DIR, "..", "..");
const REPO_ROOT = path.resolve(WORKSPACE_ROOT, "..");
const GENERATED_SLIDES_DIR = path.join(WORKSPACE_ROOT, "generated-slides");

const MASTER_NAME = "PICOCLAW_DEFAULT";
const FONTS = {
  title: "Aptos Display",
  body: "Aptos"
};
const PALETTE = {
  background: "F7F4EE",
  surface: "FFFDF8",
  text: "201A16",
  muted: "6C635C",
  accent: "B7642A",
  accentDark: "7F3C16",
  border: "D9C7B8"
};
const LAYOUTS = {
  wide: { name: "LAYOUT_WIDE", width: 13.333, height: 7.5 },
  standard: { name: "LAYOUT_STANDARD", width: 10.0, height: 7.5 }
};
const TEXT_ROLES = {
  kicker: {
    wide: 11,
    standard: 10,
    min: 9,
    compactAt: 24,
    denseAt: 40,
    compactStep: 1,
    denseStep: 1,
    fontFace: FONTS.body,
    margin: [2, 4, 2, 4],
    lineSpacingMultiple: 1.0,
    valign: "middle"
  },
  heroTitle: {
    wide: 30,
    standard: 26,
    min: 20,
    compactAt: 54,
    denseAt: 96,
    compactStep: 2,
    denseStep: 4,
    fontFace: FONTS.title,
    margin: 0,
    lineSpacingMultiple: 0.95,
    valign: "top"
  },
  heroSubtitle: {
    wide: 18,
    standard: 16,
    min: 11,
    compactAt: 80,
    denseAt: 150,
    compactStep: 2,
    denseStep: 3,
    fontFace: FONTS.body,
    margin: 0,
    lineSpacingMultiple: 1.05,
    valign: "top"
  },
  byline: {
    wide: 11,
    standard: 10,
    min: 9,
    compactAt: 70,
    denseAt: 110,
    compactStep: 1,
    denseStep: 2,
    fontFace: FONTS.body,
    margin: 0,
    lineSpacingMultiple: 1.0,
    valign: "top"
  },
  sectionLabel: {
    wide: 10,
    standard: 9,
    min: 8,
    compactAt: 18,
    denseAt: 30,
    compactStep: 1,
    denseStep: 1,
    fontFace: FONTS.body,
    margin: 0,
    lineSpacingMultiple: 1.0,
    valign: "middle"
  },
  sectionTitle: {
    wide: 32,
    standard: 28,
    min: 20,
    compactAt: 44,
    denseAt: 88,
    compactStep: 3,
    denseStep: 5,
    fontFace: FONTS.title,
    margin: 0,
    lineSpacingMultiple: 0.95,
    valign: "top"
  },
  slideTitle: {
    wide: 24,
    standard: 21,
    min: 16,
    compactAt: 52,
    denseAt: 94,
    compactStep: 2,
    denseStep: 4,
    fontFace: FONTS.title,
    margin: 0,
    lineSpacingMultiple: 0.95,
    valign: "top"
  },
  subtitle: {
    wide: 18,
    standard: 16,
    min: 11,
    compactAt: 80,
    denseAt: 140,
    compactStep: 2,
    denseStep: 3,
    fontFace: FONTS.body,
    margin: 0,
    lineSpacingMultiple: 1.05,
    valign: "top"
  },
  body: {
    wide: 15,
    standard: 14,
    min: 10,
    compactAt: 120,
    denseAt: 220,
    compactStep: 1,
    denseStep: 2,
    fontFace: FONTS.body,
    margin: [1, 2, 1, 2],
    lineSpacingMultiple: 1.1,
    valign: "top"
  },
  bullets: {
    wide: 20,
    standard: 17,
    min: 12,
    compactAt: 220,
    denseAt: 420,
    compactStep: 2,
    denseStep: 4,
    fontFace: FONTS.body,
    margin: [1, 2, 1, 2],
    lineSpacingMultiple: 1.08,
    valign: "top"
  },
  asideTitle: {
    wide: 9,
    standard: 8,
    min: 8,
    compactAt: 18,
    denseAt: 28,
    compactStep: 1,
    denseStep: 1,
    fontFace: FONTS.body,
    margin: 0,
    lineSpacingMultiple: 1.0,
    valign: "middle"
  },
  asideBody: {
    wide: 13,
    standard: 12,
    min: 9,
    compactAt: 100,
    denseAt: 180,
    compactStep: 1,
    denseStep: 2,
    fontFace: FONTS.body,
    margin: [1, 2, 1, 2],
    lineSpacingMultiple: 1.08,
    valign: "top"
  },
  closingTitle: {
    wide: 26,
    standard: 22,
    min: 18,
    compactAt: 48,
    denseAt: 84,
    compactStep: 2,
    denseStep: 4,
    fontFace: FONTS.title,
    margin: [2, 4, 2, 4],
    lineSpacingMultiple: 0.95,
    valign: "middle"
  },
  closingSubtitle: {
    wide: 16,
    standard: 15,
    min: 11,
    compactAt: 70,
    denseAt: 130,
    compactStep: 2,
    denseStep: 3,
    fontFace: FONTS.body,
    margin: 0,
    lineSpacingMultiple: 1.05,
    valign: "middle"
  }
};

main().catch(handleFatalError);

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const rawSpec = await loadSpec(args);
  const spec = await normalizeSpec(rawSpec, args.output);
  const PptxGenJS = await loadPptxGenJS();
  const presentation = buildPresentation(PptxGenJS, spec);

  await fs.mkdir(path.dirname(spec.outputPath), { recursive: true });
  await presentation.writeFile({ fileName: spec.outputPath });

  const result = {
    ok: true,
    title: spec.title,
    slide_count: spec.slides.length,
    output_path: spec.outputPath,
    warnings: spec.warnings
  };

  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

function parseArgs(argv) {
  const args = {
    specFile: null,
    output: null
  };

  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];

    if (token === "--help" || token === "-h") {
      printHelp();
      process.exit(0);
    }

    if (token === "--spec-file") {
      args.specFile = requireValue(argv, index, token);
      index += 1;
      continue;
    }

    if (token === "--output") {
      args.output = requireValue(argv, index, token);
      index += 1;
      continue;
    }

    throw new Error(`unknown argument: ${token}`);
  }

  return args;
}

function requireValue(argv, index, flagName) {
  const value = argv[index + 1];
  if (!value || value.startsWith("--")) {
    throw new Error(`missing value for ${flagName}`);
  }
  return value;
}

function printHelp() {
  process.stdout.write(
    [
      "Generate a PowerPoint deck from a JSON slide spec.",
      "",
      "Usage:",
      "  node scripts/generate_slides.mjs --spec-file spec.json [--output workspace/generated-slides/deck.pptx]",
      "  node scripts/generate_slides.mjs --output workspace/generated-slides/deck.pptx < spec.json",
      ""
    ].join("\n")
  );
}

async function loadSpec(args) {
  let raw = "";

  if (args.specFile) {
    raw = await fs.readFile(resolveAgainstRepo(args.specFile), "utf8");
  } else {
    raw = await readStdin();
  }

  raw = stripBom(raw);

  if (!raw.trim()) {
    throw new Error("no slide spec provided; pass --spec-file or pipe JSON via stdin");
  }

  try {
    return JSON.parse(raw);
  } catch (error) {
    throw new Error(`invalid JSON slide spec: ${error.message}`);
  }
}

function readStdin() {
  return new Promise((resolve, reject) => {
    let buffer = "";

    process.stdin.setEncoding("utf8");
    process.stdin.on("data", chunk => {
      buffer += chunk;
    });
    process.stdin.on("end", () => resolve(buffer));
    process.stdin.on("error", reject);

    if (process.stdin.isTTY) {
      resolve("");
    }
  });
}

function stripBom(value) {
  return String(value || "").replace(/^\uFEFF/, "");
}

async function normalizeSpec(rawSpec, outputOverride) {
  if (!rawSpec || typeof rawSpec !== "object" || Array.isArray(rawSpec)) {
    throw new Error("slide spec must be a JSON object");
  }

  const layoutKey = normalizeLayout(rawSpec.layout);
  const title = normalizeRequiredString(rawSpec.title, "title");
  const subtitle = normalizeOptionalString(rawSpec.subtitle, "subtitle");
  const filename = normalizeOptionalString(rawSpec.filename, "filename");
  const lang = normalizeLang(rawSpec.lang, "lang");
  const notes = normalizeOptionalNotes(rawSpec.notes, "notes");
  const sources = normalizeOptionalSources(rawSpec.sources, "sources");
  const slides = await Promise.all(
    normalizeArray(rawSpec.slides, "slides").map((slide, index) => normalizeSlide(slide, index))
  );

  if (slides.length === 0) {
    throw new Error("slides must contain at least one item");
  }

  const { outputPath, fallbackUsed } = resolveSafeOutputPath(outputOverride, filename, title);
  const warnings = buildDensityWarnings({
    title,
    subtitle,
    slides
  });

  if (fallbackUsed) {
    warnings.push("output filename stem could not be derived from title or filename; used timestamp fallback");
  }

  return {
    title,
    subtitle,
    filename,
    layout: layoutKey,
    outputPath,
    author: normalizeOptionalString(rawSpec.author, "author"),
    company: normalizeOptionalString(rawSpec.company, "company"),
    subject: normalizeOptionalString(rawSpec.subject, "subject"),
    lang,
    notes,
    sources,
    slides,
    warnings
  };
}

async function normalizeSlide(rawSlide, index) {
  if (!rawSlide || typeof rawSlide !== "object" || Array.isArray(rawSlide)) {
    throw new Error(`slides[${index}] must be an object`);
  }

  const type = normalizeRequiredString(rawSlide.type, `slides[${index}].type`);
  const common = {
    notes: normalizeOptionalNotes(rawSlide.notes, `slides[${index}].notes`),
    sources: normalizeOptionalSources(rawSlide.sources, `slides[${index}].sources`)
  };

  switch (type) {
    case "title":
      return {
        ...common,
        type,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        subtitle: normalizeOptionalString(rawSlide.subtitle, `slides[${index}].subtitle`),
        kicker: normalizeOptionalString(rawSlide.kicker, `slides[${index}].kicker`),
        byline: normalizeOptionalString(rawSlide.byline, `slides[${index}].byline`)
      };
    case "section":
      return {
        ...common,
        type,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        subtitle: normalizeOptionalString(rawSlide.subtitle, `slides[${index}].subtitle`)
      };
    case "bullets":
      return {
        ...common,
        type,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        body: normalizeOptionalString(rawSlide.body, `slides[${index}].body`),
        bullets: normalizeStringList(rawSlide.bullets, `slides[${index}].bullets`),
        asideTitle: normalizeOptionalString(rawSlide.aside_title, `slides[${index}].aside_title`),
        asideBody: normalizeOptionalString(rawSlide.aside_body, `slides[${index}].aside_body`),
        asideBullets: normalizeOptionalStringList(rawSlide.aside_bullets, `slides[${index}].aside_bullets`)
      };
    case "image": {
      const imagePath = normalizeRequiredString(rawSlide.image_path, `slides[${index}].image_path`);
      const resolvedImagePath = resolveAgainstRepo(imagePath);
      await assertReadableFile(resolvedImagePath, `slides[${index}].image_path`);
      return {
        ...common,
        type,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        imagePath: resolvedImagePath,
        imageFit: normalizeImageFit(rawSlide.image_fit, `slides[${index}].image_fit`),
        caption: normalizeOptionalString(rawSlide.caption, `slides[${index}].caption`),
        bullets: normalizeOptionalStringList(rawSlide.bullets, `slides[${index}].bullets`)
      };
    }
    case "closing":
      return {
        ...common,
        type,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        subtitle: normalizeOptionalString(rawSlide.subtitle, `slides[${index}].subtitle`)
      };
    default:
      throw new Error(`slides[${index}].type must be one of: title, section, bullets, image, closing`);
  }
}

function normalizeLayout(value) {
  if (value == null || value === "") {
    return "wide";
  }

  const text = normalizeRequiredString(value, "layout").toLowerCase();
  if (!LAYOUTS[text]) {
    throw new Error('layout must be "wide" or "standard"');
  }

  return text;
}

function normalizeRequiredString(value, fieldName) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${fieldName} must be a non-empty string`);
  }

  return value.trim();
}

function normalizeOptionalString(value, fieldName) {
  if (value == null) {
    return "";
  }

  if (typeof value !== "string") {
    throw new Error(`${fieldName} must be a string when provided`);
  }

  return value.trim();
}

function normalizeOptionalNotes(value, fieldName) {
  if (value == null) {
    return "";
  }

  if (typeof value !== "string") {
    throw new Error(`${fieldName} must be a string when provided`);
  }

  return value.replace(/\r\n/g, "\n").trim();
}

function normalizeOptionalSources(value, fieldName) {
  if (value == null) {
    return [];
  }

  if (!Array.isArray(value)) {
    throw new Error(`${fieldName} must be an array of strings`);
  }

  return dedupeStrings(
    value.map((item, index) => normalizeRequiredString(item, `${fieldName}[${index}]`))
  );
}

function normalizeLang(value, fieldName) {
  return normalizeOptionalString(value, fieldName);
}

function normalizeImageFit(value, fieldName) {
  if (value == null || value === "") {
    return "cover";
  }

  const fit = normalizeRequiredString(value, fieldName).toLowerCase();
  if (fit !== "cover" && fit !== "contain") {
    throw new Error(`${fieldName} must be "cover" or "contain"`);
  }

  return fit;
}

function normalizeArray(value, fieldName) {
  if (!Array.isArray(value)) {
    throw new Error(`${fieldName} must be an array`);
  }

  return value;
}

function normalizeStringList(value, fieldName) {
  const items = normalizeOptionalStringList(value, fieldName);
  if (items.length === 0) {
    throw new Error(`${fieldName} must contain at least one string item`);
  }

  return items;
}

function normalizeOptionalStringList(value, fieldName) {
  if (value == null) {
    return [];
  }

  if (!Array.isArray(value)) {
    throw new Error(`${fieldName} must be an array of strings`);
  }

  return value.map((item, index) => normalizeRequiredString(item, `${fieldName}[${index}]`));
}

async function assertReadableFile(filePath, fieldName) {
  try {
    await fs.access(filePath);
  } catch {
    throw new Error(`${fieldName} does not exist or is not readable: ${filePath}`);
  }
}

function resolveSafeOutputPath(outputOverride, filename, title) {
  if (!outputOverride) {
    const { stem, fallbackUsed } = buildDefaultStem(filename || title);
    return {
      outputPath: path.join(GENERATED_SLIDES_DIR, `${stem}.pptx`),
      fallbackUsed
    };
  }

  const candidate = ensurePptxExtension(resolveAgainstRepo(outputOverride));
  if (!isWithinDirectory(candidate, WORKSPACE_ROOT)) {
    throw new Error(`output path must stay within the workspace root: ${candidate}`);
  }

  return {
    outputPath: candidate,
    fallbackUsed: false
  };
}

function resolveAgainstRepo(inputPath) {
  if (path.isAbsolute(inputPath)) {
    return path.resolve(inputPath);
  }

  return path.resolve(REPO_ROOT, inputPath);
}

function ensurePptxExtension(filePath) {
  return filePath.toLowerCase().endsWith(".pptx") ? filePath : `${filePath}.pptx`;
}

function isWithinDirectory(candidatePath, rootPath) {
  const relative = path.relative(rootPath, candidatePath);
  return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
}

function buildDefaultStem(value) {
  const slug = slugifyUnicode(value);
  if (slug) {
    return { stem: slug, fallbackUsed: false };
  }

  return {
    stem: `presentation-${formatTimestamp(new Date())}`,
    fallbackUsed: true
  };
}

function slugifyUnicode(value) {
  const raw = String(value || "")
    .normalize("NFKC")
    .trim()
    .replace(/\.[^.]+$/, "");

  const cleaned = raw
    .replace(/[\/\\?%*:|"<>]/g, " ")
    .replace(/[\u0000-\u001f]/g, " ")
    .replace(/\s+/g, "-")
    .replace(/[^\p{Letter}\p{Number}\-._]+/gu, "")
    .replace(/[-._]{2,}/g, "-")
    .replace(/^[-._]+|[-._]+$/g, "")
    .slice(0, 80);

  if (!cleaned) {
    return "";
  }

  if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(cleaned)) {
    return `deck-${cleaned}`;
  }

  return cleaned;
}

function formatTimestamp(date) {
  const year = date.getFullYear();
  const month = pad(date.getMonth() + 1);
  const day = pad(date.getDate());
  const hours = pad(date.getHours());
  const minutes = pad(date.getMinutes());
  const seconds = pad(date.getSeconds());
  const milliseconds = String(date.getMilliseconds()).padStart(3, "0");
  return `${year}${month}${day}-${hours}${minutes}${seconds}-${milliseconds}`;
}

function pad(value) {
  return String(value).padStart(2, "0");
}

function dedupeStrings(items) {
  const seen = new Set();
  const result = [];

  for (const item of items) {
    if (!item || seen.has(item)) {
      continue;
    }

    seen.add(item);
    result.push(item);
  }

  return result;
}

function buildDensityWarnings(spec) {
  const warnings = [];

  if (!spec.slides.some(slide => slide.type === "title")) {
    warnings.push("deck does not contain a title slide");
  }

  if (spec.slides.length > 15) {
    warnings.push("deck has more than 15 slides; check readability before sharing");
  }

  spec.slides.forEach((slide, index) => {
    const prefix = `slide ${index + 1}`;

    if (slide.type === "title") {
      const effectiveSubtitle = slide.subtitle || spec.subtitle;
      if (slide.title.length > 90) {
        warnings.push(`${prefix}: title is very long and may overflow`);
      }
      if (effectiveSubtitle.length > 140) {
        warnings.push(`${prefix}: subtitle is very long and may overflow`);
      }
      if (slide.byline.length > 100) {
        warnings.push(`${prefix}: byline is long and may crowd the title slide`);
      }
      return;
    }

    if (slide.type === "section") {
      if (slide.title.length > 80) {
        warnings.push(`${prefix}: section title is very long and may overflow`);
      }
      if (slide.subtitle.length > 120) {
        warnings.push(`${prefix}: section subtitle is very long and may overflow`);
      }
      return;
    }

    if (slide.type === "bullets") {
      if (slide.title.length > 80) {
        warnings.push(`${prefix}: title is very long and may overflow`);
      }
      if (slide.bullets.length > 6) {
        warnings.push(`${prefix}: main bullet list is dense with more than 6 bullets`);
      }
      if (slide.bullets.some(item => item.length > 120)) {
        warnings.push(`${prefix}: at least one main bullet is very long`);
      }
      if (slide.body.length > 180 || slide.body.length + totalChars(slide.bullets) > 450) {
        warnings.push(`${prefix}: main content is dense and may need splitting`);
      }
      if (slide.asideBullets.length > 4) {
        warnings.push(`${prefix}: aside panel has more than 4 bullets`);
      }
      if (slide.asideBullets.some(item => item.length > 100)) {
        warnings.push(`${prefix}: aside panel includes a very long bullet`);
      }
      if (slide.asideBody.length + totalChars(slide.asideBullets) > 220) {
        warnings.push(`${prefix}: aside panel is crowded`);
      }
      return;
    }

    if (slide.type === "image") {
      if (slide.title.length > 80) {
        warnings.push(`${prefix}: title is very long and may overflow`);
      }
      if (slide.caption.length > 180) {
        warnings.push(`${prefix}: caption is very long and may crowd the image panel`);
      }
      if (slide.bullets.length > 4) {
        warnings.push(`${prefix}: image slide has more than 4 bullets`);
      }
      if (slide.bullets.some(item => item.length > 100)) {
        warnings.push(`${prefix}: image slide includes a very long bullet`);
      }
      if (slide.caption.length + totalChars(slide.bullets) > 300) {
        warnings.push(`${prefix}: image slide text is dense`);
      }
      return;
    }

    if (slide.type === "closing") {
      const effectiveSubtitle = slide.subtitle || spec.subtitle;
      if (slide.title.length > 80) {
        warnings.push(`${prefix}: closing title is very long and may overflow`);
      }
      if (effectiveSubtitle.length > 120) {
        warnings.push(`${prefix}: closing subtitle is very long and may overflow`);
      }
    }
  });

  return warnings;
}

function totalChars(items) {
  return items.reduce((sum, item) => sum + item.length, 0);
}

async function loadPptxGenJS() {
  try {
    const module = await import("pptxgenjs");
    return module.default || module;
  } catch (error) {
    throw new Error(
      `failed to load pptxgenjs. Run "npm ci --prefix workspace/skills/generate-slides" first. ${error.message}`
    );
  }
}

function buildPresentation(PptxGenJS, spec) {
  const pptx = new PptxGenJS();
  const metrics = LAYOUTS[spec.layout];

  pptx.layout = metrics.name;
  pptx.author = spec.author || "Picoclaw";
  pptx.company = spec.company || "";
  pptx.subject = spec.subject || "";
  pptx.title = spec.title;

  defineDefaultMaster(pptx, metrics);

  for (const slideSpec of spec.slides) {
    const slide = pptx.addSlide({ masterName: MASTER_NAME });
    renderSlide(slide, slideSpec, spec, metrics);

    const speakerNotes = buildSpeakerNotes(spec, slideSpec);
    if (speakerNotes) {
      slide.addNotes(speakerNotes);
    }
  }

  return pptx;
}

function buildSpeakerNotes(spec, slideSpec) {
  const blocks = [];
  const mergedSources = dedupeStrings([...spec.sources, ...slideSpec.sources]);

  if (spec.notes) {
    blocks.push(`[Deck Notes]\n${spec.notes}`);
  }

  if (slideSpec.notes) {
    blocks.push(`[Slide Notes]\n${slideSpec.notes}`);
  }

  if (mergedSources.length > 0) {
    blocks.push(`[Sources]\n${mergedSources.map(source => `- ${source}`).join("\n")}`);
  }

  return blocks.join("\n\n").trim();
}

function defineDefaultMaster(pptx, metrics) {
  pptx.defineSlideMaster({
    title: MASTER_NAME,
    background: { color: PALETTE.background },
    objects: [
      {
        rect: {
          x: 0,
          y: 0,
          w: metrics.width,
          h: 0.16,
          line: { color: PALETTE.accent, width: 1 },
          fill: { color: PALETTE.accent }
        }
      },
      {
        line: {
          x: 0.65,
          y: 6.92,
          w: metrics.width - 1.3,
          h: 0,
          line: { color: PALETTE.border, width: 1 }
        }
      }
    ],
    slideNumber: {
      x: metrics.width - 0.8,
      y: 7.0,
      w: 0.3,
      h: 0.2,
      fontFace: FONTS.body,
      fontSize: 9,
      color: PALETTE.muted,
      align: "right"
    }
  });
}

function renderSlide(slide, slideSpec, deckSpec, metrics) {
  switch (slideSpec.type) {
    case "title":
      renderTitleSlide(slide, slideSpec, deckSpec, metrics);
      break;
    case "section":
      renderSectionSlide(slide, slideSpec, deckSpec, metrics);
      break;
    case "bullets":
      renderBulletsSlide(slide, slideSpec, deckSpec, metrics);
      break;
    case "image":
      renderImageSlide(slide, slideSpec, deckSpec, metrics);
      break;
    case "closing":
      renderClosingSlide(slide, slideSpec, deckSpec, metrics);
      break;
    default:
      throw new Error(`unsupported slide type: ${slideSpec.type}`);
  }
}

function renderTitleSlide(slide, slideSpec, deckSpec, metrics) {
  const margin = 0.8;
  const contentWidth = metrics.width - margin * 2;
  const subtitle = slideSpec.subtitle || deckSpec.subtitle || "";

  addFittedText(slide, (slideSpec.kicker || "Presentation").toUpperCase(), {
    x: margin,
    y: 1.0,
    w: 2.45,
    h: 0.44
  }, "kicker", metrics, deckSpec.lang, {
    bold: true,
    color: "FFFFFF",
    align: "center",
    fill: { color: PALETTE.accentDark }
  });

  addFittedText(slide, slideSpec.title, {
    x: margin,
    y: 1.7,
    w: contentWidth,
    h: 1.45
  }, "heroTitle", metrics, deckSpec.lang, {
    bold: true,
    color: PALETTE.text
  });

  addFittedText(slide, subtitle, {
    x: margin,
    y: 3.25,
    w: contentWidth * 0.86,
    h: 1.1
  }, "heroSubtitle", metrics, deckSpec.lang, {
    color: PALETTE.muted
  });

  addFittedText(slide, slideSpec.byline, {
    x: margin,
    y: 5.95,
    w: contentWidth,
    h: 0.55
  }, "byline", metrics, deckSpec.lang, {
    color: PALETTE.muted
  });
}

function renderSectionSlide(slide, slideSpec, deckSpec, metrics) {
  const margin = 0.85;
  const contentWidth = metrics.width - margin * 2;

  addFittedText(slide, "SECTION", {
    x: margin,
    y: 1.0,
    w: 1.6,
    h: 0.28
  }, "sectionLabel", metrics, deckSpec.lang, {
    bold: true,
    color: PALETTE.accentDark
  });

  addFittedText(slide, slideSpec.title, {
    x: margin,
    y: 1.9,
    w: contentWidth,
    h: 1.55
  }, "sectionTitle", metrics, deckSpec.lang, {
    bold: true,
    color: PALETTE.text
  });

  addFittedText(slide, slideSpec.subtitle, {
    x: margin,
    y: 3.7,
    w: contentWidth * 0.82,
    h: 0.95
  }, "subtitle", metrics, deckSpec.lang, {
    color: PALETTE.muted
  });
}

function renderBulletsSlide(slide, slideSpec, deckSpec, metrics) {
  const margin = 0.75;
  const contentTop = 1.3;
  const contentHeight = 5.2;
  const gap = 0.35;
  const hasAside = slideSpec.asideTitle || slideSpec.asideBody || slideSpec.asideBullets.length > 0;
  const contentWidth = metrics.width - margin * 2;
  const rightWidth = hasAside ? (metrics.width > 11 ? 3.55 : 2.85) : 0;
  const leftWidth = hasAside ? contentWidth - gap - rightWidth : contentWidth;
  const bodyHeight = slideSpec.body ? 0.72 : 0;
  const bodyBottomGap = slideSpec.body ? 0.14 : 0;
  const bulletsY = contentTop + bodyHeight + bodyBottomGap;
  const bulletsHeight = contentHeight - (bulletsY - contentTop);

  addSlideTitle(slide, slideSpec.title, metrics, deckSpec.lang, margin);

  addFittedText(slide, slideSpec.body, {
    x: margin,
    y: contentTop,
    w: leftWidth,
    h: bodyHeight || 0.6
  }, "body", metrics, deckSpec.lang, {
    color: PALETTE.muted
  });

  addBulletList(slide, slideSpec.bullets, {
    x: margin,
    y: bulletsY,
    w: leftWidth,
    h: bulletsHeight
  }, "bullets", metrics, deckSpec.lang, {
    color: PALETTE.text,
    bulletIndent: 20
  });

  if (!hasAside) {
    return;
  }

  const asideX = margin + leftWidth + gap;
  const panelPaddingX = 0.16;
  let cursorY = contentTop + 0.14;

  addPanelFrame(slide, {
    x: asideX,
    y: contentTop,
    w: rightWidth,
    h: contentHeight
  });

  if (slideSpec.asideTitle) {
    addFittedText(slide, slideSpec.asideTitle.toUpperCase(), {
      x: asideX + panelPaddingX,
      y: cursorY,
      w: rightWidth - panelPaddingX * 2,
      h: 0.26
    }, "asideTitle", metrics, deckSpec.lang, {
      bold: true,
      color: PALETTE.accentDark
    });
    cursorY += 0.34;
  }

  if (slideSpec.asideBody) {
    const asideBodyHeight = slideSpec.asideBullets.length > 0 ? 0.82 : contentHeight - (cursorY - contentTop) - 0.18;
    addFittedText(slide, slideSpec.asideBody, {
      x: asideX + panelPaddingX,
      y: cursorY,
      w: rightWidth - panelPaddingX * 2,
      h: asideBodyHeight
    }, "asideBody", metrics, deckSpec.lang, {
      color: PALETTE.text
    });
    cursorY += asideBodyHeight + 0.08;
  }

  if (slideSpec.asideBullets.length > 0) {
    addBulletList(slide, slideSpec.asideBullets, {
      x: asideX + 0.1,
      y: cursorY,
      w: rightWidth - 0.2,
      h: Math.max(0.7, contentHeight - (cursorY - contentTop) - 0.12)
    }, "asideBody", metrics, deckSpec.lang, {
      color: PALETTE.text,
      bulletIndent: 14
    });
  }
}

function renderImageSlide(slide, slideSpec, deckSpec, metrics) {
  const margin = 0.75;
  const gap = 0.38;
  const contentTop = 1.3;
  const contentHeight = 5.2;
  const contentWidth = metrics.width - margin * 2;
  const imageWidth = metrics.width > 11 ? 7.45 : 5.55;
  const textWidth = contentWidth - gap - imageWidth;
  const panelX = margin + imageWidth + gap;
  const hasPanelContent = slideSpec.caption || slideSpec.bullets.length > 0;

  addSlideTitle(slide, slideSpec.title, metrics, deckSpec.lang, margin);

  slide.addImage({
    path: slideSpec.imagePath,
    x: margin,
    y: contentTop,
    sizing: {
      type: slideSpec.imageFit,
      w: imageWidth,
      h: contentHeight
    }
  });

  if (!hasPanelContent) {
    return;
  }

  addPanelFrame(slide, {
    x: panelX,
    y: contentTop,
    w: textWidth,
    h: contentHeight
  });

  let cursorY = contentTop + 0.18;
  if (slideSpec.caption) {
    const captionHeight = slideSpec.bullets.length > 0 ? 1.15 : contentHeight - 0.32;
    addFittedText(slide, slideSpec.caption, {
      x: panelX + 0.16,
      y: cursorY,
      w: textWidth - 0.32,
      h: captionHeight
    }, "body", metrics, deckSpec.lang, {
      color: PALETTE.text
    });
    cursorY += captionHeight + 0.08;
  }

  if (slideSpec.bullets.length > 0) {
    addBulletList(slide, slideSpec.bullets, {
      x: panelX + 0.1,
      y: cursorY,
      w: textWidth - 0.2,
      h: Math.max(0.7, contentHeight - (cursorY - contentTop) - 0.12)
    }, "body", metrics, deckSpec.lang, {
      color: PALETTE.text,
      bulletIndent: 16
    });
  }
}

function renderClosingSlide(slide, slideSpec, deckSpec, metrics) {
  const blockWidth = metrics.width > 11 ? 8.9 : 7.4;
  const blockX = (metrics.width - blockWidth) / 2;
  const subtitle = slideSpec.subtitle || deckSpec.subtitle || "";

  addFittedText(slide, slideSpec.title, {
    x: blockX,
    y: 2.1,
    w: blockWidth,
    h: 1.15
  }, "closingTitle", metrics, deckSpec.lang, {
    bold: true,
    color: "FFFFFF",
    align: "center",
    fill: { color: PALETTE.accentDark }
  });

  addFittedText(slide, subtitle, {
    x: blockX,
    y: 3.55,
    w: blockWidth,
    h: 0.9
  }, "closingSubtitle", metrics, deckSpec.lang, {
    color: PALETTE.muted,
    align: "center"
  });
}

function addSlideTitle(slide, title, metrics, lang, margin) {
  addFittedText(slide, title, {
    x: margin,
    y: 0.42,
    w: metrics.width - margin * 2,
    h: 0.65
  }, "slideTitle", metrics, lang, {
    bold: true,
    color: PALETTE.text
  });
}

function addPanelFrame(slide, box) {
  slide.addShape("rect", {
    ...box,
    fill: { color: PALETTE.surface },
    line: { color: PALETTE.border, width: 1 }
  });
}

function addFittedText(slide, text, box, roleName, metrics, lang, overrides = {}) {
  if (!text) {
    return;
  }

  slide.addText(text, {
    ...box,
    ...buildTextOptions(roleName, text, metrics, lang, overrides)
  });
}

function addBulletList(slide, items, box, roleName, metrics, lang, overrides = {}) {
  if (!items || items.length === 0) {
    return;
  }

  const { bulletIndent = 18, ...textOverrides } = overrides;
  const runs = items.map((item, index) => ({
    text: item,
    options: {
      bullet: { indent: bulletIndent },
      breakLine: index > 0
    }
  }));

  slide.addText(runs, {
    ...box,
    ...buildTextOptions(roleName, items.join(" "), metrics, lang, {
      densityHint: Math.max(0, items.length - 4),
      ...textOverrides
    })
  });
}

function buildTextOptions(roleName, text, metrics, lang, overrides = {}) {
  const role = TEXT_ROLES[roleName];
  const { densityHint = 0, ...rest } = overrides;
  const fontSize = rest.fontSize || pickFontSize(text, role, metrics, densityHint);

  return {
    fontFace: role.fontFace,
    fontSize,
    fit: "shrink",
    wrap: true,
    margin: role.margin,
    lineSpacingMultiple: role.lineSpacingMultiple,
    valign: role.valign,
    lang: lang || undefined,
    ...rest
  };
}

function pickFontSize(text, role, metrics, densityHint = 0) {
  const base = metrics.width > 11 ? role.wide : role.standard;
  const value = String(text || "");
  const newlinePenalty = Math.max(0, value.split(/\n+/).length - 1);
  let fontSize = base - densityHint - newlinePenalty;

  if (value.length > role.compactAt) {
    fontSize -= role.compactStep;
  }

  if (value.length > role.denseAt) {
    fontSize -= role.denseStep;
  }

  return Math.max(role.min, fontSize);
}

function handleFatalError(error) {
  const result = {
    ok: false,
    error: error instanceof Error ? error.message : String(error)
  };

  process.stderr.write(`${JSON.stringify(result, null, 2)}\n`);
  process.exit(1);
}
