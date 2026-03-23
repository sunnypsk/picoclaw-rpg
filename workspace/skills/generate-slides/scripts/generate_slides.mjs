#!/usr/bin/env node

import fsSync from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const SCRIPT_PATH = fileURLToPath(import.meta.url);
const SCRIPT_DIR = path.dirname(SCRIPT_PATH);
const SKILL_DIR = path.resolve(SCRIPT_DIR, "..");
const WORKSPACE_ROOT = path.resolve(SKILL_DIR, "..", "..");
const REPO_ROOT = path.resolve(WORKSPACE_ROOT, "..");
const GENERATED_SLIDES_DIR = path.join(WORKSPACE_ROOT, "generated-slides");

const MASTER_NAME = "PICOCLAW_DEFAULT";
const DEFAULT_THEME = "classic";
const THEMES = {
  classic: {
    fonts: {
      title: "Aptos Display",
      body: "Aptos"
    },
    palette: {
      background: "F7F4EE",
      surface: "FFFDF8",
      text: "201A16",
      muted: "6C635C",
      accent: "B7642A",
      accentDark: "7F3C16",
      border: "D9C7B8"
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: metrics.width,
              h: 0.16,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            line: {
              x: 0.65,
              y: 6.92,
              w: metrics.width - 1.3,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 0.8,
          y: 7.0,
          w: 0.3,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.muted,
          align: "right"
        };
      }
    }
  },
  editorial: {
    fonts: {
      title: "Georgia",
      body: "Aptos"
    },
    palette: {
      background: "F4F7FB",
      surface: "FFFFFF",
      text: "1C2533",
      muted: "5E6B7A",
      accent: "5E7FA7",
      accentDark: "2E4E74",
      border: "D4DFEA"
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: metrics.width,
              h: 0.08,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            rect: {
              x: 0.58,
              y: 0.88,
              w: 0.12,
              h: 5.78,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            line: {
              x: 0.92,
              y: 6.92,
              w: metrics.width - 1.84,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(_metrics, fonts, palette) {
        return {
          x: 0.86,
          y: 7.0,
          w: 0.45,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accentDark,
          align: "left"
        };
      }
    }
  },
  contrast: {
    fonts: {
      title: "Bahnschrift",
      body: "Aptos"
    },
    palette: {
      background: "0E1520",
      surface: "172231",
      text: "F5F7FA",
      muted: "B5C0CE",
      accent: "4FD1FF",
      accentDark: "11779D",
      border: "314154"
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: metrics.width,
              h: 0.18,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            rect: {
              x: 0,
              y: metrics.height - 0.18,
              w: metrics.width,
              h: 0.18,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 0.95,
          y: 0.34,
          w: 0.5,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accent,
          align: "right"
        };
      }
    }
  }
};
const LAYOUTS = {
  wide: { name: "LAYOUT_WIDE", width: 13.333, height: 7.5 },
  standard: { name: "LAYOUT_STANDARD", width: 10.0, height: 7.5 }
};
const DEFAULT_VARIANTS = Object.freeze({
  title: "hero-left",
  section: "divider",
  bullets: "content-aside",
  image: "image-left",
  closing: "card"
});
const LOCALIZED_SECTION_LABELS = Object.freeze({
  de: "ABSCHNITT",
  es: "SECCI\u00d3N",
  fr: "SECTION",
  it: "SEZIONE",
  ja: "\u30bb\u30af\u30b7\u30e7\u30f3",
  ko: "\uc139\uc158",
  nl: "SECTIE",
  pt: "SE\u00c7\u00c3O",
  zh: "\u7ae0\u7bc0"
});
const SLIDE_VARIANTS = Object.freeze({
  title: ["hero-left", "hero-center"],
  section: ["divider", "statement"],
  bullets: ["content-aside", "two-column"],
  image: ["image-left", "image-right"],
  closing: ["card", "minimal"]
});
const TEXT_ROLES = {
  kicker: {
    wide: 11,
    standard: 10,
    min: 9,
    compactAt: 24,
    denseAt: 40,
    compactStep: 1,
    denseStep: 1,
    fontKey: "body",
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
    fontKey: "title",
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
    fontKey: "body",
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
    fontKey: "body",
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
    fontKey: "body",
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
    fontKey: "title",
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
    fontKey: "title",
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
    fontKey: "body",
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
    fontKey: "body",
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
    fontKey: "body",
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
    fontKey: "body",
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
    fontKey: "body",
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
    fontKey: "title",
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
    fontKey: "body",
    margin: 0,
    lineSpacingMultiple: 1.05,
    valign: "middle"
  }
};

