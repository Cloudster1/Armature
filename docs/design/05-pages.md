# Page concepts

Wireframes for the pages the redesign changed, not an inventory of the product:
the routes are what the router says. Widths: `narrow` for forms and settings,
`content` for reading and lists, `wide` for canvases.

Pages with no wireframe here, because the redesign left them as they were:
`/hierarchy`, `/teams`, `/sprints`, `/sprints/$sprintId/board`, `/queues`,
`/repositories`, a project's `/fields`, `/settings/labels`, `/settings/access`,
`/unwatch`, `/login` and `/signup`, and the workflow designer's own routes
under `/settings/workflows`.

## Home (content)

```
Good morning, Ada                                      12 open assigned to you
ASSIGNED TO YOU                                          RECENTLY VIEWED
| T ALP-12  Fix the login loop     In progress  (TK) 2h |  ALP-9  Portal performance
| B ALP-14  Cache invalidation     To do        (TK) 1d |  BET-1  Onboarding
| ...                            Showing 15 of 27 · All |  YOUR PROJECTS
                                                          Alpha  ALP   12 open
                                                          Beta   BET    3 open
```

## Projects (content)

```
Projects                                          [Show archived] [New project]
| Name          Key  Kind      Lead        Open / All   Updated      ... |
| Alpha         ALP  Software  A. Lovelace  12 / 40     2h ago        ⋯  |
| Helpdesk      HLP  Service   G. Hopper     3 / 9      1d ago        ⋯  |
row menu: Open, Settings, Archive (confirm)
```

## Project issues (content)

```
Projects / Alpha
Alpha  ALP                                                    [New issue]
[Open|All|Mine]  [⌕ Search summaries         Words|Query]  [Filters v 1]  42 issues
                                                              Open in search
| T  ALP-12  Fix the login loop        In progress  (TK)  ▮▮▯  2h    [>] |
| B  ALP-14  Cache invalidation        To do        ( ? )  ▮▯▯  1d    [>] |
Filters popover: Label [select]  Milestone [select]  Type [chips]  Assignee [select]
[>] opens the issue as a drawer; the key stays a link to the page.
```

## Project workflows (content)

```
Projects / Alpha
Workflow scheme
[ This project follows the organization. ... [Choose a scheme v] [Use this scheme] ]
+ Bug triage                        2 statuses, 1 transition  Open in the designer +
| Bug issues follow this workflow here.                                          |
|   [START]                                                                       |
|   [o To Do] --Fixed--> [o Done]                                                 |
+---------------------------------------------------------------------------------+
| Issue type | Workflow                 | Decided by                    | Decide                   |
| > Bug      | Bug triage               | This project  Mapped scheme.. | [Bug triage           v] |
|   Task     | Default software workflow| Organization  ...catches...   | [Follow the organization v]|
```

