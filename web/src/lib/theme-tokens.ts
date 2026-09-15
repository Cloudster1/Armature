// The colour tokens a theme may redefine, grouped the way the editor shows
// them. A test keeps this list equal to the --color-* names in the stylesheet.

export interface TokenGroup {
  title: string;
  tokens: Array<{ name: string; label: string }>;
}

export const TOKEN_GROUPS: TokenGroup[] = [
  {
    title: "Surfaces",
    tokens: [
      { name: "canvas", label: "Page background" },
      { name: "surface", label: "Cards, sidebar, tables" },
      { name: "surface-raised", label: "Hover rows, chips, wells" },
      { name: "surface-sunken", label: "Code, inset areas" },
      { name: "surface-overlay", label: "Menus, dialogs, toasts" },
      { name: "surface-glass", label: "Glass over the backdrop" },
    ],
  },
  {
    title: "Backdrop",
    tokens: [
      { name: "backdrop-from", label: "Gradient start" },
      { name: "backdrop-to", label: "Gradient end" },
    ],
  },
  {
    title: "Borders",
    tokens: [
      { name: "border", label: "Hairlines" },
      { name: "border-strong", label: "Controls at rest" },
    ],
  },
  {
    title: "Text",
    tokens: [
      { name: "ink", label: "Text" },
      { name: "ink-muted", label: "Secondary text, labels" },
      { name: "ink-subtle", label: "Meta, placeholders" },
      { name: "ink-disabled", label: "Disabled labels" },
    ],
  },
  {
    title: "Actions",
    tokens: [
      { name: "primary", label: "The one action" },
      { name: "primary-hover", label: "The one action, hovered" },
      { name: "on-primary", label: "Text on the one action" },
    ],
  },
  {
    title: "Accent",
    tokens: [
      { name: "accent", label: "Links, the current item" },
      { name: "accent-hover", label: "Links, hovered" },
      { name: "accent-subtle", label: "Selected background" },
      { name: "on-accent", label: "Text on the accent" },
      { name: "focus", label: "The focus ring" },
      { name: "selection", label: "Text selection, selected rows" },
    ],
  },
  {
    title: "Feedback",
    tokens: [
      { name: "danger", label: "Destructive" },
      { name: "danger-hover", label: "Destructive, hovered" },
      { name: "danger-subtle", label: "Destructive background" },
      { name: "success", label: "Done, saved" },
      { name: "success-subtle", label: "Done background" },
      { name: "warning", label: "In flight, capacity" },
      { name: "warning-subtle", label: "In flight background" },
      { name: "epic", label: "The levels above standard" },
    ],
  },
  {
    title: "Statuses",
    tokens: [
      { name: "status-todo", label: "To do" },
      { name: "status-progress", label: "In progress" },
      { name: "status-done", label: "Done" },
      { name: "status-todo-subtle", label: "To do card tint" },
      { name: "status-progress-subtle", label: "In progress card tint" },
      { name: "status-done-subtle", label: "Done card tint" },
    ],
  },
  {
    title: "Charts",
    tokens: [
      { name: "chart-1", label: "Series 1" },
      { name: "chart-2", label: "Series 2" },
      { name: "chart-3", label: "Series 3" },
      { name: "chart-4", label: "Series 4" },
      { name: "chart-5", label: "Series 5" },
      { name: "chart-6", label: "Series 6" },
    ],
  },
];

/** Every token name, flat, in the editor's order. */
export const TOKEN_NAMES: string[] = TOKEN_GROUPS.flatMap((group) => group.tokens.map((token) => token.name));

/** The pointers a theme may replace, with what each lands on. */
export const CURSOR_KINDS: Array<{ kind: string; label: string; keyword: string; selector: string }> = [
  { kind: "default", label: "Default", keyword: "default", selector: ":root, body" },
  { kind: "pointer", label: "Pointer", keyword: "pointer", selector: 'a, button, [role="button"], [role="tab"], [role="option"], label, select, summary, .cursor-pointer' },
  { kind: "text", label: "Text", keyword: "text", selector: 'input:not([type="checkbox"]):not([type="radio"]):not([type="file"]), textarea, [contenteditable="true"], .cursor-text' },
  { kind: "grab", label: "Grab", keyword: "grab", selector: ".cursor-grab" },
  { kind: "grabbing", label: "Grabbing", keyword: "grabbing", selector: ".cursor-grabbing" },
  { kind: "move", label: "Move", keyword: "move", selector: ".cursor-move" },
  { kind: "notAllowed", label: "Not allowed", keyword: "not-allowed", selector: ':disabled, [aria-disabled="true"], .cursor-not-allowed' },
  { kind: "wait", label: "Wait", keyword: "wait", selector: ".cursor-wait, .cursor-progress" },
];

/** The three elevations a theme may redraw. */
export const SHADOW_KEYS = ["1", "2", "3"] as const;
