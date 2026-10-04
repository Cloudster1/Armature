import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { TOKEN_GROUPS } from "@/lib/theme-tokens";

// WCAG AA for body text. A token used only for large text or icons may be
// listed in LOWER_BAR at AA_LARGE_TEXT_OR_ICON.
const AA_TEXT = 4.5;
const AA_LARGE_TEXT_OR_ICON = 3;

const TEXT_TOKENS = TOKEN_GROUPS.find((group) => group.title === "Text")!.tokens.map((token) => token.name);
const SURFACE_TOKENS = ["canvas", "surface", "surface-raised", "surface-sunken", "surface-overlay"];

// Every text token is held to AA_TEXT except the ones named here, each with
// the reason it may sit lower. Add a token only with a reason that holds.
const NO_BAR = 1;
const LOWER_BAR: Record<string, { ratio: number; reason: string }> = {
  "ink-disabled": {
    ratio: NO_BAR,
    reason: "WCAG 1.4.3 exempts the text of inactive controls, and the faded ink is what tells a person the control is off",
  },
};

type Palette = Record<string, string>;

function channel(hex: string, offset: number): number {
  const value = parseInt(hex.slice(offset, offset + 2), 16) / 255;
  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  return 0.2126 * channel(hex, 1) + 0.7152 * channel(hex, 3) + 0.0722 * channel(hex, 5);
}

/** The WCAG contrast ratio of two #rrggbb colours. */
function contrast(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light! + 0.05) / (dark! + 0.05);
}

function declarations(block: string): Palette {
  return Object.fromEntries([...block.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1]!, m[2]!.trim().toLowerCase()]));
}