The drawing is the designer's canvas in read-only mode: the same cards
(tinted by category, a round badge with the category's glyph, the name over
the description or the category's word, `[data-workflow-subtitle]`), the same
arrows with their names in pills (`[data-workflow-edge-label=<name>]`), the
same stored positions, nothing to grab. It follows the selected row
(`data-assignment-shown`), the first by default; a click anywhere on a row
that is not a link or a control selects it. Organization administrators get a
link into the designer; nobody draws here.

The Decide column appears for anybody with `project.administer` on the
project: `select[aria-label="Workflow for <type>"]` in its own cell
(`data-assignment-control`), listing the organization's workflows plus
"Follow the organization". A change is saved at once and the table redraws
from the response. The whole-scheme picker (`#project-scheme`) stays as the
organization administrator's secondary control.

## Workflow library (content)

```
Settings
Workflows
[Workflows] Statuses  Schemes                                  <- in the strip
                                                        [New workflow]
+------------------------------------------------------------------+
| Default software workflow [The organization's]      STATUSES  TRANSITIONS  SCHEMES   [✎][⧉]
| Used by every project that has not named one of its own.   4 ===  7          all
| To Do, In Progress, In Review and Done, with a review loop.
+------------------------------------------------------------------+
```

`/settings/workflows` is a layout with three tabs (`[data-workflow-tab=
"Workflows"|"Statuses"|"Schemes"]`) over `/settings/workflows`,
`/settings/workflows/statuses` and `/settings/workflows/schemes`. Every row
is an elevated Card. A workflow (`[data-workflow-card=<name>]`): name, the
organization's Tag, "Used by ...", its description, Stats for statuses (a
share of the organization's), transitions and schemes, and icon actions
"Edit <name>", "Duplicate <name>" (the copy endpoint) and, when nothing
uses it, "Delete <name>" (`[data-action="edit"|"duplicate"|"delete"]`). A
scheme (`[data-scheme-card=<name>]`): name, Tag, "Used by CP, HELP.", what
each type uses, Stats for projects and types, Edit and Delete icons, and
"Make it the organization's" as a text button; "New scheme" opens the
SchemeEditor inline. A status (`[data-status-card=<name>]`): its badge and
description; "New status" opens the designer's NewStatusForm inline with
"Create status". No status edit or delete: there is no endpoint.

## Issue page (content), docked panel and drawer

```
Projects / Alpha / ALP-4                                             sticky
[T] ALP-12  Fix the login loop [pencil]      [Start progress] [Done v] [⋯]
--------------------------------------------------------------------------
Description | Activity | Attachments | ... | Watchers | History      rail
                                                     | PEOPLE               v |
DESCRIPTION                                   [Edit] | Assignee   (AL) Ada     |
Why it matters ...                                   | Reporter   (AM) Anna    |
                                                     | Watchers   3    [Watch] |
ACTIVITY                                             | PLANNING             v |
 (AL) Ada     "Looks like the cookie is dropped" 1h  | Sprint     Sprint 4     |
 ... HISTORY is the last section, oldest first ...   | Milestone  R1           |
 [ Add a comment ...                    ] [x] internal| Scheduled  2 Sep - 8 Sep|
                                                     | Estimate   5            |
ATTACHMENTS (1)                        [Attach file] | TRACKING             v |
DEVELOPMENT   branch fix/login-loop   PR #12 open    | Priority   ▮▮▯ High     |
CHILDREN (2)  ▮▮▮▮▯ 1/2 done            [Add child]  | Time       1d est 3h    |
LINKS         blocks ALP-13                          | Team       Platform     |
                                                     | FIELDS               v |
                                                     | Cost       1200         |
                                                     | Created 3d · Resolved   |
```

The summary is a click-to-edit input (aria-label "Summary", Enter saves,
Escape cancels).

```
+----------+-----------------------------------+------------------------+
| sidebar  | Issues     [Open|All|Mine] [Filt] | ALP-12       [^] [v] x |
|          | > ALP-12 Fix login   Bug    [>]   | ALP / ALP-4            |
|          |   ALP-13 New icons   Task   [>]   | [T] ALP-12  In progress|
|          |   ALP-14 Tidy CSS    Task   [>]   | Fix the login loop     |
|          |   (list scrolls)                  | Description | Activity |
+----------+-----------------------------------+------------------------+
```

The description and the comment box are the Editor (see 03-components): a
toolbar and the document edited in place, or a markdown textarea, by the
Rich | Markdown switch; a saved description and every comment are drawn by
DocView with headings, lists, quotes, code and mentions. The create dialog's
description and the child form stay plain boxes that read markdown. The
portal shows descriptions and replies through DocView and keeps plain boxes
for writing.

The same page opens beside a list or a board. From 1440px up it is a docked
column (`ISSUE_PANEL_WIDTH` 560px, `DockedPanel`): no scrim, no focus trap,
`main` narrows to what is left, and the row or card that is open is marked
(`data-selected`, `aria-selected` on rows). A click on a row or card, away
from its links and buttons, opens or swaps; Up and Down from the page, or
`[^]`/`[v]` in the panel's head (`data-action="panel-prev|panel-next"`), walk
the list the page registered in its reading order. Below 1440px the same
click opens the 704px drawer over the page. Escape closes either unless a
field has focus; following a link inside closes it.

## Board (wide)

```
Projects / Alpha
Platform board  Kanban  [Platform board v]   [Filters v]      [Configure v]
| TO DO 4           | IN PROGRESS 2    | REVIEW 1     | DONE 12        |
| [T ALP-14 ...   ] | [B ALP-12 ...  ] | [S ALP-9   ] | [T ALP-1     ] |
A move toasts: "ALP-12 moved to Review".
```

## Plan (wide)

```
Projects / Alpha
Plan  [Timeline | Sprints | Dependencies]
[⌕ Narrow with a query          ?]  [Filters v 2]  [Zoom: Fit|Weeks|Months|Quarters]
42 issues · 30 scheduled · 60% done · 88 pts                          [v more]
+-- issue column ---------+-- calendar --------------------------------------+
Filters popover: Type chips with counts | Milestone | Team | Hide done for [14] days
"?" opens the query help popover, the same content as the Search page's.
"more" opens the per-type breakdown. A row with a warning wears it: red
(bg-danger-subtle) for a contradiction, yellow (bg-warning-subtle) for an
omission, a warning icon beside the key, the words in the row's and the bar's
title; the list under the plan keeps everything, sprint and team warnings
included. A pointed-at or clicked dependency arrow shows an IconButton with a
cross on the arrow itself, which removes the dependency; the graph's edges do
the same.
```

## Milestones (content)

```
Projects / Alpha
Milestones
[ Name [________] Due [2026-09-24] What it means [____________]  [Add milestone] ]
+ Release 1   Due 24 Sep 2026            [Show issues] [Dashboard] [Edit] [Close milestone] [Delete] +
| ▮▮▮▮▮▮▯▯▯▯▯▯  3 of 8 done, 37%                                                                   |
| 3 done · 2 in progress · 3 to do                                                                  |
+---------------------------------------------------------------------------------------------------+
CLOSED
| Release 0   Closed 2 Aug 2026 ...                                                                  |
```

Each card (`data-milestone=<name>`) carries the counted progress
(`data-milestone-progress`). Dashboard (`data-action="milestone-dashboard"`)
opens a dialog (`data-milestone-dashboard=<name>`) with the name prefilled and
a template select, default Milestone, and lands on the new dashboard.

## Dashboard (content)

```
Projects / Alpha
Overview                                              [New dashboard] [Arrange]
[Overview | Release]
+ FILTER  (B) Bug (T) Task (S) Story          2 filters --------------------------+
| Team [Any team v]  Assignee [Anyone v]  Since [Last 30 days v]  Counted by [..v] |
| Status [Any|To do|In progress|Done]  Narrow with a query [labels = urgent    ]    |
|                                                  [Clear filter] [Save as default]|
+---------------------------------------+-----------------------------------------+
| Status                    12 in all   | Velocity                                |
| In progress  ▮▮▮▮▮▮▮▯▯  7             |  Sprint 3  ▮▮▮▮▮▮ 21 / 18               |
| To do        ▮▮▮▮▯▯▯▯▯  5             |  Not narrowed by the filter: counts     |
+---------------------------------------+  sprints, not issues.                   |
```

Two columns of widgets, a filter tile across the top when the dashboard has
one. The open dashboard is named in the address (`d`, its id) so a link opens
that one. New dashboard is a name and a Start from chooser: Blank, the built-ins
that suit the project (Overview, Milestone, Team health, Delivery), then the
organization's own, each a card (`data-dashboard-template=<key|id>`) with a
Remove beside the saved ones. While arranging, "Save as a template"
(`data-action="save-template"`) keeps the arrangement under a name
(`#field-template-name`). A chart widget draws issues grouped by a field as bars, a stack, a donut
or a line over time; its settings (`[data-action="chart-settings"]`) are a
popover while arranging: Shape, Group by, Split by for a stack, Measure, and
for a line Series, Interval and Since. Every chart carries a legend from two
groups, the total in words and "As a table" beneath it. Six colours, in a fixed
order by label; a seventh group folds into Other. A milestones widget lists the
open milestones with the card's own progress bar and due day
(`data-milestone-widget=<name>`), or one milestone in full when its settings
name one (`select[aria-label="Milestone for <title>"]`, `data-milestone-single`);
the filter tile's Milestone select (`#field-milestone`) narrows the whole
dashboard to one. The tile's live values live in the address (`types`, `team`, `assignee`,
`category`, `field`, `days`, `fq`); its saved defaults live in the widget's
config and are written by "Save as the default" while arranging. Every widget
whose kind narrows is asked with the composed query; the others carry a note
(`data-not-narrowed`). Widgets are added from cards while arranging
(`data-add-widget`), moved with arrows, and removed with an Undo toast.