const LEGACY_THEME_NAMES = Object.freeze(Object.keys(THEMES));
const BASE_LAYOUT_TOKENS = {
  content: {
    slideTitleMargin: 0.75,
    slideTitleY: 0.42,
    slideTitleHeight: 0.65,
    slideTitleAccentHeight: 0,
    slideTitleAccentWidthWide: 1.05,
    slideTitleAccentWidthStandard: 0.85,
    slideTitleAccentGap: 0.06,
    slideTitleAccentAlign: "left",
    slideTitleAccentInset: 0,
    slideTitleAccentColorKey: "accent"
  },
  panel: {
    fillColorKey: "surface",
    lineColorKey: "border",
    lineWidth: 1,
    accentBarHeight: 0,
    accentBarInset: 0,
    accentBarPosition: "top",
    accentBarColorKey: "accent",
    edgeBarWidth: 0,
    edgeBarInset: 0,
    edgeBarPosition: "left",
    edgeBarColorKey: "accentDark"
  },
  title: {
    heroLeft: {
      margin: 0.8,
      kickerY: 1.0,
      kickerWidthWide: 2.45,
      kickerWidthStandard: 2.2,
      kickerHeight: 0.44,
      kickerFillColorKey: "accentDark",
      kickerTextColor: "FFFFFF",
      titleY: 1.7,
      titleHeight: 1.45,
      titleColorKey: "text",
      subtitleY: 3.25,
      subtitleWidthFactor: 0.86,
      subtitleHeight: 1.1,
      subtitleColorKey: "muted",
      bylineY: 5.95,
      bylineHeight: 0.55,
      bylineColorKey: "muted"
    },
    heroCenter: {
      pillWidthWide: 2.65,
      pillWidthStandard: 2.3,
      blockWidthWide: 9.2,
      blockWidthStandard: 7.4,
      kickerY: 1.08,
      kickerHeight: 0.42,
      kickerFillColorKey: "accentDark",
      kickerTextColor: "FFFFFF",
      titleY: 1.82,
      titleHeight: 1.45,
      titleColorKey: "text",
      subtitleInset: 0.24,
      subtitleY: 3.34,
      subtitleHeight: 1.0,
      subtitleColorKey: "muted",
      bylineY: 5.92,
      bylineHeight: 0.5,
      bylineColorKey: "muted"
    }
  },
  section: {
    divider: {
      margin: 0.85,
      labelText: "SECTION",
      labelY: 1.0,
      labelWidth: 1.6,
      labelHeight: 0.28,
      labelColorKey: "accentDark",
      titleY: 1.9,
      titleHeight: 1.55,
      titleColorKey: "text",
      subtitleY: 3.7,
      subtitleWidthFactor: 0.82,
      subtitleHeight: 0.95,
      subtitleColorKey: "muted"
    },
    statement: {
      barWidthWide: 1.35,
      barWidthStandard: 1.1,
      barY: 1.55,
      barHeight: 0.06,
      barColorKey: "accent",
      blockWidthWide: 9.15,
      blockWidthStandard: 7.2,
      titleY: 2.0,
      titleHeight: 1.45,
      titleColorKey: "text",
      subtitleInset: 0.18,
      subtitleY: 3.74,
      subtitleHeight: 0.9,
      subtitleColorKey: "muted"
    }
  },
  bullets: {
    contentAside: {
      margin: 0.75,
      top: 1.3,
      height: 5.2,
      gap: 0.35,
      rightWidthWide: 3.55,
      rightWidthStandard: 2.85,
      bodyHeight: 0.72,
      bodyGap: 0.14,
      bodyColorKey: "muted",
      bulletsColorKey: "text",
      bulletsIndent: 20,
      panelPaddingX: 0.16,
      panelInsetY: 0.14,
      asideTitleHeight: 0.26,
      asideTitleGap: 0.34,
      asideTitleColorKey: "accentDark",
      asideBodyHeightWithBullets: 0.82,
      asideBodyColorKey: "text",
      panelBulletInsetX: 0.1,
      panelBulletIndent: 14,
      panelBottomPad: 0.12
    },
    twoColumn: {
      margin: 0.75,
      top: 1.3,
      height: 5.2,
      gap: 0.42,
      bodyHeight: 0.72,
      bodyGap: 0.18,
      bodyColorKey: "muted",
      bulletsColorKey: "text",
      bulletsIndent: 18,
      dividerWidth: 0.02,
      dividerInsetY: 0.08,
      dividerColorKey: "border"
    }
  },
  image: {
    margin: 0.75,
    gap: 0.38,
    top: 1.3,
    height: 5.2,
    imageWidthWide: 7.45,
    imageWidthStandard: 5.55,
    captionColorKey: "text",
    panelPaddingX: 0.16,
    panelTopInset: 0.18,
    captionHeightWithBullets: 1.15,
    bulletsColorKey: "text",
    panelBulletInsetX: 0.1,
    panelBulletIndent: 16,
    panelBottomPad: 0.12
  },
  closing: {
    card: {
      blockWidthWide: 8.9,
      blockWidthStandard: 7.4,
      titleY: 2.1,
      titleHeight: 1.15,
      titleFillColorKey: "accentDark",
      titleColor: "FFFFFF",
      subtitleY: 3.55,
      subtitleHeight: 0.9,
      subtitleColorKey: "muted"
    },
    minimal: {
      barWidthWide: 1.6,
      barWidthStandard: 1.35,
      barY: 2.18,
      barHeight: 0.06,
      barColorKey: "accent",
      blockWidthWide: 8.7,
      blockWidthStandard: 7.1,
      titleY: 2.55,
      titleHeight: 0.95,
      titleColorKey: "text",
      subtitleInset: 0.12,
      subtitleY: 3.7,
      subtitleHeight: 0.78,
      subtitleColorKey: "muted"
    }
  }
};
const TEMPLATE_PRESETS = Object.freeze({
  classic: createPreset({
    ...THEMES.classic
  }),
  editorial: createPreset({
    ...THEMES.editorial
  }),
  contrast: createPreset({
    ...THEMES.contrast
  }),
  academic: createPreset({
    fonts: {
      title: "Cambria",
      body: "Aptos"
    },
    palette: {
      background: "F6F5F1",
      surface: "FBFAF7",
      text: "1F2630",
      muted: "66727F",
      accent: "7A8C7F",
      accentDark: "4F6255",
      border: "CBD2CD"
    },
    defaultVariants: {
      closing: "minimal"
    },
    layoutTokens: {
      content: {
        slideTitleMargin: 0.68,
        slideTitleY: 0.48,
        slideTitleHeight: 0.58,
        slideTitleAccentHeight: 0.04,
        slideTitleAccentWidthWide: 0.92,
        slideTitleAccentWidthStandard: 0.78,
        slideTitleAccentGap: 0.08,
        slideTitleAccentColorKey: "accentDark"
      },
      title: {
        heroLeft: {
          margin: 0.72,
          kickerY: 0.92,
          kickerWidthWide: 2.2,
          kickerWidthStandard: 2.0,
          titleY: 1.5,
          titleHeight: 1.38,
          subtitleY: 3.0,
          subtitleWidthFactor: 0.78,
          subtitleHeight: 1.2,
          bylineY: 6.05
        }
      },
      section: {
        divider: {
          margin: 0.74,
          labelY: 0.95,
          labelWidth: 1.8,
          titleY: 1.75,
          titleHeight: 1.45,
          subtitleY: 3.55,
          subtitleWidthFactor: 0.78
        }
      },
      bullets: {
        contentAside: {
          margin: 0.68,
          top: 1.25,
          height: 5.35,
          gap: 0.28,
          rightWidthWide: 3.2,
          rightWidthStandard: 2.7,
          bodyHeight: 0.82,
          bodyGap: 0.12,
          panelPaddingX: 0.18,
          panelInsetY: 0.18,
          asideBodyHeightWithBullets: 1.05
        },
        twoColumn: {
          margin: 0.68,
          top: 1.25,
          height: 5.35,
          gap: 0.34,
          bodyHeight: 0.82,
          bodyGap: 0.14
        }
      },
      image: {
        margin: 0.68,
        gap: 0.28,
        top: 1.25,
        height: 5.35,
        imageWidthWide: 7.1,
        imageWidthStandard: 5.3,
        captionHeightWithBullets: 1.35
      },
      closing: {
        minimal: {
          barWidthWide: 1.1,
          barWidthStandard: 0.95,
          barY: 2.24,
          titleY: 2.48,
          titleHeight: 0.98,
          subtitleY: 3.62
        }
      }
    },
    textRoleOverrides: {
      heroTitle: { wide: 28, standard: 24, min: 18 },
      heroSubtitle: { wide: 17, standard: 15 },
      sectionTitle: { wide: 30, standard: 26 },
      slideTitle: { wide: 20, standard: 18 },
      body: { wide: 14, standard: 13 },
      bullets: { wide: 18, standard: 16 },
      asideBody: { wide: 12, standard: 11 },
      closingTitle: { wide: 24, standard: 21 }
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: metrics.width,
              h: 0.08,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            line: {
              x: 0.68,
              y: 1.0,
              w: metrics.width - 1.36,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          },
          {
            line: {
              x: 0.68,
              y: 6.92,
              w: metrics.width - 1.36,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 0.88,
          y: 7.0,
          w: 0.38,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.muted,
          align: "right"
        };
      }
    }
  }),
  "brand-design": createPreset({
    fonts: {
      title: "Bahnschrift",
      body: "Aptos"
    },
    palette: {
      background: "FFF7F2",
      surface: "FFFFFF",
      text: "231815",
      muted: "7C6558",
      accent: "F05A28",
      accentDark: "8C2E16",
      border: "F0C9BA"
    },
    defaultVariants: {
      title: "hero-center",
      section: "statement",
      image: "image-right",
      closing: "minimal"
    },
    layoutTokens: {
      content: {
        slideTitleMargin: 0.82,
        slideTitleY: 0.52,
        slideTitleAccentHeight: 0.07,
        slideTitleAccentWidthWide: 1.35,
        slideTitleAccentWidthStandard: 1.1,
        slideTitleAccentGap: 0.08
      },
      panel: {
        lineColorKey: "accent",
        lineWidth: 1.5,
        edgeBarWidth: 0.12,
        edgeBarPosition: "left",
        edgeBarColorKey: "accentDark"
      },
      title: {
        heroCenter: {
          pillWidthWide: 2.95,
          pillWidthStandard: 2.6,
          blockWidthWide: 9.6,
          blockWidthStandard: 7.7,
          kickerY: 1.02,
          titleY: 1.72,
          titleHeight: 1.62,
          subtitleY: 3.48,
          subtitleHeight: 1.0,
          bylineY: 6.0
        }
      },
      section: {
        statement: {
          barWidthWide: 1.8,
          barWidthStandard: 1.45,
          barY: 1.45,
          titleY: 1.95,
          titleHeight: 1.55,
          subtitleY: 3.82
        }
      },
      bullets: {
        contentAside: {
          margin: 0.82,
          top: 1.42,
          height: 4.95,
          gap: 0.45,
          rightWidthWide: 3.7,
          rightWidthStandard: 3.0,
          bodyHeight: 0.64,
          bodyGap: 0.18
        },
        twoColumn: {
          margin: 0.82,
          top: 1.42,
          height: 4.95,
          gap: 0.56,
          bodyHeight: 0.64,
          bodyGap: 0.2,
          dividerWidth: 0.04
        }
      },
      image: {
        margin: 0.82,
        gap: 0.44,
        top: 1.42,
        height: 4.95,
        imageWidthWide: 7.7,
        imageWidthStandard: 5.7,
        captionHeightWithBullets: 1.05
      },
      closing: {
        minimal: {
          barWidthWide: 2.1,
          barWidthStandard: 1.75,
          barY: 2.08,
          titleY: 2.45,
          titleHeight: 1.05,
          subtitleY: 3.72
        }
      }
    },
    textRoleOverrides: {
      heroTitle: { wide: 34, standard: 30 },
      heroSubtitle: { wide: 17, standard: 15 },
      sectionTitle: { wide: 34, standard: 30 },
      slideTitle: { wide: 25, standard: 22 },
      bullets: { wide: 19, standard: 17 },
      closingTitle: { wide: 28, standard: 24 }
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: 4.25,
              h: 0.2,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            rect: {
              x: metrics.width - 0.36,
              y: 0.85,
              w: 0.36,
              h: 5.95,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            line: {
              x: 0.82,
              y: 6.92,
              w: metrics.width - 1.64,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 1.05,
          y: 7.0,
          w: 0.55,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accentDark,
          align: "right"
        };
      }
    }
  }),
  "consulting-proposal": createPreset({
    fonts: {
      title: "Aptos Display",
      body: "Aptos"
    },
    palette: {
      background: "F7F9FC",
      surface: "FFFFFF",
      text: "182431",
      muted: "5E6C7A",
      accent: "2B6CB0",
      accentDark: "1E4A75",
      border: "CFD8E3"
    },
    defaultVariants: {
      title: "hero-left",
      section: "divider",
      bullets: "content-aside",
      image: "image-right",
      closing: "card"
    },
    layoutTokens: {
      content: {
        slideTitleMargin: 0.72,
        slideTitleY: 0.46,
        slideTitleHeight: 0.58,
        slideTitleAccentHeight: 0.04,
        slideTitleAccentWidthWide: 1.0,
        slideTitleAccentWidthStandard: 0.82,
        slideTitleAccentGap: 0.06
      },
      panel: {
        accentBarHeight: 0.06,
        accentBarInset: 0.12,
        accentBarColorKey: "accent",
        edgeBarWidth: 0.08,
        edgeBarPosition: "left",
        edgeBarColorKey: "accentDark"
      },
      title: {
        heroLeft: {
          margin: 0.74,
          kickerY: 0.98,
          kickerWidthWide: 2.35,
          kickerWidthStandard: 2.1,
          titleY: 1.58,
          titleHeight: 1.35,
          subtitleY: 3.08,
          subtitleWidthFactor: 0.8,
          bylineY: 6.02
        }
      },
      section: {
        divider: {
          margin: 0.78,
          labelY: 1.02,
          labelWidth: 1.8,
          titleY: 1.82,
          titleHeight: 1.42,
          subtitleY: 3.55,
          subtitleWidthFactor: 0.76
        }
      },
      bullets: {
        contentAside: {
          margin: 0.72,
          top: 1.28,
          height: 5.18,
          gap: 0.32,
          rightWidthWide: 3.4,
          rightWidthStandard: 2.8,
          bodyHeight: 0.66,
          bodyGap: 0.14
        }
      },
      image: {
        margin: 0.72,
        gap: 0.32,
        top: 1.28,
        height: 5.18,
        imageWidthWide: 7.2,
        imageWidthStandard: 5.35,
        captionHeightWithBullets: 1.0
      },
      closing: {
        card: {
          blockWidthWide: 9.1,
          blockWidthStandard: 7.55,
          titleY: 2.06,
          titleHeight: 1.05,
          subtitleY: 3.38,
          subtitleHeight: 0.88
        }
      }
    },
    textRoleOverrides: {
      heroTitle: { wide: 29, standard: 25 },
      sectionTitle: { wide: 30, standard: 26 },
      slideTitle: { wide: 21, standard: 19 },
      bullets: { wide: 18, standard: 16 },
      asideBody: { wide: 12, standard: 11 },
      closingTitle: { wide: 24, standard: 21 }
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: metrics.width,
              h: 0.12,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            rect: {
              x: 0.72,
              y: 0.82,
              w: 2.85,
              h: 0.12,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            line: {
              x: 0.72,
              y: 6.92,
              w: metrics.width - 1.44,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 1.1,
          y: 0.44,
          w: 0.6,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accentDark,
          align: "right"
        };
      }
    }
  }),
  "market-research": createPreset({
    fonts: {
      title: "Georgia",
      body: "Aptos"
    },
    palette: {
      background: "F5F7F9",
      surface: "FFFFFF",
      text: "21303A",
      muted: "64727B",
      accent: "3B7A84",
      accentDark: "27545B",
      border: "D5DEE2"
    },
    defaultVariants: {
      image: "image-right",
      closing: "minimal"
    },
    layoutTokens: {
      content: {
        slideTitleMargin: 0.76,
        slideTitleAccentHeight: 0.05,
        slideTitleAccentWidthWide: 0.95,
        slideTitleAccentWidthStandard: 0.8,
        slideTitleAccentGap: 0.08
      },
      panel: {
        accentBarHeight: 0.08,
        accentBarInset: 0.12,
        accentBarColorKey: "accentDark"
      },
      title: {
        heroLeft: {
          margin: 0.78,
          kickerY: 0.96,
          kickerWidthWide: 2.3,
          kickerWidthStandard: 2.05,
          titleY: 1.62,
          subtitleY: 3.12,
          subtitleWidthFactor: 0.74,
          bylineY: 5.98
        }
      },
      section: {
        divider: {
          margin: 0.8,
          labelWidth: 1.7,
          titleY: 1.82,
          subtitleY: 3.58,
          subtitleWidthFactor: 0.72
        }
      },
      bullets: {
        contentAside: {
          margin: 0.76,
          top: 1.28,
          height: 5.18,
          gap: 0.34,
          rightWidthWide: 3.65,
          rightWidthStandard: 2.95,
          bodyHeight: 0.68,
          bodyGap: 0.12
        }
      },
      image: {
        margin: 0.76,
        gap: 0.34,
        top: 1.28,
        height: 5.18,
        imageWidthWide: 7.0,
        imageWidthStandard: 5.2,
        captionHeightWithBullets: 1.22
      },
      closing: {
        minimal: {
          barWidthWide: 1.3,
          barWidthStandard: 1.05
        }
      }
    },
    textRoleOverrides: {
      heroTitle: { wide: 29, standard: 25 },
      sectionTitle: { wide: 31, standard: 27 },
      slideTitle: { wide: 22, standard: 19 },
      bullets: { wide: 18, standard: 16 },
      asideBody: { wide: 12, standard: 11 }
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: 0.2,
              h: metrics.height,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            rect: {
              x: 0.2,
              y: 0,
              w: 3.8,
              h: 0.11,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            line: {
              x: 0.76,
              y: 6.92,
              w: metrics.width - 1.52,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 1.0,
          y: 7.0,
          w: 0.5,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accentDark,
          align: "right"
        };
      }
    }
  }),
  "pitch-deck": createPreset({
    fonts: {
      title: "Bahnschrift",
      body: "Aptos"
    },
    palette: {
      background: "FFF7F0",
      surface: "FFFFFF",
      text: "20161A",
      muted: "6A5A60",
      accent: "FF6B35",
      accentDark: "B83616",
      border: "F3C5B6"
    },
    defaultVariants: {
      title: "hero-center",
      section: "statement",
      bullets: pitchDeckBulletsDefaultVariant,
      image: "image-right",
      closing: "minimal"
    },
    layoutTokens: {
      content: {
        slideTitleMargin: 0.84,
        slideTitleY: 0.48,
        slideTitleAccentHeight: 0.08,
        slideTitleAccentWidthWide: 1.4,
        slideTitleAccentWidthStandard: 1.15,
        slideTitleAccentGap: 0.08
      },
      panel: {
        lineColorKey: "accent",
        lineWidth: 1.2,
        edgeBarWidth: 0.12,
        edgeBarPosition: "right",
        edgeBarColorKey: "accentDark"
      },
      title: {
        heroCenter: {
          pillWidthWide: 2.8,
          pillWidthStandard: 2.45,
          blockWidthWide: 9.7,
          blockWidthStandard: 7.8,
          kickerY: 1.0,
          titleY: 1.72,
          titleHeight: 1.7,
          subtitleY: 3.5,
          subtitleHeight: 0.92,
          bylineY: 6.02
        }
      },
      section: {
        statement: {
          barWidthWide: 2.0,
          barWidthStandard: 1.6,
          barY: 1.46,
          titleY: 1.95,
          titleHeight: 1.55,
          subtitleY: 3.86
        }
      },
      bullets: {
        contentAside: {
          margin: 0.84,
          top: 1.42,
          height: 4.9,
          gap: 0.44,
          rightWidthWide: 3.4,
          rightWidthStandard: 2.9,
          bodyHeight: 0.64,
          bodyGap: 0.18
        },
        twoColumn: {
          margin: 0.84,
          top: 1.42,
          height: 4.9,
          gap: 0.56,
          bodyHeight: 0.64,
          bodyGap: 0.2,
          dividerWidth: 0.04
        }
      },
      image: {
        margin: 0.84,
        gap: 0.44,
        top: 1.42,
        height: 4.9,
        imageWidthWide: 7.85,
        imageWidthStandard: 5.75,
        captionHeightWithBullets: 0.95
      },
      closing: {
        minimal: {
          barWidthWide: 2.2,
          barWidthStandard: 1.8,
          barY: 2.05,
          titleY: 2.36,
          titleHeight: 1.1,
          subtitleY: 3.64
        }
      }
    },
    textRoleOverrides: {
      heroTitle: { wide: 36, standard: 32, min: 22 },
      heroSubtitle: { wide: 17, standard: 15 },
      sectionTitle: { wide: 35, standard: 31 },
      slideTitle: { wide: 26, standard: 22 },
      bullets: { wide: 18, standard: 16 },
      closingTitle: { wide: 30, standard: 26 }
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: metrics.width,
              h: 0.18,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            rect: {
              x: metrics.width - 0.34,
              y: 0.88,
              w: 0.34,
              h: 5.9,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            rect: {
              x: 0.84,
              y: metrics.height - 0.18,
              w: 2.6,
              h: 0.18,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 1.0,
          y: 0.34,
          w: 0.5,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accentDark,
          align: "right"
        };
      }
    }
  }),
  "project-kickoff": createPreset({
    fonts: {
      title: "Aptos Display",
      body: "Aptos"
    },
    palette: {
      background: "F6F7FB",
      surface: "FFFFFF",
      text: "1D2840",
      muted: "60708A",
      accent: "7B6CF6",
      accentDark: "493AAE",
      border: "D8DCEE"
    },
    defaultVariants: {
      image: "image-right",
      closing: "minimal"
    },
    layoutTokens: {
      content: {
        slideTitleMargin: 0.72,
        slideTitleY: 0.44,
        slideTitleAccentHeight: 0.06,
        slideTitleAccentWidthWide: 1.2,
        slideTitleAccentWidthStandard: 0.95,
        slideTitleAccentGap: 0.06
      },
      panel: {
        accentBarHeight: 0.08,
        accentBarColorKey: "accentDark",
        edgeBarWidth: 0.1,
        edgeBarPosition: "left",
        edgeBarColorKey: "accent"
      },
      title: {
        heroLeft: {
          margin: 0.78,
          kickerY: 0.98,
          kickerWidthWide: 2.5,
          kickerWidthStandard: 2.2,
          titleY: 1.62,
          subtitleY: 3.08,
          subtitleWidthFactor: 0.8,
          bylineY: 6.0
        }
      },
      section: {
        divider: {
          margin: 0.76,
          labelY: 0.98,
          labelWidth: 1.75,
          titleY: 1.8,
          subtitleY: 3.58,
          subtitleWidthFactor: 0.8
        }
      },
      bullets: {
        contentAside: {
          margin: 0.72,
          top: 1.26,
          height: 5.2,
          gap: 0.32,
          rightWidthWide: 3.7,
          rightWidthStandard: 3.0,
          bodyHeight: 0.68,
          bodyGap: 0.12
        }
      },
      image: {
        margin: 0.72,
        gap: 0.32,
        top: 1.26,
        height: 5.2,
        imageWidthWide: 7.15,
        imageWidthStandard: 5.28,
        captionHeightWithBullets: 1.1
      },
      closing: {
        minimal: {
          barWidthWide: 1.7,
          barWidthStandard: 1.4,
          barY: 2.14,
          titleY: 2.48,
          titleHeight: 0.98,
          subtitleY: 3.68
        }
      }
    },
    textRoleOverrides: {
      heroTitle: { wide: 31, standard: 27 },
      sectionTitle: { wide: 31, standard: 27 },
      slideTitle: { wide: 23, standard: 20 },
      bullets: { wide: 18, standard: 16 },
      closingTitle: { wide: 26, standard: 23 }
    },
    master: {
      objects(metrics, palette) {
        return [
          {
            rect: {
              x: 0,
              y: 0,
              w: 0.28,
              h: metrics.height,
              line: { color: palette.accent, width: 1 },
              fill: { color: palette.accent }
            }
          },
          {
            rect: {
              x: 0.28,
              y: 0,
              w: metrics.width - 0.28,
              h: 0.08,
              line: { color: palette.accentDark, width: 1 },
              fill: { color: palette.accentDark }
            }
          },
          {
            line: {
              x: 0.72,
              y: 6.92,
              w: metrics.width - 1.44,
              h: 0,
              line: { color: palette.border, width: 1 }
            }
          }
        ];
      },
      slideNumber(metrics, fonts, palette) {
        return {
          x: metrics.width - 1.05,
          y: 7.0,
          w: 0.55,
          h: 0.2,
          fontFace: fonts.body,
          fontSize: 9,
          color: palette.accentDark,
          align: "right"
        };
      }
    }
  })
});
const SUPPORTED_TEMPLATE_PRESETS = Object.freeze(Object.keys(TEMPLATE_PRESETS));

