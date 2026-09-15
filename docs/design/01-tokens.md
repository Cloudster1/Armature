# Tokens: the Clay identity

Warm stone neutrals kept from the first version, a cobalt accent, and a face
of its own in Inter and JetBrains Mono. The dark theme's neutrals lean to
slate rather than stone, a deep grey-blue under the same accent, because a
dark warm brown reads as a stain where a dark slate reads as a night. Blue is the one hue no status, level
or type uses, so "current" and "link" never collide with "done", "in
progress", "epic" or "bug". The values live in `web/src/styles/index.css`.

| token | light | dark | role |
|---|---|---|---|
| canvas | #F6F4F0 | #14171C | page background |
| surface | #FDFCFA | #1A1E24 | cards, sidebar, tables |
| surface-raised | #F1EEE8 | #222731 | hover rows, chips, wells |
| surface-sunken | #ECE8E1 | #0F1216 | code, inset areas, the meter |
| surface-overlay | #FFFFFF | #262C36 | menus, dialogs, drawer, toasts |
| surface-glass | rgb(253 252 250 / .72) | rgb(26 30 36 / .6) | what floats over the backdrop: elevated cards, the page's head; always under a backdrop blur |
| backdrop-from / backdrop-to | #FAF8F4 / #EBE6DE | #1B1F26 / #0F1216 | the radial gradient behind the content (`bg-backdrop`) |
| border | #E4DFD6 | #2B313B | hairlines |
| border-strong | #C9C2B6 | #3F4652 | controls at rest |
| ink | #1F1B16 | #EDE8E0 | text |
| ink-muted | #625B51 | #ABA396 | secondary text, labels |
| ink-subtle | #8F877B | #7D7569 | meta, placeholders |
| ink-disabled | #B5AEA3 | #5A544B | disabled labels |
| primary / primary-hover / on-primary | #1F1B16 / #37312A / #FBFAF7 | #EDE8E0 / #DBD5CB / #171512 | the one action |
| accent / accent-hover | #2F5FD0 / #264DAD | #7FA3F2 / #9BB7F5 | links, the current item |
| accent-subtle / on-accent | #E6ECFA / #FFFFFF | #22304F / #0F1A33 | selected background |
| focus | #2F5FD0 | #9BB7F5 | the focus ring, nothing else |
| selection | #DCE5F8 | #2A3A5E | text selection, selected rows |
| danger / danger-hover / danger-subtle | #B93A2E / #9C2F25 / #F9E7E4 | #E4756A / #EC8F86 / #3E2220 | destructive |
| success / success-subtle | #2E7D4F / #E3F1E8 | #6CC08F / #1E3527 | done, saved |
| warning / warning-subtle | #B7791F / #FBF0DA | #E0A94A / #3D2F16 | in flight, capacity |
| epic | #7A4FB5 | #B392E6 | the levels above standard |
| status-todo / status-progress / status-done | #8F877B / #B7791F / #2E7D4F | #7D7569 / #E0A94A / #6CC08F | the learned colours |
| status-todo-subtle / status-progress-subtle / status-done-subtle | #F0EDE7 / #FBF0DA / #E3F1E8 | #2B2721 / #3D2F16 / #1E3527 | the tint of a workflow card |
| chart-1 to chart-6 | #2F5FD0 #2E7D4F #7A4FB5 #B7791F #0F9A9A #B93A2E | #5B86E6 #4AA66E #9A7AD6 #B8862A #1FA3A3 #D4574A | series, in order: blue, green, purple, amber, teal, red; neighbours stay apart under protan and deutan vision, checked by a validator, not by eye |
| shadow-1 | 0 1px 2px rgb(31 27 22 / .06) | 0 1px 2px #0008 | raised |
| shadow-2 | 0 8px 24px rgb(31 27 22 / .12) | 0 12px 32px #000a | overlays |
| shadow-3 | 0 16px 40px rgb(31 27 22 / .10) + a top hairline of light | 0 16px 40px #0008 + the same | elevated glass cards |
| radius-control / radius-overlay | 6px / 8px | same | controls / menus, dialogs, cards |

The accent's old name, `brand`, is gone; a test in `web/src/styles` refuses it
outside the kit, along with sizes typed in pixels.

The names are also what a custom theme writes to: a theme keeps a light and a
dark set of overrides on any of them, and the editor lists them from
`web/src/lib/theme-tokens.ts`, which a test keeps equal to the `--color-*`
names in the stylesheet. Renaming a token is therefore a change to what saved
themes mean; add a name rather than rename one.

## How to use them

- Backgrounds: `canvas` for the page, `surface` for anything that holds
  content, `surface-raised` for hover and for chips, `surface-overlay` for
  anything that floats.
- Text: `ink` for what is read, `ink-muted` for labels and secondary facts,
  `ink-subtle` for meta and placeholders. Never colour body text with the
  accent.
- The accent marks the current item, a selected option, a link. A button is
  `primary` (ink), `secondary` (bordered), `ghost` or `danger`; never accent.
- Status colour is only ever the three status tokens, and only on statuses.
- Charts take the six series tokens in order; a legend names them.
