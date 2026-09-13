# The browser suite's contract

The scenarios in `e2e/scenarios/`, one module per area and loaded in order
by `e2e/suite.mjs`, and `e2e/helpers.mjs` read the interface through the hooks
below. A redesign keeps them on the same elements, or changes a flow through
one named helper so each scenario moves by a line.

## Selectors that must keep working

- **Field ids**: `fill(page, "Label")` finds `#field-<label slug>` (the label
  lowercased, spaces to dashes). `Field` and `Select` derive the id from the
  label. Hard-coded ids also read: `#field-key`, `#field-name`,
  `#field-add-status`, `#field-new-status-name`, `#field-new-status-category`,
  `#field-transition-name`, `#field-to-status`, `#field-scheme-name`,
  `#field-new-sprint`, `#field-new-team`, `#field-link-name`, `#field-expires-on`, `#field-milestone`, `#field-milestone-name`,
  `#field-template-name`,
  `#field-milestone-due`, `#issue-type`, `#issue-start`, `#issue-due`,
  `#issue-estimate`, `#issue-sprint`, `#issue-milestone`, `#issue-team`,
  `#arrange-type`, `#arrange-where-<place>`,
  `#child-type`, `#child-summary`, `#new-comment`, `#portal-description`,
  `#portal-reply`, `#rt-team`, `#rt-template`.
- **Button text**: `clickButton(page, "Label")` matches visible text. A button
  that becomes an icon keeps its text in an `sr-only` span.
- **aria labels**: `select[aria-label="Issue type"|"Workflow"|"Milestone for
  <widget title>"|"Workflow for <type>"|"Who to grant to"|"Role"|"Where the role applies"]`, `[role="group"][aria-label="Zoom"]
  button[aria-pressed="true"]`.
- **Settled**: `goto`, `reload` and `settled` wait for `data-settled="true"`
  on `<body>` to hold for three animation frames. `Settled` in the root route
  writes it from React Query's fetching and mutating counts and the router's
  status: busy the instant a fetch or mutation starts, settled from an effect
  after every mounted component's. The frames are there because a query's
  answer and the render that consumes it, which starts the next queries, are
  not one step in React; three frames stretch with a busy main thread where a
  timer would not. A scenario may read the page the moment `goto` returns.
  Data the app fetches outside React Query would not be waited for; there is
  none today.
- **The shell**: `waitForApp` waits for a `<header>` and
  `a[href="/settings/tokens"]`. `[data-action="sign-out"]` and
  `[data-action="theme"|"panel-prev"|"panel-next"|"share"|"copy-share"|"revoke-share"|"export-pdf"|"download-pdf"]` are pressed; the theme button's text is read as
  Auto, Light or Dark.
