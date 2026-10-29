# The shell and navigation

Two columns of chrome on the left; the content has the rest. The rail is
what is always there: the places every page reaches from, and the person at
the bottom. The sidebar beside it is the map: where you are is highlighted,
where you can go is listed in groups that fold, and a project's pages are
always there once a project is chosen. Over the content floats the page's
head, breadcrumb, title and tabs, on glass; behind the content a soft
gradient.

```
+---+------------------------+------------------------------------------------------+
|[<]| (m) Acme               | Admin                               <- crumbs        |
|   |                        | Workflows                                            |
| ⌂ | WORK & PLAN         v  | [Workflows] Statuses  Schemes          <- glass strip|
| ⌕ |  o Home                |------------------------------------------------------|
| ⚑ |  o Search      Ctrl K  |                                                      |
| ▭ |  o Notifications   (3) |  +------------------------------------------------+  |
| + |  o Saved searches      |  | Default software workflow [In the default]     |  |
|   |  o Projects            |  | Used by every project ...     STATUSES  SCHEMES|  |
|   |                        |  | To Do, In Progress ...        4 ====    all    |  |
|   | PROJECT             v  |  +------------------------------------------------+  |
|   |  [v Alpha         ALP] |     elevated glass cards with icon actions [✎][⧉][🗑] |
|   |  o Issues   o Board    |                                                      |
|   |  o Sprints  o Plan     |                                                      |
|   |  o Milestones ...      |                                                      |
|   |  o Settings            |                                                      |
|   |                        |                                                      |
|   |  o Admin               |                                                      |
| ? |                        |                                                      |
| ◐ |                        |                                                      |
|(A)|            build stamp |                                                      |
+---+------------------------+------------------------------------------------------+

Below 768px:  the rail and the sidebar are gone; a 48px strip holds the menu
button, the installation's name and the bell, and the menu button opens the
same groups as a drawer.
```

## The rail

48px, `<aside data-rail>`. Its first child is the navigation (`<nav>`,
"Everywhere"), whose top is a `div` (`[data-rail-top]`) holding the toggle
(`data-action="sidebar"`, "Collapse the sidebar" / "Expand the sidebar"). Then
Home, Search, the bell (`data-action="notifications"`, `data-unread-count`,
`data-guide="inbox"`), Projects as a folder, as in Stator and Exciter, and New
issue (`data-action="new-issue"`, `data-guide="new-issue"`), each an icon with
its word in a tooltip to the right. Its foot: the guide (`data-action="guide"`),
the theme (`data-action="theme"`, `data-guide="theme"`, sr-only text Auto,
Light or Dark), and the avatar (`data-action="account"`), a round button whose
menu opens to the right of the rail: a header with the avatar beside the name,
address and role, the person's own pages, "Switch to" the partner tools, Admin
for an administrator, and Sign out (`data-action="sign-out"`), which lands on
the sign-in page however the request ends. That menu is the one way out.

## The sidebar

232px, `<nav data-sidebar="open">`, only while open; the state is
`localStorage armature.sidebar` (open or rail), and below 1024px it starts as
the rail alone until the toggle opens it. Its head is the mark and the
installation's name (`[data-installation-name]`), plain text; its foot only
the shared build stamp, with no person and no Sign out. Then the groups, each
a fold whose title is the switch (`data-sidebar-group=<id>`, `data-open`,
`aria-expanded`; folds are kept in `localStorage armature.sidebar-groups`):

- **Work & Plan** (open): Home, Search (with the palette's key, "Ctrl K", or
  "Cmd K" on a Mac, from the catalogue), Notifications (with the unread
  `Count`), Saved searches, Projects.