function goMap(body: string): Palette {
  return Object.fromEntries([...body.matchAll(/"([a-z0-9-]+)":\s*"([^"]+)"/g)].map((m) => [m[1]!, m[2]!.toLowerCase()]));
}

const css = readFileSync(join(__dirname, "index.css"), "utf8");
const clayLight = declarations(css.match(/@theme \{([\s\S]*?)\n\}/)![1]!);
const clayDarkChoice = declarations(css.match(/\n {2}:root\[data-theme="dark"\] \{([\s\S]*?)\n {2}\}/)![1]!);
const clayDarkSystem = declarations(css.match(/@media \(prefers-color-scheme: dark\) \{([\s\S]*?)\n {4}\}/)![1]!);

interface Example {
  key: string;
  light: Palette;
  dark: Palette;
  css: string;
}

// The shipped themes are Go, the one place they are written down; reading
// the source keeps this test from trusting a copy that could drift.
function readExamples(): Example[] {
  const path = process.env.ARMATURE_THEME_EXAMPLES ?? join(__dirname, "..", "..", "..", "backend", "internal", "theme", "examples.go");
  const source = readFileSync(path, "utf8");
  const bodies = new Map<string, string>();
  const funcs = [...source.matchAll(/\nfunc (\w+)\(/g)];
  funcs.forEach((m, i) => bodies.set(m[1]!, source.slice(m.index, funcs[i + 1]?.index)));

  const build = (name: string): Example => {
    const body = bodies.get(name);
    if (!body) throw new Error(`examples.go has no func ${name}() Example`);
    const base = body.match(/base := (\w+)\(\)/)?.[1];
    const inherited = base ? build(base) : { light: {}, dark: {} };
    const light: Palette = { ...inherited.light, ...goMap(body.match(/\blight := map\[string\]string\{([\s\S]*?)\n\t\}/)?.[1] ?? "") };
    const dark: Palette = { ...inherited.dark, ...goMap(body.match(/\bdark := map\[string\]string\{([\s\S]*?)\n\t\}/)?.[1] ?? "") };
    for (const m of body.matchAll(/range map\[string\]string\{([\s\S]*?)\}\s*\{\s*(light|dark)\[k\] = v/g)) {
      Object.assign(m[2] === "light" ? light : dark, goMap(m[1]!));
    }
    return { key: body.match(/Key:\s*"([^"]+)"/)![1]!, light, dark, css: body.match(/css := `([^`]*)`/)?.[1] ?? "" };
  };

  const listed = source.match(/examples := \[\]Example\{([^}]*)\}/)![1]!;
  return [...listed.matchAll(/(\w+)\(\)/g)].map((m) => build(m[1]!));
}

interface Case {
  name: string;
  palette: Palette;
  surfaces: string[];
}

// A theme's palette lands on top of the stylesheet, so what it leaves out is Clay's.
function cases(): Case[] {
  const out: Case[] = [
    { name: "Clay light", palette: clayLight, surfaces: SURFACE_TOKENS },
    { name: "Clay dark", palette: { ...clayLight, ...clayDarkChoice }, surfaces: SURFACE_TOKENS },
  ];
  for (const example of readExamples()) {
    for (const [mode, own, clay] of [
      ["light", example.light, clayLight],
      ["dark", example.dark, { ...clayLight, ...clayDarkChoice }],
    ] as const) {
      const palette = { ...clay, ...own };
      out.push({ name: `${example.key} ${mode}`, palette, surfaces: SURFACE_TOKENS });
      // A rule in the theme's CSS that repaints a part of the page, such as
      // Constellation's dark column, is judged on the surfaces it repaints.
      for (const m of example.css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
        const scoped = declarations(m[2]!);
        const surfaces = SURFACE_TOKENS.filter((token) => token in scoped);
        if (surfaces.length === 0) continue;
        out.push({ name: `${example.key} ${mode}, ${m[1]!.trim()}`, palette: { ...palette, ...scoped }, surfaces });
      }
    }
  }
  return out;
}

describe("text contrast", () => {
  it("reads both dark blocks and every shipped theme", () => {
    expect(clayDarkSystem).toEqual(clayDarkChoice);
    const names = cases().map((c) => c.name);
    expect(names).toEqual(expect.arrayContaining(["deep-tech light", "deep-tech dark", "constellation light", "constellation dark"]));
    expect(names.filter((name) => name.startsWith("constellation") && name.includes("[data-rail]"))).toHaveLength(2);
    for (const { palette } of cases()) {
      for (const token of [...TEXT_TOKENS, ...SURFACE_TOKENS]) expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  it("lowers the bar only for text tokens, each with a reason", () => {
    for (const [token, { ratio, reason }] of Object.entries(LOWER_BAR)) {
      expect(TEXT_TOKENS).toContain(token);
      expect([AA_LARGE_TEXT_OR_ICON, NO_BAR]).toContain(ratio);
      expect(reason).not.toBe("");
    }
  });

  describe.each(cases())("$name", ({ palette, surfaces }) => {
    const pairs = TEXT_TOKENS.flatMap((text) => surfaces.map((surface) => ({ text, surface })));
    it.each(pairs)("$text on $surface", ({ text, surface }) => {
      const needed = LOWER_BAR[text]?.ratio ?? AA_TEXT;
      const ratio = contrast(palette[text]!, palette[surface]!);
      expect(ratio, `${text} ${palette[text]} on ${surface} ${palette[surface]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(needed);
    });
  });
});

// Coloured words: links and the current item in the accent, errors and
// destructive menu items in danger, each on any surface and on its own tint.
const COLOURED_TEXT: Array<{ text: string; tint: string }> = [
  { text: "accent", tint: "accent-subtle" },
  { text: "danger", tint: "danger-subtle" },
];

// The fills a label is written on, with the token that writes it, hover included.
const FILLS: Array<{ text: string; fill: string }> = [
  { text: "on-primary", fill: "primary" },
  { text: "on-primary", fill: "primary-hover" },
  { text: "on-accent", fill: "accent" },
  { text: "on-accent", fill: "accent-hover" },
  { text: "on-danger", fill: "danger" },
  { text: "on-danger", fill: "danger-hover" },
];

describe("coloured text and labels on fills", () => {
  it("reads every token it judges as a colour", () => {
    for (const { palette } of cases()) {
      for (const token of [...COLOURED_TEXT.flatMap((c) => [c.text, c.tint]), ...FILLS.flatMap((f) => [f.text, f.fill])]) {
        expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
      }
    }
  });

  describe.each(cases())("$name", ({ palette, surfaces }) => {
    const pairs = [
      ...COLOURED_TEXT.flatMap(({ text, tint }) => [...surfaces, tint].map((background) => ({ text, background }))),
      ...FILLS.map(({ text, fill }) => ({ text, background: fill })),
    ];
    it.each(pairs)("$text on $background", ({ text, background }) => {
      const ratio = contrast(palette[text]!, palette[background]!);
      expect(ratio, `${text} ${palette[text]} on ${background} ${palette[background]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
    });
  });
});