- **data attributes**: data-access-tab, data-action, data-add-widget,
  data-assignment, data-assignment-control, data-assignment-shown,
  data-attach-queued, data-attach-queued-file, data-attachment,
  data-attachments-note, data-backlog-of, data-bar, data-board,
  data-board-showing, data-board-type, data-branch, data-branch-head,
  data-branch-merged, data-branch-repository, data-burndown,
  data-burndown-summary, data-card, data-chart, data-chart-group,
  data-chart-legend, data-chart-part, data-commit, data-connect-handle,
  data-custom-field, data-dashboard, data-dashboard-template, data-description, data-desk-address, data-desk-code, data-desk-entry, data-dir, data-graph-unlink, data-issue-history, data-field, data-filter-category, data-filter-count, data-filter-tile,
  data-filter-type,
  data-follower-address, data-graph-edge, data-graph-edge-hit,
  data-assistant-answer, data-graph-handle, data-graph-node, data-graph-unlinked, data-group,
  data-guide, data-guide-callout,
  data-issue, data-issue-drawer, data-issue-link, data-issue-list,
  data-issue-page, data-issue-panel, data-issue-row, data-issue-watcher,
  data-label,
  data-label-row, data-labels, data-load, data-milestone, data-milestone-flag,
  data-milestone-dashboard, data-milestone-progress, data-milestone-single, data-milestone-widget,
  data-milestones-widget, data-node-panel, data-plan-add, data-plan-add-row,
  data-plan-arrow, data-plan-arrow-hit, data-plan-trouble, data-plan-trouble-icon, data-plan-unlink, data-plan-warning-for, data-plan-bar, data-plan-bar-row,
  data-not-narrowed, data-open-drawer, data-open-page,
  data-palette, data-palette-empty, data-palette-mode, data-palette-option,
  data-plan-closed-for, data-plan-draft-summary, data-plan-drop-top,
  data-plan-figure, data-plan-group, data-plan-group-band,
  data-plan-link-handle, data-plan-load-cells, data-plan-load-row,
  data-plan-load-week, data-plan-milestone, data-plan-row, data-plan-schedule,
  data-plan-type-filter, data-print-hide, data-pull-request, data-px-per-day, data-query,
  data-query-caret, data-query-error, data-queue-row, data-queue-team,
  data-repository, data-request-category, data-request-team,
  data-request-type, data-request-type-team, data-request-watcher, data-rule,
  data-rail, data-selected, data-series, data-sla, data-sla-state, data-sprint, data-sprint-board,
  data-save-template, data-share, data-share-dialog, data-share-gone,
  data-share-made, data-share-url, data-shared-dashboard, data-shared-query,
  data-shared-updated, data-sprint-capacity, data-sprint-outcome, data-sprint-showing,
  data-swimlane, data-swimlane-editor, data-team, data-team-capacity,
  data-token, data-token-scope, data-trusted-domain, data-trusted-domains,
  data-feature, data-feature-off, data-member, data-access-tab,
  data-query-suggestions, data-suggestion, data-suggestion-text,
  data-editor, data-editor-mode, data-editor-action, data-mention, data-doc,
  data-rail, data-sidebar, data-sidebar-group, data-open, data-shell-header, data-card-actions,
  data-workflow-tab, data-scheme-card, data-status-card,
  data-inbox-view, data-mention-list, data-mention-option, data-notification,
  data-notification-read, data-notification-settings, data-pref-inapp, data-pref-mail,
  data-unread-count, data-flow-band, data-flow-legend, data-flow-status, data-cumulative-flow,
  data-control-chart, data-rolling-line, data-chart-point, data-created-resolved, data-average-age,
  data-resolution-histogram, data-release-burndown, data-versions-widget, data-version-widget,
  data-version-setting, data-saved-filters, data-filter, data-filter-shared, data-filter-star, data-starred,
  data-save-filter, data-export-dialog, data-export-column, data-filter-row, data-filters-view,
  data-filter-subscription, data-filter-menu, data-select-all, data-select-issue, data-bulk-bar,
  data-bulk-result, data-bulk-refusal, data-import-input, data-import-file, data-import-mapping,
  data-import-column, data-import-target, data-import-words, data-import-word, data-import-word-means,
  data-import-people, data-import-person, data-import-person-choice, data-import-domain,
  data-import-report, data-import-refusal, data-import-partial, data-import-notes, data-import-updated,
  data-clone-dialog, data-move-dialog, data-version, data-version-state, data-version-menu, data-version-progress,
  data-versions-view, data-version-editor, data-release-notes, data-notes-group, data-notes-issue,
  data-component, data-component-assignee, data-component-menu, data-component-editor,
  data-picker, data-picked, data-picker-option, data-rule, data-rule-enabled, data-rule-menu, data-rule-editor,
  data-rule-condition, data-rule-action, data-rule-log, data-rule-run, data-incoming-address,
  data-webhook, data-webhook-enabled, data-webhook-menu, data-webhook-form, data-webhook-secret,
  data-secret-value, data-topic, data-webhook-log, data-delivery, data-delivery-state,
  data-template, data-testid, data-theme, data-time-spent, data-unwatched, data-value,
  data-widget, data-widget-loading, data-workflow-canvas, data-workflow-card, data-workflow-edge,
  data-workflow-edge-label, data-workflow-graph, data-workflow-initial, data-workflow-node,
  data-workflow-subtitle,
  data-worklog,
  data-portal-address,
  data-article, data-article-published, data-article-search, data-portal-article,
  data-canned, data-canned-option, data-business-hours, data-weekday, data-weekday-open,
  data-goal-calendars, data-policy-calendar, data-rate-page, data-score, data-rated,
  data-queue-csat, data-csat-widget,
  data-audit-filters, data-audit-row, data-audit-actor, data-field-scope,
  data-calendar, data-calendar-title, data-day, data-drop, data-calendar-item, data-calendar-kind,
  data-calendar-more, data-status-strip, data-status-note, data-project-status, data-status-dialog,
  data-status-option, data-status-history, data-status-update,
  data-arrangement, data-arrange-area, data-arrange-origin, data-arrange-place,
  data-arrange-type, data-arrange-where, data-issue-group, data-pick-many,
  data-create-issue, data-create-refused (the key of the issue that was made
  but did not take everything).