function createPreset(definition) {
  return {
    ...definition,
    defaultVariants: {
      ...DEFAULT_VARIANTS,
      ...(definition.defaultVariants || {})
    },
    layoutTokens: mergeDeep(BASE_LAYOUT_TOKENS, definition.layoutTokens || {}),
    textRoles: mergeDeep(TEXT_ROLES, definition.textRoleOverrides || {}),
    textRoleOverrides: definition.textRoleOverrides || {}
  };
}

function mergeDeep(baseValue, overrideValue) {
  if (overrideValue == null) {
    return cloneValue(baseValue);
  }

  if (baseValue == null) {
    return cloneValue(overrideValue);
  }

  if (Array.isArray(overrideValue)) {
    return overrideValue.slice();
  }

  if (isPlainObject(baseValue) && isPlainObject(overrideValue)) {
    const result = {};
    const keys = new Set([...Object.keys(baseValue), ...Object.keys(overrideValue)]);
    for (const key of keys) {
      if (Object.prototype.hasOwnProperty.call(overrideValue, key)) {
        result[key] = mergeDeep(baseValue[key], overrideValue[key]);
      } else {
        result[key] = cloneValue(baseValue[key]);
      }
    }
    return result;
  }

  return cloneValue(overrideValue);
}

function cloneValue(value) {
  if (Array.isArray(value)) {
    return value.slice();
  }
  if (isPlainObject(value)) {
    return mergeDeep({}, value);
  }
  return value;
}

