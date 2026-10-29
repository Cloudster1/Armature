// The colour tokens a theme may redefine, grouped the way the editor shows
// them. A test keeps this list equal to the --color-* names in the stylesheet,
// and the words for each in both catalogues.

/** A group of tokens; its name and each token's are in the looks catalogue, by these keys. */
export interface TokenGroup {
  id: string;
  tokens: Array<{ name: string }>;
}

export const TOKEN_GROUPS: TokenGroup[] = [
  {
    id: "Surfaces",
    tokens: [
      { name: "canvas" },
      { name: "surface" },
      { name: "surface-raised" },
      { name: "surface-sunken" },
      { name: "surface-overlay" },
      { name: "surface-glass" },
    ],
  },
  {
    id: "Backdrop",
    tokens: [{ name: "backdrop-from" }, { name: "backdrop-to" }],
  },
  {
    id: "Borders",
    tokens: [{ name: "border" }, { name: "border-strong" }],
  },
  {
    id: "Text",
    tokens: [{ name: "ink" }, { name: "ink-muted" }, { name: "ink-subtle" }, { name: "ink-disabled" }],
  },
  {
    id: "Actions",
    tokens: [{ name: "primary" }, { name: "primary-hover" }, { name: "on-primary" }],
  },
  {
    id: "Accent",
    tokens: [{ name: "accent" }, { name: "accent-hover" }, { name: "accent-subtle" }, { name: "on-accent" }, { name: "focus" }, { name: "selection" }],
  },
  {
    id: "Feedback",
    tokens: [
      { name: "danger" },
      { name: "danger-hover" },
      { name: "danger-subtle" },
      { name: "on-danger" },
      { name: "success" },
      { name: "success-subtle" },
      { name: "warning" },
      { name: "warning-subtle" },
      { name: "epic" },
    ],
  },
  {
    id: "Statuses",
    tokens: [
      { name: "status-todo" },
      { name: "status-progress" },
      { name: "status-done" },
      { name: "status-todo-subtle" },
      { name: "status-progress-subtle" },
      { name: "status-done-subtle" },
    ],
  },
  {
    id: "Charts",
    tokens: [{ name: "chart-1" }, { name: "chart-2" }, { name: "chart-3" }, { name: "chart-4" }, { name: "chart-5" }, { name: "chart-6" }],
  },
];

/** Every token name, flat, in the editor's order. */
export const TOKEN_NAMES: string[] = TOKEN_GROUPS.flatMap((group) => group.tokens.map((token) => token.name));

/** The pointers a theme may replace, with what each lands on. */
export const CURSOR_KINDS: Array<{ kind: string; keyword: string; selector: string }> = [
  { kind: "default", keyword: "default", selector: ":root, body" },
  {
    kind: "pointer",
    keyword: "pointer",
    selector: 'a, button, [role="button"], [role="tab"], [role="option"], label, select, summary, .cursor-pointer',
  },
  {
    kind: "text",
    keyword: "text",
    selector: 'input:not([type="checkbox"]):not([type="radio"]):not([type="file"]), textarea, [contenteditable="true"], .cursor-text',
  },
  { kind: "grab", keyword: "grab", selector: ".cursor-grab" },
  { kind: "grabbing", keyword: "grabbing", selector: ".cursor-grabbing" },
  { kind: "move", keyword: "move", selector: ".cursor-move" },
  { kind: "notAllowed", keyword: "not-allowed", selector: ':disabled, [aria-disabled="true"], .cursor-not-allowed' },
  { kind: "wait", keyword: "wait", selector: ".cursor-wait, .cursor-progress" },
];

/** The three elevations a theme may redraw. */
export const SHADOW_KEYS = ["1", "2", "3"] as const;