- One structural selector: `span[aria-hidden='true'].w-px` inside the plan's
  chart (the today line).

## Helpers a changed flow adds

- `openPalette(page)` presses Ctrl+K and waits for `[data-palette]`.
  `askGuide(page, question)` presses `[data-action="guide"]`, types the
  question, follows the first `[data-palette-option^="answer:"]` and returns
  its id, or null after `[data-palette-empty]`. The callout is
  `[data-guide-callout]`; its element carries `data-guide=<id>`. With a model
  configured, an unanswered question offers `[data-palette-option="ask-assistant"]`
  and the answer renders in `[data-assistant-answer]` with `[data-action="go-there"]`.
- `createToken(page, name, { readOnly })` makes an API token on the settings
  page (`#field-can-only-read` is the read-only box) and returns the secret the
  page shows once.
- `confirm(page)` presses `[data-action="confirm"]` in the open dialog.
- `shareDashboard(page, name)` opens the Share dialog (`[data-action="share"]`,
  `[data-share-dialog]`), makes a link and returns the address read from
  `[data-share-url]`, the one place it is shown. `revokeShare(page, name)`
  revokes the row `[data-share=<name>]`. The shared page is `/shared/$token`
  with `data-shared-dashboard=<name>`; a dead link shows `[data-share-gone]`.
- `milestoneDashboard(page, projectKey, name)` presses
  `[data-action="milestone-dashboard"]` on the milestone's card, submits the
  dialog (`[data-milestone-dashboard=<name>]`) and waits to land on the
  dashboard page, which names the open dashboard in the address as `d`.
- `createDashboard(page, projectKey, name, templateTitle)` fills the New
  dashboard form, picks the template card whose text has the title
  (`[data-dashboard-template=<key|id>]`, "blank" first), and waits for
  `[data-dashboard=<name>]`. `[data-action="save-template"]` opens the save
  dialog while arranging; `[data-action="remove-template"]` sits beside a
  saved template's card.
- `chartSettings(page, {shape, groupBy, splitBy, series})` opens a chart
  widget's settings popover (`[data-action="chart-settings"]`,
  `[data-testid="chart-settings"]`) and picks from the Shape group and the
  `#field-group-by`, `#field-split-by`, `#field-series` selects.
- `addWidget(page, kind)` arranges if need be, clicks `[data-add-widget=kind]`
  and waits for `[data-widget=kind]`. The filter tile's query field is
  `#field-narrow-with-a-query`; `[data-action="save-filter"]` saves the live
  filter as the tile's default.
- `openIssueBeside(page, issueKey)` clicks a row's key cell and waits for
  `[data-issue-panel="KEY"]`, docked or as the drawer. `withViewport(page,
  width, fn)` runs fn at another width and restores the runner's 1280px; the
  panel docks from 1440px, so a docked scenario asks for 1600.
- `assignWorkflow(page, projectKey, typeName, workflowName)` decides one row
  on the project's workflow page and waits for the table to say so; the table
  reader `assignmentRows` ignores the control cell so option text never leaks
  into a row.