function isPlainObject(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function pitchDeckBulletsDefaultVariant(slideSpec) {
  if (hasAsideContent(slideSpec)) {
    return "content-aside";
  }

  const bulletCount = Array.isArray(slideSpec?.bullets) ? slideSpec.bullets.length : 0;
  return bulletCount >= 4 ? "two-column" : "content-aside";
}

if (process.argv[1] && path.resolve(process.argv[1]) === SCRIPT_PATH) {
  main().catch(handleFatalError);
}

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
  const theme = normalizeTheme(rawSpec.theme, "theme");
  const templatePreset = normalizeTemplatePreset(rawSpec.template_preset, "template_preset");
  const activePreset = resolveActivePreset({ theme, templatePreset });
  const title = normalizeRequiredString(rawSpec.title, "title");
  const subtitle = normalizeOptionalString(rawSpec.subtitle, "subtitle");
  const filename = normalizeOptionalString(rawSpec.filename, "filename");
  const lang = normalizeLang(rawSpec.lang, "lang");
  const notes = normalizeOptionalNotes(rawSpec.notes, "notes");
  const sources = normalizeOptionalSources(rawSpec.sources, "sources");
  const slides = await Promise.all(
    normalizeArray(rawSpec.slides, "slides").map((slide, index) => normalizeSlide(slide, index, activePreset))
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
    theme,
    templatePreset,
    activePreset,
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

async function normalizeSlide(rawSlide, index, activePreset) {
  if (!rawSlide || typeof rawSlide !== "object" || Array.isArray(rawSlide)) {
    throw new Error(`slides[${index}] must be an object`);
  }

  const type = normalizeRequiredString(rawSlide.type, `slides[${index}].type`);
  const variant = normalizeSlideVariant(type, rawSlide.variant, `slides[${index}].variant`, activePreset, rawSlide);
  const common = {
    notes: normalizeOptionalNotes(rawSlide.notes, `slides[${index}].notes`),
    sources: normalizeOptionalSources(rawSlide.sources, `slides[${index}].sources`)
  };

  switch (type) {
    case "title":
      return {
        ...common,
        type,
        variant,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        subtitle: normalizeOptionalString(rawSlide.subtitle, `slides[${index}].subtitle`),
        kicker: normalizeOptionalString(rawSlide.kicker, `slides[${index}].kicker`),
        byline: normalizeOptionalString(rawSlide.byline, `slides[${index}].byline`)
      };
    case "section":
      return {
        ...common,
        type,
        variant,
        title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
        label: normalizeOptionalString(rawSlide.label, `slides[${index}].label`),
        subtitle: normalizeOptionalString(rawSlide.subtitle, `slides[${index}].subtitle`)
      };
    case "bullets":
      {
        const asideTitle = normalizeOptionalString(rawSlide.aside_title, `slides[${index}].aside_title`);
        const asideBody = normalizeOptionalString(rawSlide.aside_body, `slides[${index}].aside_body`);
        const asideBullets = normalizeOptionalStringList(rawSlide.aside_bullets, `slides[${index}].aside_bullets`);
        if (variant === "two-column" && (asideTitle || asideBody || asideBullets.length > 0)) {
          throw new Error(
            `slides[${index}].variant "two-column" cannot be combined with aside_title, aside_body, or aside_bullets`
          );
        }
        return {
          ...common,
          type,
          variant,
          title: normalizeRequiredString(rawSlide.title, `slides[${index}].title`),
          body: normalizeOptionalString(rawSlide.body, `slides[${index}].body`),
          bullets: normalizeStringList(rawSlide.bullets, `slides[${index}].bullets`),
          asideTitle,
          asideBody,
          asideBullets
        };
      }
    case "image": {
      const imagePath = normalizeRequiredString(rawSlide.image_path, `slides[${index}].image_path`);
      const resolvedImagePath = resolveAgainstRepo(imagePath);
      await assertReadableFile(resolvedImagePath, `slides[${index}].image_path`);
      return {
        ...common,
        type,
        variant,
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
        variant,
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

function normalizeTheme(value, fieldName) {
  if (value == null || value === "") {
    return DEFAULT_THEME;
  }

  const theme = normalizeRequiredString(value, fieldName).toLowerCase();
  if (!LEGACY_THEME_NAMES.includes(theme)) {
    throw new Error(`${fieldName} must be one of: ${LEGACY_THEME_NAMES.join(", ")}`);
  }

  return theme;
}

function normalizeTemplatePreset(value, fieldName) {
  if (value == null || value === "") {
    return "";
  }

  const preset = normalizeRequiredString(value, fieldName).toLowerCase();
  if (!SUPPORTED_TEMPLATE_PRESETS.includes(preset)) {
    throw new Error(`${fieldName} must be one of: ${SUPPORTED_TEMPLATE_PRESETS.join(", ")}`);
  }

  return preset;
}

function resolveActivePreset({ theme = DEFAULT_THEME, templatePreset = "" } = {}) {
  return templatePreset || theme || DEFAULT_THEME;
}

function normalizeSlideVariant(type, value, fieldName, presetName, rawSlide) {
  const variants = SLIDE_VARIANTS[type];
  if (!variants) {
    return "";
  }

  if (value == null || value === "") {
    return getDefaultVariantForPreset(presetName, type, rawSlide);
  }

  const variant = normalizeRequiredString(value, fieldName).toLowerCase();
  if (!variants.includes(variant)) {
    throw new Error(`${fieldName} must be one of: ${variants.join(", ")}`);
  }

  return variant;
}

function getDefaultVariantForPreset(presetName, type, slideSpec = {}) {
  const preset = getPreset(presetName);
  const configured = preset.defaultVariants[type];
  const variant = typeof configured === "function" ? configured(slideSpec) : configured;
  return variant || DEFAULT_VARIANTS[type] || "";
}

function getPreset(presetName = DEFAULT_THEME) {
  return TEMPLATE_PRESETS[presetName] || TEMPLATE_PRESETS[DEFAULT_THEME];
}

function getPresetLayoutTokens(presetName) {
  return getPreset(presetName).layoutTokens;
}

function getPresetTextRoleOverrides(presetName) {
  return getPreset(presetName).textRoleOverrides || {};
}

function getTextRoleForPreset(presetName, roleName) {
  const preset = getPreset(presetName);
  return preset.textRoles[roleName] || TEXT_ROLES[roleName];
}

function getDeckPresetName(deckSpec) {
  return resolveActivePreset({
    theme: deckSpec?.theme || DEFAULT_THEME,
    templatePreset: deckSpec?.activePreset || deckSpec?.templatePreset || ""
  });
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
  const canonicalCandidatePath = resolvePathThroughRealParents(candidatePath);
  const canonicalRootPath = fsSync.realpathSync.native(path.resolve(rootPath));
  const relative = path.relative(canonicalRootPath, canonicalCandidatePath);
  return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
}

function resolvePathThroughRealParents(candidatePath) {
  const resolvedCandidatePath = path.resolve(candidatePath);
  const missingSegments = [];
  let currentPath = resolvedCandidatePath;

  for (;;) {
    if (fsSync.existsSync(currentPath)) {
      const realPath = fsSync.realpathSync.native(currentPath);
      return missingSegments.length === 0 ? realPath : path.join(realPath, ...missingSegments.reverse());
    }

    const parentPath = path.dirname(currentPath);
    if (parentPath === currentPath) {
      throw new Error(`output path does not have an existing parent: ${candidatePath}`);
    }

    missingSegments.push(path.basename(currentPath));
    currentPath = parentPath;
  }
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
      const effectiveSubtitle = getClosingSlideSubtitle(slide);
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

  defineDefaultMaster(pptx, metrics, spec.activePreset);

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

function defineDefaultMaster(pptx, metrics, presetName = DEFAULT_THEME) {
  const preset = getPreset(presetName);
  pptx.defineSlideMaster({
    title: MASTER_NAME,
    background: { color: preset.palette.background },
    objects: preset.master.objects(metrics, preset.palette),
    slideNumber: preset.master.slideNumber(metrics, preset.fonts, preset.palette)
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
  if (slideSpec.variant === "hero-center") {
    renderTitleSlideHeroCenter(slide, slideSpec, deckSpec, metrics);
    return;
  }

  renderTitleSlideHeroLeft(slide, slideSpec, deckSpec, metrics);
}

function renderTitleSlideHeroLeft(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).title.heroLeft;
  const margin = tokens.margin;
  const contentWidth = metrics.width - margin * 2;
  const subtitle = slideSpec.subtitle || deckSpec.subtitle || "";
  const kickerWidth = pickByLayout(tokens, metrics, "kickerWidthWide", "kickerWidthStandard");
  const kicker = slideSpec.kicker || "";

  if (kicker) {
    addFittedText(slide, kicker, {
      x: margin,
      y: tokens.kickerY,
      w: kickerWidth,
      h: tokens.kickerHeight
    }, "kicker", metrics, deckSpec, {
      bold: true,
      color: tokens.kickerTextColor,
      align: "center",
      fill: { color: resolvePaletteColor(palette, tokens.kickerFillColorKey, "accentDark") }
    });
  }

  addFittedText(slide, slideSpec.title, {
    x: margin,
    y: tokens.titleY,
    w: contentWidth,
    h: tokens.titleHeight
  }, "heroTitle", metrics, deckSpec, {
    bold: true,
    color: resolvePaletteColor(palette, tokens.titleColorKey, "text")
  });

  addFittedText(slide, subtitle, {
    x: margin,
    y: tokens.subtitleY,
    w: contentWidth * tokens.subtitleWidthFactor,
    h: tokens.subtitleHeight
  }, "heroSubtitle", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.subtitleColorKey, "muted")
  });

  addFittedText(slide, slideSpec.byline, {
    x: margin,
    y: tokens.bylineY,
    w: contentWidth,
    h: tokens.bylineHeight
  }, "byline", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bylineColorKey, "muted")
  });
}

function renderTitleSlideHeroCenter(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).title.heroCenter;
  const subtitle = slideSpec.subtitle || deckSpec.subtitle || "";
  const pillWidth = pickByLayout(tokens, metrics, "pillWidthWide", "pillWidthStandard");
  const blockWidth = pickByLayout(tokens, metrics, "blockWidthWide", "blockWidthStandard");
  const blockX = (metrics.width - blockWidth) / 2;
  const kicker = slideSpec.kicker || "";

  if (kicker) {
    addFittedText(slide, kicker, {
      x: (metrics.width - pillWidth) / 2,
      y: tokens.kickerY,
      w: pillWidth,
      h: tokens.kickerHeight
    }, "kicker", metrics, deckSpec, {
      bold: true,
      color: tokens.kickerTextColor,
      align: "center",
      fill: { color: resolvePaletteColor(palette, tokens.kickerFillColorKey, "accentDark") }
    });
  }

  addFittedText(slide, slideSpec.title, {
    x: blockX,
    y: tokens.titleY,
    w: blockWidth,
    h: tokens.titleHeight
  }, "heroTitle", metrics, deckSpec, {
    bold: true,
    color: resolvePaletteColor(palette, tokens.titleColorKey, "text"),
    align: "center"
  });

  addFittedText(slide, subtitle, {
    x: blockX + tokens.subtitleInset,
    y: tokens.subtitleY,
    w: blockWidth - tokens.subtitleInset * 2,
    h: tokens.subtitleHeight
  }, "heroSubtitle", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.subtitleColorKey, "muted"),
    align: "center"
  });

  addFittedText(slide, slideSpec.byline, {
    x: blockX,
    y: tokens.bylineY,
    w: blockWidth,
    h: tokens.bylineHeight
  }, "byline", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bylineColorKey, "muted"),
    align: "center"
  });
}