## Search (content)

```
Search
[⌕ assignee = currentUser() AND statusCategory != done               ?] [Search]
Recent: statusCategory != done · project = ALP AND type = Bug
| results table |
Errors keep the caret line under the offending character.
```

Under the bar, once something is typed or Arrow Down pressed, a listbox (`[data-query-suggestions]`,
the input is a combobox with `aria-activedescendant`): the words the query
takes at the caret (`[data-suggestion="completion"]`, monospace with a hint:
field, operator, value, keyword, function), then the issues the text finds
(`[data-suggestion="issue"]`, key then summary, `data-suggestion-text` is the
key). An empty bar lists recent queries (`"recent"`) and saved searches
(`"filter"`). Arrows move, Enter takes the row, Tab takes the first word,
Escape closes; Enter with nothing highlighted runs the query. The compact
bar on a project's issues page shows words and issues only, scoped to the
project.

## Settings hub (narrow) and profile (narrow)

```
Settings
YOU             Profile · API tokens
ORGANIZATION    Access · Workflows · Labels · Fields · Automation · Webhooks
                Audit log

Settings / Profile
[ (TK) ]  [Upload picture] [Remove]        PNG or JPEG, up to 2 MB.
Name       [Ada Lovelace              ]
Email      ada@armature.test   (read-only; sign-in uses it)
Time zone  [Europe/Berlin           v]   Language [English (United Kingdom) v]
           Dates are shown as 5 Sep 2026, 14:30.               [Save changes]
ORGANIZATIONS   Acme (Owner) · Beta Corp (Member)
```

