# Feedback rules

What the interface says back, and when. These are rules, not suggestions;
a screen that breaks one is a defect.

## Loading

A `Skeleton` in the shape of what is coming: rows for a table, lines for
prose, a block for a canvas. It waits 150ms before showing so a fast load
never flashes. The word "Loading" is not shown. A page never renders `null`
while it waits.

## Empty

`EmptyState` with the noun, one sentence, and one action when there is
something to do: "No projects yet. A project holds issues, boards and a plan.
[New project]". This is where a first-time visitor reads what a page is for;
the page header has no room for it.

## Errors

`ErrorBanner` in place, next to what failed, with Retry when retrying is a
thing. A form's error sits above its fields; a field's error sits under the
field and replaces its hint. A route that throws is caught by the shell's
error component, so the sidebar survives and the reader can go elsewhere.
Every message is a sentence naming what to do.

## Destructive actions

Anything not undoable in one click goes through `ConfirmDialog`. The title is
the question ("Delete team?"), the body names the consequence in one or two
sentences ("Platform has 3 members. Its issues keep their assignees."), the
danger button reads verb plus noun ("Delete team"), and Cancel is first in
tab order after the body. The confirm button carries `data-action="confirm"`,
which the browser suite's `confirm(page)` helper presses.

Undoable things are done at once and toast with Undo: removing a dependency,
removing a widget while arranging, stopping watching.

## Success

A toast, six seconds, at most three on screen, Escape dismisses the newest.
A creation toasts with a link to what was made: "Created ALP-13 [Open]". A
move toasts the destination. A save that the reader can see happen (an inline
edit) toasts nothing. Errors toast only when the failing action has no place
of its own on the page.

## Focus and keyboard

Dialogs trap focus and return it. Menus and popovers return it on Escape and
on a pick. Segmented controls, tabs and menus move with the arrow keys. Every
icon-only button has a label. Nothing a test presses lives only inside a
closed menu.
