import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { __test__ } from "./generate_slides.mjs";

const { REPO_ROOT, WORKSPACE_ROOT, buildDensityWarnings, renderClosingSlide, resolveSafeOutputPath } = __test__;

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

test("renderClosingSlide omits the deck subtitle when the closing slide has none", () => {
  const calls = [];
  const slide = {
    addText(text, options) {
      calls.push({ text, options });
    }
  };

  renderClosingSlide(
    slide,
    { type: "closing", title: "Questions", subtitle: "" },
    { subtitle: "Q2 2026", lang: "en" },
    { width: 13.333, height: 7.5 }
  );

  assert.deepEqual(calls.map(call => call.text), ["Questions"]);
});

test("buildDensityWarnings ignores the deck subtitle for closing slides", () => {
  const warnings = buildDensityWarnings({
    title: "Deck",
    subtitle: "Q".repeat(140),
    slides: [{ type: "closing", title: "Questions", subtitle: "" }]
  });

  assert.equal(warnings.some(warning => warning.includes("closing subtitle")), false);
});