- `arrangeIssue(page, projectKey, typeName, { moves, follow })` arranges one
  issue type on `/projects/<key>/issue-view`, pressing the area select on each
  place rather than synthesising a drag, and waits until the place sits under
  the card whose heading carries that area's title. `groupRows(page, title)`
  reads the `dt` labels of one `[data-issue-group]` on the issue page.
- `selectNode`, `dragNode` and `dragConnect` drive the workflow canvas with
  the real mouse (`page.mouse`), since React Flow listens for mouse events
  on the window; `data-workflow-node` (with `data-x`/`data-y`),
  `data-workflow-edge`, `data-workflow-initial`, `data-workflow-anywhere` and
  `data-connect-handle` sit on the library's wrapper elements, `div`s and
  `g`s rather than the old `<g>`s, and read-only drawings still carry no
  `role="button"` and no `data-connect-handle`.
- `mentionInComment(page, issueKey, words, personName)` types an at sign and
  the first letters into `#new-comment`, picks `[data-mention-option=<name>]`
  from `[data-mention-list]` and posts. `inboxRows(page)` opens `/inbox`
  (`[data-inbox-view=unread|all]` switch) and reads `[data-notification=<kind>]`
  rows with `data-notification-read`. `unreadCount(page)` reads the bell's
  `[data-unread-count]` (`[data-action="inbox"]` opens the inbox). The
  preferences grid on the profile page is `[data-notification-settings]` with
  `[data-pref-inapp=<kind>]`, `[data-pref-mail=<kind>]`, `#field-digest` and
  `[data-action="save-notifications"]`.
- `createRule(page, projectKey, {name, trigger, action, value, text})` fills
  the editor (`[data-rule-editor]`, `#field-trigger`, `#field-action-0`,
  `#field-action-value-0` or `#field-action-text-0`, `[data-action="save-rule"]`)
  and waits for `[data-rule=<name>]`. `ruleMenu(page, name, action)` opens
  `[data-rule-menu=<name>]` and presses `rule-log`, `rule-run`, `rule-edit`,
  `rule-toggle` or `rule-delete`; the log is `[data-rule-log=<name>]` with
  `[data-rule-run=<outcome>]` rows. `incomingAddress(page, name)` reads the
  hook's path from `[data-incoming-address]` in the editor.
- `createWebhook(page, name, url)` fills `[data-webhook-form]` and returns the
  secret from `[data-secret-value]`, shown once. `webhookMenu(page, name,
  action)` opens `[data-webhook-menu=<name>]` for `webhook-deliveries`,
  `webhook-test`, `webhook-toggle`, `webhook-rotate` or `webhook-delete`; the
  log is `[data-webhook-log=<name>]` with `[data-delivery=<topic>]` rows
  carrying `data-delivery-state` and a `[data-action="redeliver"]` button.
- `createVersion(page, projectKey, name, releaseOn)` fills the Releases page's
  form (`#field-version-name`, `#field-version-release`) and waits for
  `[data-version=<name>]` (`data-version-state` unreleased|released|archived,
  `[data-version-progress=<percent>]`). `versionMenu(page, name, action)`
  opens `[data-version-menu=<name>]` for `version-notes`, `version-release`,
  `version-unrelease`, `version-edit`, `version-archive` or `version-delete`;
  the notes drawer is `[data-release-notes=<name>]` with
  `[data-notes-issue=<key>]` rows and `[data-action="copy-notes"]`;
  `[data-versions-view=<view>]` switches the table.
- `createComponent(page, projectKey, name, {assignee})` fills the Components
  page's form (`#field-component-name`, `#field-component-lead`,
  `#field-component-assignee`) and waits for `[data-component=<name>]`
  (`[data-component-assignee=<name>]` says who takes new work).
- `pickName(page, pickerId, name)` opens one of the issue page's set pickers
  (`#issue-fix-versions`, `#issue-affects-versions`, `#issue-components`),
  ticks `[data-picker-option=<name>]` and waits for `[data-picked=<name>]`
  inside `[data-picker=<id>]`.