API tokens: a name and a "Can only read" checkbox (`#field-can-only-read`)
make a token; the secret is shown once. Each row is `data-token=<name>` and
a read-only one also carries `data-token-scope="read"` and a "Read only" tag.

Below the profile form, Notifications: a grid of reasons by Inbox and Mail
checkboxes (`data-pref-inapp`, `data-pref-mail`), a "Mail arrives" select
(`#field-digest`: as it happens, bundled hourly, bundled daily) and a save
button. Unticked is off; everything starts on.

Under the profile's organizations, "Your data": "Download my data"
(`[data-action="export-me"]`, an anchor that downloads the JSON file) and
"Delete my account" (`[data-action="erase-me"]`, a danger Button behind a
confirm whose noun is "account"). Access gains a Members tab: one row per
member (`[data-member=<name>]`) with their address, role and an IconButton
"Remove <name>" (`[data-action="remove-member"]`, behind a confirm), disabled
on your own row. The portal's foot puts the customer's name on a Menu
(`[data-action="customer-menu"]`) with the same two actions.


## Users (narrow)

```
Settings / Users
[Email            ] [Name             ]
[Role      v      ] [Password         ]           [Create user]
NAME          EMAIL              SIGNS IN WITH   ROLE           STATE
Ada Lovelace  ada@armature.test  Password        Owner          Active   ...
```

The organization's own accounts. The form (`[data-new-user]`) takes
`#field-email`, `#field-name`, `#field-role` (Member, Administrator) and
`#field-password`, and "Create user" toasts. Each row is `data-user=<email>`
with `data-user-active` and `data-user-managed`; its Menu
(`[data-action="user-menu"]`) holds Rename (`rename-user`, a Dialog
`[data-rename-user=<email>]` with `#field-new-name`), Change role
(`change-role`, `[data-change-role=<email>]` with `#field-new-role`), Set
password (`set-password`, `[data-set-password=<email>]` with
`#field-new-password`) and Deactivate (`deactivate-user`, a danger item behind
a confirm whose noun is "account") or Reactivate (`reactivate-user`). Every
item is disabled on your own row and on anybody who also belongs to another
organization (`data-user-managed="false"`). Somebody who is not a global
administrator sees an empty state. The profile gains a Password card
(`[data-change-password]`): `#field-current-password`, `#field-new-password`
and "Change password" (`[data-action="change-password"]`), toasting
"Password changed".

## Themes (narrow) and the theme editor (content)

```
Settings / Themes                                        [+ New theme]
[Mine | Shared with me]
THEME              OWNER    IN USE
Magenta  Shared    you      2 people                     ...
```