function renderSectionSlide(slide, slideSpec, deckSpec, metrics) {
  if (slideSpec.variant === "statement") {
    renderSectionSlideStatement(slide, slideSpec, deckSpec, metrics);
    return;
  }

  renderSectionSlideDivider(slide, slideSpec, deckSpec, metrics);
}

function renderSectionSlideDivider(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).section.divider;
  const margin = tokens.margin;
  const contentWidth = metrics.width - margin * 2;
  const label = getSectionLabelText(slideSpec, deckSpec, tokens.labelText);

  addFittedText(slide, label, {
    x: margin,
    y: tokens.labelY,
    w: tokens.labelWidth,
    h: tokens.labelHeight
  }, "sectionLabel", metrics, deckSpec, {
    bold: true,
    color: resolvePaletteColor(palette, tokens.labelColorKey, "accentDark")
  });

  addFittedText(slide, slideSpec.title, {
    x: margin,
    y: tokens.titleY,
    w: contentWidth,
    h: tokens.titleHeight
  }, "sectionTitle", metrics, deckSpec, {
    bold: true,
    color: resolvePaletteColor(palette, tokens.titleColorKey, "text")
  });

  addFittedText(slide, slideSpec.subtitle, {
    x: margin,
    y: tokens.subtitleY,
    w: contentWidth * tokens.subtitleWidthFactor,
    h: tokens.subtitleHeight
  }, "subtitle", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.subtitleColorKey, "muted")
  });
}