- `saveSearch(page, name, {shared})` presses `[data-action="save-search"]`,
  fills `#field-filter-name` (and `#field-share-filter`) in `[data-save-filter]`
  and waits for the chip `[data-filter=<name>]` (`data-filter-shared`,
  `aria-pressed` when it is the search running); `[data-filter-star=<name>]`
  toggles a star (`data-starred`). `/filters` rows are `[data-filter-row=<name>]`
  with `[data-filters-view=mine|shared|starred]`, a `[data-filter-subscription=<name>]`
  select and `[data-filter-menu=<name>]` for `filter-share` and `filter-delete`.
  `[data-action="export-csv"]` opens `[data-export-dialog]` with
  `[data-export-column=<name>]` boxes and `[data-action="download-csv"]`.
- `selectAllIssues(page)` ticks `[data-select-all]` (rows have
  `[data-select-issue=<key>]`) and waits for `[data-bulk-bar=<count>]`;
  `applyBulk(page, {transition, priority})` fills `#field-bulk-transition`,
  `#field-bulk-priority` (also `#field-bulk-assignee`, `#field-bulk-labels`),
  presses `[data-action="bulk-apply"]` and reads `[data-bulk-result]` with its
  `[data-bulk-refusal=<key>]` rows.
- `moveIssue(page, issueKey, targetKey)` opens `[data-action="issue-more"]`,
  `[data-action="move"]`, picks `#field-move-project` in `[data-move-dialog]`
  (`#field-move-status` when the workflow asks) and presses
  `[data-action="move-go"]`; `[data-action="clone"]` opens `[data-clone-dialog]`
  (`#field-clone-summary`, `#field-clone-links`, `#field-clone-subtasks`,
  `[data-action="clone-go"]`).
- `attachFile(page, path)` puts a file through the
  `input[type=file][aria-label="Attach a file"]` picker of the attachment
  panel shown, on the issue page or the portal's request page, and waits for
  `[data-attachment=<name>]`; `queueFiles(page, paths)` uses the raise form's
  `aria-label="Attach files"` picker and waits for each
  `[data-attach-queued-file=<name>]`. The agent's panel on a desk request
  carries `[data-attachments-note]`.
- `importCSV(page, projectKey, path, {dry})` uploads through
  `[data-import-input]` on the Import page, maps in `[data-import-mapping]`
  (`[data-import-target=<column>]` selects), presses `[data-action="dry-run"]`
  and `[data-action="import"]`, and reads `[data-import-report=dry|done]` with
  `[data-import-refusal=<row>]` rows.
- The flow widgets are added with `addWidget(page, kind)` for
  `cumulative_flow` (bands `[data-flow-band=<status>]`, legend
  `[data-flow-legend]` with `[data-flow-status=<status>]`), `control_chart`
  (`[data-chart-point=<key>]`, `[data-rolling-line]`), `created_vs_resolved`,
  `avg_age`, `resolution_histogram` and `release_burndown` (its version is
  picked with `[data-version-setting]`, shared with the `versions` widget).
- `writeArticle(page, projectKey, title, body, {publish})` fills
  `#field-article-title` and `#field-article-body` on the service desk page,
  presses Add article and, when publishing, `[data-article=<title>]
  [data-action="article-publish"]` until `[data-article-published="true"]`.
  `writeCannedResponse(page, projectKey, name, body)` does the same with
  `#field-canned-name`, `#field-canned-body` and `[data-canned=<name>]`; the
  reply box offers them behind `[data-action="canned-responses"]` as
  `[data-canned-option=<name>]`.
