# The kit

`web/src/components/ui/`, one file per component, a barrel at `index.ts`.
Every name the first kit exported is still exported, so callers keep
`import { ... } from "@/components/ui"`.

| component | props | behaviour |
|---|---|---|
| Button | variant primary, secondary, ghost, danger; size sm, md, lg; loading; icon; iconRight | aria-busy while loading, label never swapped |
| IconButton | icon, label (required), variant, size | label is the name and the title |
| Input, Textarea | controlSize, invalid | the shared control look at one of three heights |
| Checkbox | label | one click target with its words |
| Switch | checked, onChange, label | role switch, aria-checked, Space toggles |
| Select | label, hint, error | id `field-<label slug>`, described by hint or error |
| Field | label, hint, error, rows | id `field-<label slug>`; rows makes it a textarea |
| Labelled | id, label, hint, error | the label and messages around any control |
| Segmented | label, value, options, onChange, size | role group, aria-pressed, arrows move |
| Tabs, TabPanel | label, value, tabs, onChange | tablist and tabs, arrows and Home and End activate |
| Menu | trigger, items, label, align | role menu, arrows, Home, End, typeahead, Escape returns focus |
| Popover | open, onClose, trigger, label, align | anchored, not modal, Escape and outside press close, focus in and back |
| Tooltip | text, side | shows after 400ms on hover or focus, role tooltip, never the only label |
| MentionTextarea | value, onValueChange, people | a Textarea that lists `[data-mention-option=<name>]` after an at sign; arrows walk, Enter or Tab takes, Escape closes; inserts `@Full Name` as plain text |
| Spotlight | target, text, onDone, onMissing | a callout portalled to body beside `[data-guide=<target>]` with a ring around it; waits for the element after a navigation, Escape or Got it closes, onMissing when it never shows |
| Dialog | open, onClose, title, description, footer, size | modal, focus trap, Escape, scroll lock, portal, focus returns |
| ConfirmDialog | noun, verb, body, onConfirm, loading, error | title "Verb noun?", button "Verb noun" with data-action confirm |
| Drawer | open, onClose, title, width, actions | a dialog from the right, the page stays in view |
| DockedPanel | open, onClose, title, width, actions | a column beside the page: no scrim, no trap, Escape closes unless typing |
| ToastProvider, useToast | success, error, info (message, link, action) | polite status, assertive alert, six seconds, three at most, Escape dismisses the newest |
| Breadcrumbs | crumbs | nav labelled Breadcrumb, the last item is the page and is not rendered |
| PageHeader | crumbs, title, meta, actions, tabs | a header element |
| Page | width narrow, content, wide | the one place widths are decided |
| ButtonLink | href, download, variant, size, icon | a link drawn as a button, for a file the browser handles itself |
| Columns, Donut, Legend, LineChart (features/dashboard/charts.tsx and ChartBody.tsx) | groups or series, a tone per label | hand-drawn; HTML columns with a 4px cap and a surface gap between segments, an SVG ring with the total in the hole, a legend from two series, a table under every chart |
| Toolbar | start, end, label | role toolbar, wraps |
| Table, Th, Td | dense, sticky | tabular numbers, sticky heads |
| Card, Tag, SectionTitle, EmptyState, ErrorBanner | as before; EmptyState takes an icon, ErrorBanner an onRetry | |
| Skeleton | lines or rows | waits 150ms, then grey in the shape of what is coming |
| Avatar | name, src, size xs to lg | picture when there is one, initials when not or when it breaks |

## Rules

- Nothing a browser test presses may live only inside a closed menu or
  popover; a flow that gains a step gains a named helper in the suite.
- Overlays are portaled last in the DOM and close on Escape. Menus and
  popovers also close on a press outside. Dialogs trap focus; popovers do not.
- Every interactive component has a test for focus, keyboard and aria in
  `controls.test.tsx` and `overlays.test.tsx`.
- Ad hoc controls are defects: `<button>` outside the kit, an `<input>` with a
  hand-written class, a `<select>` without `Select` or `control()`.

## Filters, choices and the search field

- **Chip**: a filter that is on or off, `aria-pressed`, round. The plan's type
  filters, a board's grouping, a backlog's team.
- **Choice** and **OptionCard**: one of a radiogroup (`role="radio"`,
  `aria-checked`) or, without `checked`, a card that acts. Board type, project
  template, request type, the widgets a dashboard can add, a label's colour.
- **SelectInput**: the select without a label, named by `aria-label`, for a
  toolbar or a table cell. `Select` composes it.
- **Button variant="link"**: text in a sentence, no height and no padding, for
  "Remove", "Sign in with single sign-on", an example query.
- **IconButton size="xs"**: 20px, for a tree's collapse toggle or a chip's x.
- **isInteractiveTarget(target)**: whether a click landed on a link, a button
  or a control of its own, so a row around it does not act as well. Beside
  `isEditing`; the issue table and the project's workflow rows share it.
- The search field is `lg`, and its help is a Popover behind an Info button
  (`data-action="query-help"`); recent queries are offered under it
  (`data-recent-queries`).

## The editor

- **Editor** (`features/editor/Editor.tsx`): one document, two faces. Rich
  is TipTap over ProseMirror, editing the stored document in place with a
  toolbar (Bold, Italic, Code, Heading, Bullet list, Numbered list, Quote,
  Code block, Link; `data-editor-action`, `aria-pressed`) and the markdown
  shortcuts typed (`## `, `- `, `**x**`). Markdown is a Textarea over the
  text the document converts to. A Segmented "Editor mode" switches
  (`data-editor-mode="rich|markdown"`, kept in `localStorage armature.editor`,
  every editor on the page follows). The `id` names whichever is editable:
  the ProseMirror div (`data-editor="rich"`, `role="textbox"`) or the
  textarea (`data-editor="markdown"`). `@` offers people in either face
  (`data-mention-list`, `data-mention-option`); Ctrl+Enter submits. The form
  outside reaches in through `handle` (insertMarkdown, clear), never a ref.
- **DocView** (`features/editor/DocView.tsx`): a document drawn as elements,
  never HTML: `p`, `h2`-`h4`, lists, `blockquote`, `pre > code`, `br`,
  `strong`, `em`, `code`, `s`, `a` for a web or mail address only, a mention
  as `[data-mention=<id>]`. An unknown node shows its text.
- **markdown.ts**: `docToMarkdown` and `markdownToDoc` over exactly the
  schema's node set, round-trip safe; prose that would read as markup is
  escaped, and nothing is ever parsed as HTML.

## Cards, figures and the shell's strip

- **ProgressBar**: `{ value, max?, segments?: [{ value, tone }], label, size?:
  "xs" | "sm" }`, `role="progressbar"` with its aria values; segments wear
  the status colours. The milestone and hierarchy bars are adapters over it.
- **Stat**: a small figure with its word over it and, given `share`, a thin
  bar under it. The column of them on a card.
- **Card `elevated`**: glass over the backdrop (`bg-surface-glass`, the
  third shadow, a backdrop blur) for a card in a list of cards. **Card
  `actions`**: icon buttons in the top right corner (`[data-card-actions]`),
  labelled "Edit <name>", "Duplicate <name>", "Delete <name>" with
  `data-action="edit" | "duplicate" | "delete"`.
- **PageHeader** draws into the shell's strip through `ShellHeaderContext`
  when one is provided, in place otherwise. The kit stays router-free: a
  tab strip that navigates passes `value` from the pathname and navigates in
  `onChange`.