function renderSectionSlideStatement(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).section.statement;
  const barWidth = pickByLayout(tokens, metrics, "barWidthWide", "barWidthStandard");
  const blockWidth = pickByLayout(tokens, metrics, "blockWidthWide", "blockWidthStandard");
  const blockX = (metrics.width - blockWidth) / 2;

  slide.addShape("rect", {
    x: (metrics.width - barWidth) / 2,
    y: tokens.barY,
    w: barWidth,
    h: tokens.barHeight,
    fill: { color: resolvePaletteColor(palette, tokens.barColorKey, "accent") },
    line: { color: resolvePaletteColor(palette, tokens.barColorKey, "accent"), width: 1 }
  });

  addFittedText(slide, slideSpec.title, {
    x: blockX,
    y: tokens.titleY,
    w: blockWidth,
    h: tokens.titleHeight
  }, "sectionTitle", metrics, deckSpec, {
    bold: true,
    color: resolvePaletteColor(palette, tokens.titleColorKey, "text"),
    align: "center"
  });

  addFittedText(slide, slideSpec.subtitle, {
    x: blockX + tokens.subtitleInset,
    y: tokens.subtitleY,
    w: blockWidth - tokens.subtitleInset * 2,
    h: tokens.subtitleHeight
  }, "subtitle", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.subtitleColorKey, "muted"),
    align: "center"
  });
}

function renderBulletsSlide(slide, slideSpec, deckSpec, metrics) {
  if (slideSpec.variant === "two-column") {
    renderBulletsSlideTwoColumn(slide, slideSpec, deckSpec, metrics);
    return;
  }

  renderBulletsSlideAside(slide, slideSpec, deckSpec, metrics);
}

function renderBulletsSlideAside(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).bullets.contentAside;
  const margin = tokens.margin;
  const contentTop = tokens.top;
  const contentHeight = tokens.height;
  const gap = tokens.gap;
  const hasAside = hasAsideContent(slideSpec);
  const contentWidth = metrics.width - margin * 2;
  const rightWidth = hasAside ? pickByLayout(tokens, metrics, "rightWidthWide", "rightWidthStandard") : 0;
  const leftWidth = hasAside ? contentWidth - gap - rightWidth : contentWidth;
  const bodyHeight = slideSpec.body ? tokens.bodyHeight : 0;
  const bodyBottomGap = slideSpec.body ? tokens.bodyGap : 0;
  const bulletsY = contentTop + bodyHeight + bodyBottomGap;
  const bulletsHeight = contentHeight - (bulletsY - contentTop);

  addSlideTitle(slide, slideSpec.title, metrics, deckSpec, margin);

  addFittedText(slide, slideSpec.body, {
    x: margin,
    y: contentTop,
    w: leftWidth,
    h: bodyHeight || 0.6
  }, "body", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bodyColorKey, "muted")
  });

  addBulletList(slide, slideSpec.bullets, {
    x: margin,
    y: bulletsY,
    w: leftWidth,
    h: bulletsHeight
  }, "bullets", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bulletsColorKey, "text"),
    bulletIndent: tokens.bulletsIndent
  });

  if (!hasAside) {
    return;
  }

  const asideX = margin + leftWidth + gap;
  const panelPaddingX = tokens.panelPaddingX;
  let cursorY = contentTop + tokens.panelInsetY;

  addPanelFrame(slide, {
    x: asideX,
    y: contentTop,
    w: rightWidth,
    h: contentHeight
  }, deckSpec);

  if (slideSpec.asideTitle) {
    addFittedText(slide, slideSpec.asideTitle, {
      x: asideX + panelPaddingX,
      y: cursorY,
      w: rightWidth - panelPaddingX * 2,
      h: tokens.asideTitleHeight
    }, "asideTitle", metrics, deckSpec, {
      bold: true,
      color: resolvePaletteColor(palette, tokens.asideTitleColorKey, "accentDark")
    });
    cursorY += tokens.asideTitleGap;
  }

  if (slideSpec.asideBody) {
    const asideBodyHeight = slideSpec.asideBullets.length > 0
      ? tokens.asideBodyHeightWithBullets
      : contentHeight - (cursorY - contentTop) - tokens.panelBottomPad;
    addFittedText(slide, slideSpec.asideBody, {
      x: asideX + panelPaddingX,
      y: cursorY,
      w: rightWidth - panelPaddingX * 2,
      h: asideBodyHeight
    }, "asideBody", metrics, deckSpec, {
      color: resolvePaletteColor(palette, tokens.asideBodyColorKey, "text")
    });
    cursorY += asideBodyHeight + 0.08;
  }

  if (slideSpec.asideBullets.length > 0) {
    const bulletInsetX = tokens.panelBulletInsetX;
    addBulletList(slide, slideSpec.asideBullets, {
      x: asideX + bulletInsetX,
      y: cursorY,
      w: rightWidth - bulletInsetX * 2,
      h: Math.max(0.7, contentHeight - (cursorY - contentTop) - tokens.panelBottomPad)
    }, "asideBody", metrics, deckSpec, {
      color: resolvePaletteColor(palette, tokens.asideBodyColorKey, "text"),
      bulletIndent: tokens.panelBulletIndent
    });
  }
}