- `articlesOffered(page, query)` types into `#field-article-search` on the
  portal's Raise a request page and reads `[data-article-search]
  [data-article=<title>]`; an article page is `[data-portal-article=<title>]`.
- `ratingLinkFor(address, requestKey)` reads the `/rate/<token>` path out of
  the resolution mail; `rateRequest(page, path, score, comment)` presses
  `[data-score=<n>]` and `[data-action="send-rating"]` on `[data-rate-page]`
  and waits for `[data-rated]`. The queue shows `[data-queue-csat=<score>]`.
- Business hours live in `[data-business-hours]`: `[data-weekday-open=<mon..sun>]`
  boxes, `#field-calendar-timezone`, `#field-calendar-holidays` and
  `[data-action="save-hours"]`; each goal's box is `[data-policy-calendar=<metric>]`.
- `auditRowsFor(page, action)` opens Settings, Audit log, picks
  `#field-audit-action` (also `#field-audit-from`, `#field-audit-to`) and reads
  `[data-audit-row=<action>]` rows; `[data-action="export-audit"]` is the CSV,
  `[data-action="audit-older"]` the next page.
- Fields carry `[data-field-scope=org|project]`; `[data-action="promote-field"]`
  on a project field asks the confirm dialog and makes it the organization's.
  The organization's own list is at `/settings/fields` with the same form.
- `calendarDaysOf(page, key)` reads which `[data-day=<YYYY-MM-DD>]` cells hold
  `[data-calendar-item=<key>]` (kind in `[data-calendar-kind]`);
  `dragCalendarItem(page, key, fromDay, toDay)` drags the bar by pointer from
  one cell to another. `[data-action="calendar-prev|today|next"]` walk months,
  `[data-calendar]` names the month shown, `[data-calendar-more=<n>]` folds a day.
- `postStatus(page, projectKey, status, note)` presses `[data-action="post-status"]`
  on the project's issues page, picks `[data-status-option=<status>]`, types
  `#field-status-note` (also `#field-target-on`) and presses
  `[data-action="post-status-go"]`; the strip `[data-status-strip]` then wears
  `[data-project-status=<status>]`, as does the projects table row.
  `[data-action="status-history"]` opens `[data-status-history]` with
  `[data-status-update=<status>]` rows.
- A project somebody holds no role in is not on the projects page, so
  `[data-project=<key>]` is absent for it, and its pages and its issues show
  the router's "Page not found" rather than a refusal.
- `goto(page, path)` loads a page and waits for it to settle; `reload(page)`
  does the same in place, and `settled(page)` alone waits after an in-app
  navigation. `fill` pastes its value in one piece rather than typing it, so a
  field that listens for key events is filled through `page.type` instead.
- A dependency arrow's remove control sits on the arrow: after a click on
  `[data-plan-arrow-hit]` (or a pointer over it) an IconButton
  `[data-plan-unlink=<linkId>]`, also `[data-plan-remove-dependency]`, with the
  text "Remove dependency A blocks B" appears inside `[data-testid="plan-calendar"]`;
  on the graph it is `[data-graph-unlink=<linkId>]`, also `[data-graph-remove]`,
  inside `[data-testid="graph-canvas"]`. A troubled row carries
  `data-plan-trouble="warning"|"error"` and a `title` with the words, as does
  its `[data-plan-bar]`; `[data-plan-trouble-icon=<severity>]` beside the key
  carries them as `aria-label`; each entry of `[data-testid="plan-warnings"]`
  says `data-plan-warning-for=<key|sprint|team>` and `data-plan-severity`.
- The issue page's changelog is its last section, `[data-issue-history]`
  with `[data-activity="history"]`; comments are `[data-activity="comments"]`
  under the Activity title. There are no Activity tabs.
- `enterDesk(page, slug, address, next)` goes through the door with a mailed
  code (`#field-email`, "Send me a code", `[data-desk-code]`, "Enter").
  `enterOpenDesk(page, slug, deskKey, who, next)` walks into a desk whose door
  is open: the page `/desk/<slug>?desk=<key>` shows `[data-open-door=<key>]`
  with `#field-your-name`, `#field-email` and `[data-action="enter-open"]`
  ("Continue"). `toggleDoor(page, projectKey)` flips `[data-action="toggle-door"]`
  on the service desk page, waits for its `data-door-verifies` to change and
  returns the address in `[data-desk-address]`; `[data-action="copy-desk-address"]`
  copies it. The door section is `[data-guide="desk-door"]`.
  `trustDomain(page, projectKey, domain)` types into `#field-trusted-domain`,
  presses `[data-action="add-domain"]` and waits for
  `[data-trusted-domain=<domain>]`; `turnedAwayAtOpenDesk(page, slug, deskKey, who)`
  fills the open door's form, presses Continue and returns the `[role=alert]`
  text the refusal shows. An open door answers an address with a password or
  a place on a team the way a careful desk does, asking for a code.
