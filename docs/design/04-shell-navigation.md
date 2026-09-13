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
|[<]| Acme Org            v  | Settings                                             |
|   |                        | Workflows                                            |
| ⌂ | WORK & PLAN         v  | [Workflows] Statuses  Schemes          <- glass strip|
| ⌕ |  o Home                |------------------------------------------------------|
| ⚑ |  o Search      Ctrl K  |                                                      |
| ▦ |  o Inbox               |  +------------------------------------------------+  |
| + |  o Projects            |  | Default software workflow [The organization's] |  |
|   |                        |  | Used by every project ...     STATUSES  SCHEMES|  |
|   | PROJECT             v  |  | To Do, In Progress ...        4 ====    all    |  |
|   |  [v Alpha         ALP] |  +------------------------------------------------+  |
|   |  o Issues   o Board    |     elevated glass cards with icon actions [✎][⧉][🗑] |
|   |  o Sprints  o Plan     |                                                      |
|   |  o Milestones ...      |                                                      |
|   |  PROJECT ADMIN      >  |                                                      |
|   |                        |                                                      |
|   | SETTINGS            v  |                                                      |
|   |  o Access  o Workflows |                                                      |
|   |  o Labels  o API tokens|                                                      |
| ? |                        |                                                      |
| ◐ |------------------------|                                                      |
|(A)| (AL) Ada     Owner [⏻] |                                                      |
+---+------------------------+------------------------------------------------------+

Rail only (below 1024px or by the toggle):  the sidebar is gone; the rail's
avatar becomes a menu with Your profile and Sign out.
```

## The rail

48px, `<aside data-rail>`. Its top is a `<header>` holding the toggle
(`data-action="sidebar"`, "Collapse the sidebar" / "Expand the sidebar").
Then Home, Search, the inbox bell (`data-action="inbox"`,
`data-unread-count`, `data-guide="inbox"`), Projects, and New issue
(`data-action="new-issue"`, `data-guide="new-issue"`), each an icon with its
word in a tooltip to the right. Its foot: the guide (`data-action="guide"`),
the theme (`data-action="theme"`, `data-guide="theme"`, sr-only text Auto,
Light or Dark), and the avatar, a link to the profile (`data-action="profile"`)
while the sidebar is open and a Menu holding Sign out (`data-action="sign-out"`)
when it is folded, so the way out exists exactly once either way.

## The sidebar

232px, `<nav data-sidebar="open">`, only while open; the state is
`localStorage armature.sidebar` (open or rail). The organization's name at the
top, a Menu when there is more than one. Then the groups, each a fold whose
title is the switch (`data-sidebar-group=<id>`, `data-open`, `aria-expanded`;
folds are kept in `localStorage armature.sidebar-groups`):

- **Work & Plan** (open): Home, Search (with the palette's shortcut shown),
  Inbox, Projects.
- **Project** (open): a switcher at the top, a menu with a filter field
  listing every project; switching keeps the page kind (Board stays Board).
  Below it the project's Work pages that its features include, then
  **Project Admin** (folded by default): Teams, Workflow, Fields,
  Components, Repositories, Automation, Import, Settings. With no project in
  the route the group shows the last visited project (`localStorage
  armature.project`) or a "Choose a project" button.
- **Settings** (open): Access, Workflows, Labels, Automation, Webhooks,
  Fields, Audit log, API tokens. The link to API tokens is what the browser
  suite waits for, so the group opens by default and the link stays a link.

At the bottom the person: avatar and name (a link to the profile), the role
they hold here, and Sign out (`data-action="sign-out"`).

## The strip and the backdrop

`main` wears the backdrop gradient (`bg-backdrop`). Its first child is the
strip (`[data-shell-header]`): sticky, glass (`bg-surface-glass` under a
backdrop blur), a hairline under it. `PageHeader` draws itself into the
strip through `ShellHeaderContext` and in place where there is no strip
(the portal, a shared page, every unit test), so pages keep their one
`PageHeader` call. The issue page's own sticky head is unchanged.

## Routes

- A nested `projectRoute` at `/projects/$projectKey` loads the project once,
  owns the crumb and the not-found state, and renders its children: index
  (Issues), board, sprints, plan, calendar, milestones, releases, dashboard,
  hierarchy, teams, workflows, fields, components, repositories, automation,
  import, queues, service-desk, settings.
- `/settings/workflows` is **Workflows**, the organization's library of
  workflows and schemes. `/projects/$key/workflows` is **Workflow scheme**;
  its sidebar label is **Workflow**.
- `/settings` is a hub: You (Profile, API tokens) and Organization (Access,
  Workflows, Labels, Automation, Webhooks, Fields, Audit log). `/settings/fields`
  is the organization's shared fields and `/settings/audit` the audit log. `/settings/automation` is the
  organization's rules and `/settings/webhooks` its endpoints. `/settings/profile` is the profile, with the
  notification settings below it; `/me` redirects.
- `/inbox` is what the person was told, newest first, unread or everything;
  a row is a link to its issue and opening it marks it read.
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
question mark button in the sidebar's foot (`data-action="guide"`) opens the
palette in ask mode (`data-palette-mode="ask"`): answers first, placeholder
"Ask where something is...", and "No answer for that yet" when there is none.

## Breadcrumbs

The sidebar's path minus the current page, rendered by `Breadcrumbs` inside
`PageHeader`: `Projects / Alpha` on a project page, `Projects / Alpha /
ALP-4` on an issue whose parent is ALP-4, `Settings` on a settings page. Home,
Projects and Search have none.
