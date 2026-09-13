# Interface v2: principles

What the redesign is for, and the rules every screen follows. The other
documents in this folder carry the tokens, the kit, the shell, the pages and
the feedback rules; this one says why.

## What stays

The interface is a tool, not a landing page. Tables where there are columns.
One accent for what is current; primary actions in ink. No sentence under a
page title; the empty state is where a first-time visitor reads. Navigation in
the sidebar, one column of chrome, the content has the rest. Nothing installed
on the host; hand-drawn SVG rather than libraries, with the one exception the
workflow canvas makes for React Flow.

## The rules

1. **One kit.** Every control, overlay and message comes from
   `web/src/components/ui`. A raw button or a hand-drawn input is a defect.
2. **One scale.** Seven text sizes, three control heights, two radii, a 4px
   grid. A size typed by hand is a defect; a test counts them.
3. **Where am I.** Every page under the shell has breadcrumbs that are the
   sidebar's path minus the page itself. A project's pages are always in the
   sidebar, with a switcher to change project.
4. **Nothing is lost on one click.** A deletion asks, with the noun on the
   button. An undoable act is done at once and offers Undo in a toast.
5. **Something happened.** A creation toasts with a link to what was made. A
   move toasts. A save toasts only when the reader could not otherwise tell.
6. **Never blank.** Loading is a skeleton in the shape of what is coming. Empty
   is a noun, a sentence and one action. An error is in place, with Retry.
7. **The keyboard works.** Ctrl+K reaches anything. Menus, dialogs, tabs and
   segmented controls follow the ARIA patterns. Focus goes in and comes back.
8. **Dates are the reader's.** Times are shown in the reader's time zone and
   language, from their profile, not the browser's guess.
9. **The tests are the contract.** Field ids, button texts, aria labels and
   data attributes the browser suite reads are kept; a flow that gains a step
   gains one helper.