- `inviteAddress(page, email, role)` invites an address and returns the
  invitation's token; `inviteMember` is it with a fresh identity.
  `tryAcceptInvite(page, { token, name, password })` offers the invitation
  from the browser as it is and returns the refusal's error code, empty when
  it was taken: `sign_in_to_accept` for an address that has an account and
  nobody signed in, `invite_for_someone_else` for another signed-in person.
- The shell: `waitForApp` waits for a `<header>` (the rail's top) and a
  rendered `a[href="/settings/tokens"]` (the Settings group, open by
  default; a scenario that folds the sidebar expands it again before
  `waitForApp`). `[data-action="sidebar"]` is the toggle, in the rail;
  `theme`, `guide`, `inbox`, `new-issue` and `profile` live in the rail;
  `sign-out` is in the sidebar's foot while open and in the rail's avatar
  menu while folded. `[data-workflow-card=<name>] [data-action="edit"]` opens
  the designer (`openWorkflowDesign`); `createScheme` goes to
  `/settings/workflows/schemes`; `deleteScheme(page, name)` presses
  `[data-scheme-card=<name>] [data-action="delete"]` and confirms.
- `#issue-description` and `#new-comment` name the editable element in
  whichever face the editor shows: the contenteditable div in rich mode, the
  textarea in markdown mode. `editorText(page, selector)` reads either;
  `writeInEditor(page, selector, text)` empties it and types;
  `setEditorMode(page, "rich" | "markdown")` presses `[data-editor-mode=...]`.
  `page.type` into the rich face works as it does into a textarea, and the
  markdown shortcuts format as they are typed. A mention picked in either
  face lands as `[data-mention]` in the comment. `#field-link-address` is the
  toolbar's link field.
- `pickSuggestion(page, text)` presses, by mouse, the row under the query box
  (`[data-query-suggestions] [data-suggestion]`) whose `data-suggestion-text`
  or text starts with `text`. The list opens on typing or Arrow Down, not on focus; `runQuery`
  is unchanged, since Enter with nothing highlighted runs the query.
- `deleteMyAccount(page)` presses `[data-action="erase-me"]` on the profile
  page, confirms, and waits for the sign-in page. `removeMember(page, name)`
  opens Access, the Members tab, presses `[data-member=<name>]`'s
  `[data-action="remove-member"]`, confirms and waits for the row to go.
  `[data-action="export-me"]` is the anchor that downloads the export; in the
  portal both live in the `[data-action="customer-menu"]` Menu.
- `turnFeature(page, projectKey, feature, on)` flips `[data-feature=<feature>]`
  on the project's settings page and waits for its `aria-checked`; a page the
  project lacks shows `[data-feature-off=<feature>]`.
- `openFilters(page)` opens the issues toolbar's Filters popover.
- `openPlanFilters(page)` opens the plan toolbar's Filters popover.

- The search page: `[data-action="query-help"]` opens the query help popover;
  `[data-recent-queries]` lists the queries this browser ran, newest first.
  `[data-query]`, `[data-query-error]` and `[data-query-caret]` are unchanged.
- The kit sweep changed no selector: a `Chip` is still a button with
  aria-pressed, an `OptionCard` still `role="radio"` with aria-checked, a
  `SelectInput` still a select named by aria-label, and every data-* attribute
  sits on the same element as before. A palette option is a `div[role="option"]`
  rather than a button; `[data-palette-option]` is unchanged.