`/settings/themes` lists what the reader may use; rows are
`data-theme-row=<name>` with `data-theme-active`, and the Menu
(`[data-action="theme-menu"]`) holds Use this theme (`use-theme`) or Stop
using (`stop-theme`), Edit (`edit-theme`), Share with the organization or
Stop sharing (`share-theme`) and Delete (`delete-theme`, behind a confirm
whose noun is "theme" and whose body says how many people go back to the
built-in theme). `[data-themes-view="mine"|"shared"]` switch the list;
`[data-action="new-theme"]` opens the editor.

`/settings/themes/new` and `/settings/themes/<id>` are the editor
(`[data-theme-editor=<id>|"new"]`): `#field-theme-name`, a Switch "Shared with
the organization" (`[data-theme-shared]`), a Switch "Preview on this page"
(`[data-action="preview-theme"]`) that shows the draft on the page it is on,
"Save theme" (`[data-action="save-theme"]`) and, once saved, "Use this theme"
(`[data-action="use-theme"]`). Tabs (`data-theme-tab`): Colours (a
`[data-theme-mode="light"|"dark"]` Segmented over the token groups, each a
`ColorField` `#field-token-<name>` with `data-token` and a Reset), Type,
Shape, Cursors (`[data-theme-cursor=<kind>]`), Icons (every glyph with its
`[data-theme-icon=<name>]` row, a picture or path data), Backdrop, Files
(`[data-action="add-theme-file"]`, rows `data-theme-asset=<name>`, a file in
use cannot be removed) and Advanced (`[data-theme-css]`).

## Releases (content) and Components (content)

```
Releases
[Version 1.0     ] [Release on] [What is in it            ] [Add version]
[Unreleased | Released | Archived]
VERSION   PROGRESS                 RELEASE      STATE
1.0       1 of 2 done, 50% ====    24 Dec 2026  Unreleased   ...
```

A version row (`data-version=<name>`, `data-version-state`) shows its
progress bar (`data-version-progress`) and a menu: Release notes (a drawer,
`data-release-notes`, finished issues by type, `data-notes-issue`, Copy as
text), Release or Take the release back, Edit (a drawer with name, what is
in it, start and release days), Archive once released, Delete. Planning a
version takes the sprint permission; reading is for everyone.

Components: a form (name, lead, who new work goes to) and a table
(`data-component=<name>`, `data-component-assignee`) with Edit and Delete.
Configuring them is administering the project.

On the issue page, Planning gains Fix versions, Affects versions and
Components: tags for what is chosen and an edit button opening a popover of
checkboxes (`data-picker`, `data-picked`, `data-picker-option`).

## Saved searches, bulk edit, import (content)

```
Search                                      [Save this search] [Export] [All saved searches]
[query .................................................]
(*) Open work   ( ) Bugs this week   ( ) Shared by Ada
[x] 3 selected  Priority [high v] Assignee [Leave v] Add labels [ ] Transition [Start progress]  [Apply to 3]
```

Under the query, the saved searches as chips (starred first, then mine, then
shared) with a star on each, Save this search (a dialog: name, share) and
Export (a dialog of column boxes and a Download link). Ticking rows (a box
per row and one in the header) puts the bulk bar over the list; Apply answers
with a dialog naming what was refused and why. `/filters` is a table of the
saved searches by view (mine, shared with me, starred) with a Mail me select
per row and a menu to share or delete one's own.

Import, under a project's Setup: choose a CSV file, then three cards. Which
column is which, one row per column with its position and its first values
beside its name, because a file may name five columns Labels. What its words
mean here, one row per distinct type, status and priority the file holds, with
what each matches. Who its people are, one row per name, each of them a member,
an account made at a domain you give, or nobody. Then Try it dry, then Import;
each run says what was made, what was corrected from an earlier run, which rows
were refused and on which line of the file, and what it added to the project on
the way through.