- **Project** (open): a switcher at the top, a menu with a filter field
  listing every project; switching keeps the page kind (Board stays Board)
  and drops the ids of the old project (a sprint's board becomes Sprints).
  Below it the project's Work pages that its features include, then one
  **Settings** entry for every member of the project, which opens the
  project's settings: one tab per page, in the suite's sections (General;
  Issue types, Fields, Issue view; Workflow; Components, Repositories,
  Automation; Import), read only for somebody who does not administer the
  project. Teams stay a Work page. With no project in
  the route the group shows the last visited project (`localStorage
  armature.project`) or a "Choose a project" button.
- **Admin**, for an administrator only: one entry that opens `/admin`.

In the settings area (`/settings` and below) the sidebar is the settings' own:
"Back to Armature" and the person's pages.

Below 768px there is no rail and no sidebar: the strip over the content holds
the menu button (`data-action="open-drawer"`), the installation's name and the
bell, and the button opens the same groups as a modal drawer from the left
(`[data-sidebar="drawer"]`), which a followed link closes.

## The strip and the backdrop

`main` wears the backdrop gradient (`bg-backdrop`). Its first child is the
strip (`[data-shell-header]`): sticky, glass (`bg-surface-glass` under a
backdrop blur), a hairline under it. `PageHeader` draws itself into the
strip through `ShellHeaderContext` and in place where there is no strip
(a shared page, every unit test), so pages keep their one `PageHeader` call.
The shell hands every `PageHeader` its breadcrumbs through `PageCrumbsContext`;
no page names its own. The issue page's own sticky head is unchanged.

A path that leads nowhere, and a page that throws, stay inside the shell for
somebody signed in: a page header, an `EmptyState` and the way home or Try
again (`[data-not-found]`, `[data-route-error]`), as Stator's and Exciter's
`routes/root.tsx`. Somebody not signed in sees the plain not-found page.

## Routes

- A nested `projectRoute` at `/projects/$projectKey` loads the project once,
  owns the crumb and the not-found state, and renders its children: index
  (Issues), board, sprints, plan, calendar, milestones, releases, dashboard,
  hierarchy, teams, queues, service-desk and settings. The settings route
  holds the tabs: `/settings` (General), `/settings/issue-types`, `fields`,
  `issue-view`, `workflows`, `components`, `repositories`, `automation` and
  `import`; each page's old address under the project redirects to its tab.
- `/admin/workflows` is **Workflows**, the installation's library of
  workflows and schemes. `/projects/$key/workflows` is **Workflow scheme**;
  its sidebar label is **Workflow**.
- `/settings` is a hub of the person's own pages: Profile, Notifications,
  Watching, Themes and API tokens. `/settings/profile` is the profile, with
  the notification settings below it; `/me` redirects.
- `/admin` is the Admin page, for administrators only (#462). Its index
  groups the installation's pages as cards under People and access (Installation,
  Access, Users), Work (Holidays, Workflows, Issue types, Project templates, Labels, Automation,
  Fields, Issue view), Integrations (Connected apps, Webhooks) and Records (Audit log); a list
  of the same on the left, or a menu on a phone, moves between them. The work
  sidebar stays, with one Admin entry. A member who opens it is told only
  administrators can, and no child page is mounted. Every old
  `/settings/<page>` address of those pages redirects to `/admin/<page>`,
  search and hash kept; `/settings/organization` lands on `/admin/installation`.
- `/notifications` is the Notifications page: what the person was told,
  newest first, unread or everything; a row is a link to its issue and
  opening it marks it read. `/inbox`, its old address, redirects there.
- `/filters` lists every saved search the person may see. `/search` takes `f`
  beside `q` to say which saved search the query came from.
- Creating a project navigates to it. Creating an issue toasts with a link,
  and the dialog offers "Create and open".

## The command palette

Ctrl or Cmd+K anywhere in the shell. A dialog over a listbox: issues (a key
jumps straight; words search the summaries and show the first eight),
projects, the current project's pages, settings pages, and actions (New
issue, Switch theme, Collapse sidebar, Search with a query). Arrows, Enter,
Escape. Typing a key and Enter is the fastest path in the product.

A question (two words or more, or ending in "?") adds an Answers group from
the guide (`web/src/features/guide`): pages, actions and query intents scored
by the words they share with the question, above a threshold or not at all.
An answer navigates and, when the entry names a `data-guide` target on that
page, a `Spotlight` callout points at it with the entry's sentence. The
question mark button in the rail's foot (`data-action="guide"`) opens the
palette in ask mode (`data-palette-mode="ask"`): answers first, placeholder
"Ask where something is...", and "No answer for that yet" when there is none.

## Breadcrumbs

Derived from the places, the sidebar's path minus the current page, by
`crumbsFor` in `features/shell/Crumbs.tsx`, and drawn by `PageHeader` in the
sticky strip, as Exciter's `Crumbs`: `Projects` on a project's issues,
`Projects / Alpha` on its other pages and on an issue, `Projects / Alpha /
Project settings` on a settings tab, `Settings` below the person's pages,
`Admin` below the installation's, `Admin / Workflows` deeper still. Home,
Projects, Search, Notifications, the settings hub and the Admin page have none.
An issue's parents are its own trail (`nav[aria-label="Parent issues"]`).