function renderBulletsSlideTwoColumn(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).bullets.twoColumn;
  const margin = tokens.margin;
  const contentTop = tokens.top;
  const contentHeight = tokens.height;
  const gap = tokens.gap;
  const contentWidth = metrics.width - margin * 2;
  const columnWidth = (contentWidth - gap) / 2;
  const bodyHeight = slideSpec.body ? tokens.bodyHeight : 0;
  const bodyBottomGap = slideSpec.body ? tokens.bodyGap : 0;
  const bulletsY = contentTop + bodyHeight + bodyBottomGap;
  const bulletsHeight = contentHeight - (bulletsY - contentTop);
  const [leftItems, rightItems] = splitItemsForColumns(slideSpec.bullets);

  addSlideTitle(slide, slideSpec.title, metrics, deckSpec, margin);

  addFittedText(slide, slideSpec.body, {
    x: margin,
    y: contentTop,
    w: contentWidth,
    h: bodyHeight || 0.6
  }, "body", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bodyColorKey, "muted")
  });

  addBulletList(slide, leftItems, {
    x: margin,
    y: bulletsY,
    w: columnWidth,
    h: bulletsHeight
  }, "body", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bulletsColorKey, "text"),
    bulletIndent: tokens.bulletsIndent
  });

  addBulletList(slide, rightItems, {
    x: margin + columnWidth + gap,
    y: bulletsY,
    w: columnWidth,
    h: bulletsHeight
  }, "body", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.bulletsColorKey, "text"),
    bulletIndent: tokens.bulletsIndent
  });

  if (rightItems.length > 0) {
    slide.addShape("rect", {
      x: margin + columnWidth + gap / 2 - tokens.dividerWidth / 2,
      y: bulletsY + tokens.dividerInsetY,
      w: tokens.dividerWidth,
      h: Math.max(0.8, bulletsHeight - tokens.dividerInsetY * 2),
      fill: { color: resolvePaletteColor(palette, tokens.dividerColorKey, "border") },
      line: { color: resolvePaletteColor(palette, tokens.dividerColorKey, "border"), width: 1 }
    });
  }
}

function splitItemsForColumns(items) {
  if (items.length <= 1) {
    return [items, []];
  }

  const left = [];
  const right = [];
  let leftChars = 0;
  let rightChars = 0;

  for (const item of items) {
    if (leftChars <= rightChars) {
      left.push(item);
      leftChars += item.length;
    } else {
      right.push(item);
      rightChars += item.length;
    }
  }

  if (right.length === 0) {
    right.push(left.pop());
  }

  return [left, right];
}

function renderImageSlide(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).image;
  const margin = tokens.margin;
  const gap = tokens.gap;
  const contentTop = tokens.top;
  const contentHeight = tokens.height;
  const contentWidth = metrics.width - margin * 2;
  const imageWidth = pickByLayout(tokens, metrics, "imageWidthWide", "imageWidthStandard");
  const textWidth = contentWidth - gap - imageWidth;
  const isImageRight = slideSpec.variant === "image-right";
  const panelX = isImageRight ? margin : margin + imageWidth + gap;
  const imageX = isImageRight ? margin + textWidth + gap : margin;
  const hasPanelContent = slideSpec.caption || slideSpec.bullets.length > 0;

  addSlideTitle(slide, slideSpec.title, metrics, deckSpec, margin);

  slide.addImage({
    path: slideSpec.imagePath,
    x: imageX,
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
  }, deckSpec);

  let cursorY = contentTop + tokens.panelTopInset;
  if (slideSpec.caption) {
    const captionHeight = slideSpec.bullets.length > 0
      ? tokens.captionHeightWithBullets
      : contentHeight - (cursorY - contentTop) - tokens.panelBottomPad;
    addFittedText(slide, slideSpec.caption, {
      x: panelX + tokens.panelPaddingX,
      y: cursorY,
      w: textWidth - tokens.panelPaddingX * 2,
      h: captionHeight
    }, "body", metrics, deckSpec, {
      color: resolvePaletteColor(palette, tokens.captionColorKey, "text")
    });
    cursorY += captionHeight + 0.08;
  }

  if (slideSpec.bullets.length > 0) {
    const bulletInsetX = tokens.panelBulletInsetX;
    addBulletList(slide, slideSpec.bullets, {
      x: panelX + bulletInsetX,
      y: cursorY,
      w: textWidth - bulletInsetX * 2,
      h: Math.max(0.7, contentHeight - (cursorY - contentTop) - tokens.panelBottomPad)
    }, "body", metrics, deckSpec, {
      color: resolvePaletteColor(palette, tokens.bulletsColorKey, "text"),
      bulletIndent: tokens.panelBulletIndent
    });
  }
}

function renderClosingSlide(slide, slideSpec, deckSpec, metrics) {
  if (slideSpec.variant === "minimal") {
    renderClosingSlideMinimal(slide, slideSpec, deckSpec, metrics);
    return;
  }

  renderClosingSlideCard(slide, slideSpec, deckSpec, metrics);
}

function renderClosingSlideCard(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).closing.card;
  const blockWidth = pickByLayout(tokens, metrics, "blockWidthWide", "blockWidthStandard");
  const blockX = (metrics.width - blockWidth) / 2;
  const subtitle = getClosingSlideSubtitle(slideSpec);

  addFittedText(slide, slideSpec.title, {
    x: blockX,
    y: tokens.titleY,
    w: blockWidth,
    h: tokens.titleHeight
  }, "closingTitle", metrics, deckSpec, {
    bold: true,
    color: tokens.titleColor,
    align: "center",
    fill: { color: resolvePaletteColor(palette, tokens.titleFillColorKey, "accentDark") }
  });

  addFittedText(slide, subtitle, {
    x: blockX,
    y: tokens.subtitleY,
    w: blockWidth,
    h: tokens.subtitleHeight
  }, "closingSubtitle", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.subtitleColorKey, "muted"),
    align: "center"
  });
}

