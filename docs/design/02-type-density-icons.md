# Type, density and icons

## Type

Inter for text, JetBrains Mono for keys, queries, commit ids and tokens. Both
bundled through `@fontsource`, weights 400, 500 and 600 (mono 400 and 500);
no font service at load time.

The root stays 14px so Tailwind's spacing stays in rem. The scale is seven
sizes, each with its line height, defined as theme tokens so `text-sm` means
one thing everywhere:

| class | size / line | used for |
|---|---|---|
| text-2xs | 11 / 16 | section titles in small capitals, tags, table heads |
| text-xs | 12 / 16 | meta beside a name, timestamps, counts in chips |
| text-sm | 13 / 20 | body: table cells, controls, labels, buttons |
| text-base | 14 / 22 | prose: descriptions, comments, dialogs |
| text-lg | 16 / 24 | page titles |
| text-xl | 20 / 28 | the one big number on a page, the profile name |
| text-2xl | 24 / 32 | reserved: the sign-in page |

Weights: 400 for text, 500 for labels and emphasis, 600 for titles. Every
number, date and key is `tabular-nums` (the body sets `tnum`), so columns
line up. A size typed by hand (`text-[13px]`) is a defect, and
`web/src/styles/ratchet.test.ts` refuses one anywhere outside the kit.

## Density

| thing | size |
|---|---|
| control sm / md / lg | 28 / 32 / 36 px |
| table row | 36 px |
| dense list row (panels, menus) | 32 px |
| sidebar item | 32 px |
| spacing grid | 4 px |
| radius control / overlay | 6 / 8 px |
| sidebar open / rail | 232 / 48 px |
| drawer | 704 px |
| docked issue panel, from 1440 px | 560 px |

Constants live in `web/src/config.ts` (`CONTROL_HEIGHT_*`, `ROW_HEIGHT_*`,
`SIDEBAR_WIDTH`, `SIDEBAR_RAIL_WIDTH`, `DRAWER_WIDTH`, `ISSUE_PANEL_WIDTH`,
`ISSUE_PANEL_DOCK_MIN_PX`). Three page widths, by
what a page is: `narrow` 44rem for forms and settings, `content` 72rem for
reading and lists, `wide` for canvases (board, plan, hierarchy, workflow,
dashboard).

## Icons

`web/src/components/icons.tsx`: about eighty glyphs drawn on a 16px grid with
a 1.5px stroke, round caps and joins, `currentColor`. An icon is decoration
beside text (`aria-hidden`) and gets a `label` when it stands alone, which
makes it `role="img"` with a title. A missing icon is drawn, not imported.

The set: home, search, plus, issue, board, sprint, plan, milestone, dashboard,
hierarchy, queue, desk, team, users, user, workflow, field, repository,
settings, key, tag, chevrons, check, x, more, edit, trash, archive, link,
unlink, attach, clock, calendar, comment, eye, eye-off, sun, moon, monitor,
external, filter, zoom in and out, warning, info, drag, collapse, expand,
command, bell, upload, mail, help.

Where icons go: sidebar items, icon-only buttons (always with a label),
menu items, empty states, toasts. Where they do not go: beside every button
label, inside table cells, on tags.