The issue's head has a More button with Clone (a dialog: summary, copy links,
copy subtasks) and Move to another project (a dialog: project, and a status
when the target workflow does not have the issue's).

## Automation (content) and Webhooks (content)

```
Automation                                                  [+ New rule]
RULE              DOES                                      LAST RUN  STATE
Mark moved work   When an issue moves, then add the label   2 min ago  On   ...
                  moving.
```

A rule is a row read as a sentence (`data-rule=<name>`), with a menu
(`data-rule-menu`) for the run log (a drawer, `data-rule-log`, rows
`data-rule-run=<outcome>` with the reason and each action's note), Run now,
Edit, Turn off and Delete. The editor (`data-rule-editor`) is a dialog in
three parts, When (`#field-trigger` and the trigger's own fields), If
(condition rows, `data-rule-condition`) and Then (action rows,
`data-rule-action`, at least one), plus the hourly cap and whether the rule
runs on the automation's own changes. An incoming rule shows its address
(`data-incoming-address`) once it exists. The same panel serves
`/settings/automation` for the organization's rules.

Webhooks: a table of endpoints (`data-webhook=<name>`) with a menu for
Deliveries (drawer `data-webhook-log`, rows `data-delivery=<topic>` with
`data-delivery-state` and an Again button), Send a test, Turn off, Rotate the
secret and Delete. Add a webhook is a card form (`data-webhook-form`) with
name, address and topic checkboxes (`data-topic`); the secret is shown once in
a card (`data-webhook-secret`, `data-secret-value`).

## Inbox (narrow)

```
Inbox                                          [Mark all read]
[Unread | Everything]
 * Ada assigned CP-12 to you            Fix the door              2 min ago
 * Ada mentioned you on CP-9            Have a look @Ben          1 h ago
   Ada moved CP-3 to Done               Paint it                  yesterday
```

Rows are links to the issue (`data-notification=<kind>`,
`data-notification-read`); a dot marks unread; opening marks read. The empty
state names where the settings are. The bell in the sidebar's foot carries
the count and opens this page.

## Calendar (wide)

A month as a seven-column grid, Monday first, six weeks with the neighbours'
days dimmed and weekends tinted. Previous, Today and Next sit beside the
month's name. A day cell shows its number and up to four items; an issue is a
bar in the accent tint from its start to its due day, one-day issues a single
chip, sprints, milestones and versions in their chart tones, a done issue
quiet and struck through, and the rest folded into "+n more". An issue's bar
is grabbed with the pointer and dropped on another day; the cell under the
pointer wears a ring while it is over it, and both dates move together. A
double click on a bar opens the issue.

## Audit log (content) and organization fields (content)

The audit log is a Table under a row of three controls: an action Select
listing the actions the log holds, and From and To date Fields. Each row is
when, the action as a Tag, who, and the payload's readable words. Older and
Newer walk pages by time; Export CSV in the header downloads the same filter.
The organization's fields page reuses the project fields list without a
project: the same New field form and Table, every row tagged Organization. On
a project's fields page the organization's fields come first, read-only,
tagged, and a project's own field has a Promote button for an organization
administrator.

## Project status strip

Under the project's title on its issues page: the latest status as a coloured
Tag (On track, At risk, Off track), its note, who said it and when, a History
link, and Post an update on the right for whoever administers the project.
Post an update is a Dialog with a Segmented of the three words, a Textarea,
and an Aiming for date. History is a Dialog listing every post, newest first.
The projects table gains a Status column with the same Tag, "Not said" until
a first post.

## Project settings (narrow)

```
Projects / Alpha
Settings  [ALP]
| Project name | Description | Lead |                    [Save changes]
FEATURES
| Board       [on] | Sprints    [on] |
| Plan        [on] | Calendar   [on] |   ... one Switch per feature,
| Queues      [on] | Service desk [on]|  desk pages only on a desk
ARCHIVE                                              [Archive project]
```

Each Switch is `[data-feature=<feature>]`; flipping one saves the whole list
and toasts "Sprints turned on". A page the project lacks answers its address
with an EmptyState `[data-feature-off=<feature>]` ("Alpha does not use
sprints") and, for an administrator, a link to these settings.

## Portal (content)

The door at /desk/$orgSlug (narrow, no shell) is the code form, or, when the
address names a desk whose door is open, a Name and Email form with one
Continue Button. The header keeps Your requests and Raise a request; for a
session that came in without a code, Your requests names its desk. The request page reuses
the issue page's Description and Activity sections; followers and the reply
box stay as they are, and the issue page's Attachments panel sits between the
conversation and the reply box (`source="portal"`): every file on the request
with a Remove Button only on the customer's own.

The raise form ends with an "Attach files" Button (a hidden multiple file
input) and the queued files as a bordered list (`[data-attach-queued]`, one
`[data-attach-queued-file=<name>]` row with its size and a ghost Remove); they
go up one at a time after the request is sent, the submit Button counting
them, and a file that fails leaves an ErrorBanner naming it with a link to the
request rather than a second request.

Raise a request starts with the knowledge base: a search Field ("Is it one of
these?") above the request types, with the published articles as Cards in two
columns; typing narrows them. An article opens at `/portal/articles/<id>` as
one Card of plain text with a way back to raising the request anyway. The
search is not shown at all when the desk has written nothing.

## Service desk (content) and rating (narrow, no shell)

The service desk setup page is one column of sections: the door, request
types, goals, business hours, knowledge base, canned responses. The door is a
Card with the desk's address in a code line beside a copy IconButton, a
Switch for the code by mail with its hint, and a Trusted domains block
(`[data-trusted-domains]`): the domains as Tags each with a remove IconButton
("Stop trusting <domain>"), a Field (`#field-trusted-domain`) with an Add
Button, and the hint "Empty means any address. Applies to the door and to
replies by mail."; the Switch and the block's controls are disabled or absent
for anyone who does not administer the project. Business hours are a
Checkbox per weekday with two time Inputs beside it, a time zone Field, a
Days off Field and a box per goal that says the goal counts business hours
only. Articles and canned responses are each a form above a Table; an article
row carries a Publish or Unpublish Button, and the canned responses section
opens by naming the placeholders a response may use.

The rating page at `/rate/<token>` has no shell: one Card with the desk's
name, "How did we do with HELP-12?", the request's summary, five score
Buttons in a row with the chosen one primary, one word for the chosen score, a
Textarea and Send. Once rated, the same Card says thank you and offers nothing
else; the link in the mail lands on that sentence forever.

## Shared dashboard (content, no shell)

```
armature   Alpha   Overview                                  Updated 12:03  [theme]
Narrowed to type = "Bug"
+ Status                 1 in all +  + Velocity                                +
| In progress ▮▮▮▮▮▮ 1            |  |  Sprint 3 ▮▮▮▮ 21 / 18                 |
+---------------------------------+  |  Not narrowed by the filter ...        |
```

`/shared/$token` hangs off the root route, outside the shell: a slim header
(`data-print-hide`) with the project, the dashboard name, when the numbers
were last read (`data-shared-updated`) and the theme button; the frozen
filter as a sentence (`data-shared-query`); the widgets in the same grid
(`data-testid="dashboard"`, `data-widget`) without the filter tile and with
nothing to press. Every widget reads through the link, per widget id. A dead
link shows the sign-in layout with `[data-share-gone]`. Share
(`[data-action="share"]`) on the dashboard header opens the dialog
(`data-share-dialog`) that lists links, makes one and shows its address once
(`data-share-url`). Export as PDF (`[data-action="export-pdf"]`) on the
dashboard header and Download PDF (`[data-action="download-pdf"]`) on the
shared page are links to the api, which has the render service print the
shared page in print mode (`?print=1&theme=light`: no header, light theme,
`@media print` rules) and streams the file as a download.

## Command palette, confirm dialog, toast

```
+--------------------------------------------------+
| ⌕ ALP-1                                           |
|  ISSUES    ALP-12  Fix the login loop             |
|  PROJECTS  Alpha                                  |
|  PAGES     Alpha / Board     Alpha / Plan         |
|  ACTIONS   + New issue       ◐ Switch theme       |
+--------------------------------------------------+

+------------------------------------+   +--------------------------------+
| Delete team?                       |   | ✓ Created ALP-13   Open    ×  |
| Platform has 3 members. Its issues |   +--------------------------------+
| keep their assignees.              |
|                [Cancel] [Delete team]
+------------------------------------+
```

Ask mode: the question mark in the sidebar's foot (`data-action="guide"`)
opens the palette with `data-palette-mode="ask"`, and any question of two
words or more adds an Answers group (`data-palette-option="answer:<id>"`) in
either mode. An answer navigates and points at its element with a Spotlight
(`[data-guide-callout]`, the element carries `data-guide=<id>`); no answer
shows `[data-palette-empty]`, or, when a model is configured, an "Ask the
assistant" option whose answer renders above the list (`[data-assistant-answer]`)
with a Go there button. A change the model asked for appears beside the
answer as `[data-proposal=<tool>]` with a Confirm button
(`[data-action="confirm-proposal"]`); nothing changes until it is pressed,
and the row then reads Done.