function renderClosingSlideMinimal(slide, slideSpec, deckSpec, metrics) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).closing.minimal;
  const barWidth = pickByLayout(tokens, metrics, "barWidthWide", "barWidthStandard");
  const blockWidth = pickByLayout(tokens, metrics, "blockWidthWide", "blockWidthStandard");
  const blockX = (metrics.width - blockWidth) / 2;
  const subtitle = getClosingSlideSubtitle(slideSpec);

  slide.addShape("rect", {
    x: (metrics.width - barWidth) / 2,
    y: tokens.barY,
    w: barWidth,
    h: tokens.barHeight,
    fill: { color: resolvePaletteColor(palette, tokens.barColorKey, "accent") },
    line: { color: resolvePaletteColor(palette, tokens.barColorKey, "accent"), width: 1 }
  });

  addFittedText(slide, slideSpec.title, {
    x: blockX,
    y: tokens.titleY,
    w: blockWidth,
    h: tokens.titleHeight
  }, "closingTitle", metrics, deckSpec, {
    bold: true,
    color: resolvePaletteColor(palette, tokens.titleColorKey, "text"),
    align: "center"
  });

  addFittedText(slide, subtitle, {
    x: blockX + tokens.subtitleInset,
    y: tokens.subtitleY,
    w: blockWidth - tokens.subtitleInset * 2,
    h: tokens.subtitleHeight
  }, "closingSubtitle", metrics, deckSpec, {
    color: resolvePaletteColor(palette, tokens.subtitleColorKey, "muted"),
    align: "center"
  });
}

function getClosingSlideSubtitle(slideSpec) {
  return slideSpec.subtitle || "";
}

function addSlideTitle(slide, title, metrics, deckSpec, marginOverride) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const tokens = getPresetLayoutTokens(presetName).content;
  const margin = marginOverride ?? tokens.slideTitleMargin;

  addFittedText(slide, title, {
    x: margin,
    y: tokens.slideTitleY,
    w: metrics.width - margin * 2,
    h: tokens.slideTitleHeight
  }, "slideTitle", metrics, deckSpec, {
    bold: true,
    color: palette.text
  });

  if (tokens.slideTitleAccentHeight > 0) {
    const accentWidth = pickByLayout(tokens, metrics, "slideTitleAccentWidthWide", "slideTitleAccentWidthStandard");
    const accentX = tokens.slideTitleAccentAlign === "center"
      ? (metrics.width - accentWidth) / 2
      : margin + tokens.slideTitleAccentInset;

    slide.addShape("rect", {
      x: accentX,
      y: tokens.slideTitleY + tokens.slideTitleHeight + tokens.slideTitleAccentGap,
      w: accentWidth,
      h: tokens.slideTitleAccentHeight,
      fill: { color: resolvePaletteColor(palette, tokens.slideTitleAccentColorKey, "accent") },
      line: { color: resolvePaletteColor(palette, tokens.slideTitleAccentColorKey, "accent"), width: 1 }
    });
  }
}

function addPanelFrame(slide, box, deckSpec) {
  const presetName = getDeckPresetName(deckSpec);
  const { palette } = getPreset(presetName);
  const panelTokens = getPresetLayoutTokens(presetName).panel;

  slide.addShape("rect", {
    ...box,
    fill: { color: resolvePaletteColor(palette, panelTokens.fillColorKey, "surface") },
    line: {
      color: resolvePaletteColor(palette, panelTokens.lineColorKey, "border"),
      width: panelTokens.lineWidth
    }
  });

  if (panelTokens.accentBarHeight > 0) {
    const accentX = box.x + panelTokens.accentBarInset;
    const accentWidth = Math.max(0.12, box.w - panelTokens.accentBarInset * 2);
    const accentY = panelTokens.accentBarPosition === "bottom"
      ? box.y + box.h - panelTokens.accentBarHeight
      : box.y;

    slide.addShape("rect", {
      x: accentX,
      y: accentY,
      w: accentWidth,
      h: panelTokens.accentBarHeight,
      fill: { color: resolvePaletteColor(palette, panelTokens.accentBarColorKey, "accent") },
      line: { color: resolvePaletteColor(palette, panelTokens.accentBarColorKey, "accent"), width: 1 }
    });
  }

  if (panelTokens.edgeBarWidth > 0) {
    const edgeX = panelTokens.edgeBarPosition === "right"
      ? box.x + box.w - panelTokens.edgeBarInset - panelTokens.edgeBarWidth
      : box.x + panelTokens.edgeBarInset;
    const edgeHeight = box.h - panelTokens.edgeBarInset * 2;

    slide.addShape("rect", {
      x: edgeX,
      y: box.y + panelTokens.edgeBarInset,
      w: panelTokens.edgeBarWidth,
      h: Math.max(0.12, edgeHeight),
      fill: { color: resolvePaletteColor(palette, panelTokens.edgeBarColorKey, "accentDark") },
      line: { color: resolvePaletteColor(palette, panelTokens.edgeBarColorKey, "accentDark"), width: 1 }
    });
  }
}

function addFittedText(slide, text, box, roleName, metrics, deckSpec, overrides = {}) {
  if (!text) {
    return;
  }

  slide.addText(text, {
    ...box,
    ...buildTextOptions(roleName, text, metrics, deckSpec, overrides)
  });
}

function addBulletList(slide, items, box, roleName, metrics, deckSpec, overrides = {}) {
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
    ...buildTextOptions(roleName, items.join(" "), metrics, deckSpec, {
      densityHint: Math.max(0, items.length - 4),
      ...textOverrides
    })
  });
}

function buildTextOptions(roleName, text, metrics, deckSpec, overrides = {}) {
  const presetName = getDeckPresetName(deckSpec);
  const preset = getPreset(presetName);
  const role = getTextRoleForPreset(presetName, roleName);
  if (!role) {
    throw new Error(`unknown text role: ${roleName}`);
  }
  const { densityHint = 0, ...rest } = overrides;
  const fontSize = rest.fontSize || pickFontSize(text, role, metrics, densityHint);

  return {
    fontFace: preset.fonts[role.fontKey] || preset.fonts.body,
    fontSize,
    fit: "shrink",
    wrap: true,
    margin: role.margin,
    lineSpacingMultiple: role.lineSpacingMultiple,
    valign: role.valign,
    lang: deckSpec.lang || undefined,
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

function hasAsideContent(slideSpec = {}) {
  return Boolean(slideSpec.aside_title || slideSpec.asideTitle || slideSpec.aside_body || slideSpec.asideBody)
    || (Array.isArray(slideSpec.aside_bullets) && slideSpec.aside_bullets.length > 0)
    || (Array.isArray(slideSpec.asideBullets) && slideSpec.asideBullets.length > 0);
}

function getSectionLabelText(slideSpec = {}, deckSpec = {}, fallbackLabel = "SECTION") {
  return slideSpec.label || localizeSectionLabel(deckSpec.lang, fallbackLabel);
}

function localizeSectionLabel(lang, fallbackLabel) {
  const languageTag = String(lang || "").trim().toLowerCase();
  if (!languageTag) {
    return fallbackLabel;
  }

  const baseLanguage = languageTag.split(/[-_]/, 1)[0];
  return LOCALIZED_SECTION_LABELS[baseLanguage] || fallbackLabel;
}

function pickByLayout(tokens, metrics, wideKey, standardKey) {
  return metrics.width > 11 ? tokens[wideKey] : tokens[standardKey];
}

function resolvePaletteColor(palette, keyOrValue, fallbackKey) {
  if (!keyOrValue && fallbackKey) {
    return palette[fallbackKey];
  }
  if (typeof keyOrValue === "string" && palette[keyOrValue]) {
    return palette[keyOrValue];
  }
  return keyOrValue || palette[fallbackKey];
}

function handleFatalError(error) {
  const result = {
    ok: false,
    error: error instanceof Error ? error.message : String(error)
  };

  process.stderr.write(`${JSON.stringify(result, null, 2)}\n`);
  process.exit(1);
}

export const __test__ = {
  REPO_ROOT,
  WORKSPACE_ROOT,
  TEMPLATE_PRESETS,
  buildPresentation,
  buildDensityWarnings,
  defineDefaultMaster,
  getDefaultVariantForPreset,
  getPreset,
  getPresetLayoutTokens,
  getPresetTextRoleOverrides,
  getSectionLabelText,
  localizeSectionLabel,
  normalizeSpec,
  normalizeTemplatePreset,
  renderBulletsSlide,
  renderImageSlide,
  renderClosingSlide,
  renderSectionSlide,
  renderTitleSlide,
  resolveActivePreset,
  resolveSafeOutputPath
};
