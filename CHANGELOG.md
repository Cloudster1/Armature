# Changelog

What changed in each release, newest first. Versions follow
[semantic versioning](https://semver.org); `make release` moves everything
under Unreleased into a section of its own, which becomes the release's notes
and its tag message, so a change is written here in the branch that makes it.

## Unreleased

- Added: the issue list, the create dialog, an issue's page and its panels
  (activity, history, links, watchers, pages, files, time and hierarchy)
  speak German as well as English, from `web/src/i18n` (#371). Durations keep
  the d, h and m a person types in both languages.
- Fixed: no main page scrolls sideways on a phone (#275). Tabs scroll in
  their own row, a toolbar's buttons and an issue's transitions wrap, a
  summary in the issue, queue and portal tables keeps room to be read and
  the table scrolls in its box instead, a chip cuts a long name short with
  the whole of it as its title, and the bulk bar folds to its count and one
  button until that button opens the rest.
- Changed: Armature uses the names Stator and Exciter use (#309), in English
  and German. The palette is Quick search, a project's settings page is
  Project settings, its group Project admin, the Access tab Sign-in is Single
  sign-on, and API tokens are access tokens wherever they were still called
  that. Inbox, Access tokens and the profile's Language were renamed earlier.
- Added: Armature speaks English and German from one catalogue, as Stator
  and Exciter do (#354). The profile's Language changes the interface as
  well as how dates and numbers are written: a German locale shows it in
  German, any other the browser's language, and its first choice now reads
  "Follow the browser". The shell (rail, sidebar, drawer, avatar menu, bell,
  palette and the settings hub), the profile's own form and the planning
  view come from `web/src/i18n` first; a test refuses a key missing in either
  language and a German line left in English.
- Fixed: "Follow the browser" on the profile could not be saved, and choosing
  it broke the page (#354). Every account held `en`, a value the profile
  never offered; a migration makes the unchosen locale empty, which follows
  the browser, and the profile accepts it. Toasts stay on screen while the
  interface changes language.
- Added: the frontend checks Stator and Exciter run (#343). `make lint` runs
  ESLint with their shared rules, in CI too; axe-core checks the shell, the
  planning view and the settings hub in Vitest; and the style ratchet now
  also refuses bare pixel sizes, arbitrary sizes, hex colours and colour
  functions outside the kit, with today's few exceptions listed with why.
  The kit's `Button` is a plain button unless it says `type="submit"`.
  `make format` formats `web/` and `e2e/` with Biome as Stator and Exciter
  do, and CI refuses a pull request `make check-format` would change.
- Changed: descriptions and comments are drawn with the document styles
  Stator and Exciter share (#344), in the view and in the rich editor alike,
  so an Armature description, a Stator page and an Exciter requirement read
  the same. The shared `.doc-content` rules are noted in
  `docs/design/03-components.md` as changed in all three tools together.
- Changed: the kit's small shapes match Stator's and Exciter's (#342). A
  page header's actions wrap instead of widening the page, a long menu no
  longer scrolls the page behind it, the unread count wears the accent,
  Settings is drawn as sliders and the key as theirs, and a chip shows the
  focus ring. The bell's `data-action` is `notifications`, as in the other two.
- Changed: the shell lays itself out with the breakpoint Stator and Exciter
  share (#341). `--breakpoint-shell` in the stylesheet, the same 768px as the
  drawer's switch in code, sets the pages' padding, so the portal's pages are
  as narrow on a phone as the app's; the portal's header fits a phone, and the
  drawer slides in unless less motion is asked for.
- Changed: the shell has the landmarks and states Stator's and Exciter's have
  (#353). A "Skip to the content" link, shown when the keyboard reaches it,
  moves focus past the navigation to the page. The rail's toggle says whether
  the sidebar is out and names it, a folded sidebar group stays in the page
  hidden so its title names it, each tab names the panel it switches, and the
  rail, sidebar and narrow strip are left off a printout.
- Changed: Armature shows its logo the way Stator and Exciter show theirs
  (#340). Sign-in and the two-step setup show the logo above the name on the
  app's backdrop, the sidebar head and the shared dashboard show the gopher's
  face beside the name, and the browser tab, touch icon and a new web app
  manifest use the same face. `make branding` makes them all from
  `docs/armature-logo.jpg`, and nginx serves the manifest with its own type.
- Added: the plan lines its dates up with its dependencies (#345 to #352).
  Auto plan in the plan's toolbar previews where each issue would move, drawn
  as ghost bars with a list of the moves, so that every blocked issue starts
  after what blocks it, keeping its length in its team's working days. Only
  later, never earlier; done and started issues stay where they are. Apply
  saves every move in one go or none, refusing with a sentence when an issue
  moved since the preview, and the toast offers to undo it in one step. The
  preview can also place undated issues from their estimate, a Blocks link on
  a parent holds back every issue beneath it, and dragging a blocker later
  offers to move what it blocks along. `POST /projects/{key}/plan/align` and
  `PUT /projects/{key}/plan/schedule` do the same over the API, and the MCP
  tools `align_plan` and `apply_plan_schedule` offer it to an assistant.
- Added: a partner such as Rotor clones a project's repository and pushes an
  issue's branch through Armature, as the person, while the host's token stays
  in Armature (#333). The git proxy answers git's smart HTTP at
  `/api/v1/git/{projectKey}/{owner}/{name}.git` with an API token or a
  connected app's token, as a bearer or as the password. A clone needs `read`;
  a push needs the new scope `git.write` and the right to edit the issue, and
  goes only to a branch made for an issue, never to the default branch, as a
  tag, a deletion or a force push, each refused with a reason git prints.
  Pushes are capped in size and rate per token, and each is in the audit log
  with the person, the app and the refs. `/meta` lists `git-proxy@1`.
- Added: a partner such as Rotor works on an issue's code over MCP, as the
  person (#332). `list_repositories` and `get_issue_development` read a
  project's repositories and an issue's branches, pull requests, commits and
  CI runs; `create_issue_branch` makes the issue's branch and
  `open_pull_request` opens its pull request into the default branch on
  GitHub, GitLab or Gitea, with the repository's token, recorded on the issue.
  Both need `issue.write` on the issue. A pull request is opened over HTTP at
  `POST /issues/{issueKey}/pull-requests`, and a branch or pull request names
  its repository by owner/name as well as by id. `docs/integration.md` lists
  the tools.
- Changed: a branch is only made in a repository of the issue's own project,
  and never named for the default branch, which the database keeps from being
  any issue's branch. A repository without an access token says so in a
  sentence that says who adds one.
- Added: `/meta` lists `mcp@1`, so a partner such as Rotor finds Armature's
  MCP endpoint by name before it acts in it as a person, with an API token or
  a connected app's token (#305).
- Changed: the menus are where Stator's and Exciter's are (#308). The avatar
  opens "Your account" whether the sidebar is open or folded: your name,
  address and seat, then Your profile, Notification settings, Watching, Themes
  and Access tokens (until now "API tokens"), the connected tools, Settings for
  administrators and Sign out. Settings leaves the sidebar for a settings area
  at `/settings` with a sidebar of its own, You and then Settings, headed by
  "Back to Armature"; somebody who does not administer sees no Settings group,
  card or palette entry. The sidebar keeps the work, with Saved searches, and
  Inbox is now Notifications, with the unread count, while its address stays
  `/inbox`. The bell opens the latest notifications with Mark all as read, See
  all and Notification settings. The palette reaches every page of the
  sidebar, the avatar menu and the settings area. Below 768px wide the rail and
  sidebar give way to a drawer, opened from a strip at the top.
- Added: `watcher` in a search, such as `watcher = currentUser()`, which is
  what Watching opens (#308).
- Added: the avatar opens the "Your account" menu whether the sidebar is open
  or folded, and its "Switch to" group links Stator and Exciter when the
  installation has a connected app for them: one whose Keycloak client is
  `stator` or `exciter`, or that has the tool's name, at the app's address.
  Each is a plain link, shown to everyone signed in; with none connected the
  group is not shown. `GET /installation/tools` lists them (#327).
- Fixed: `make test`, `make release` and the other targets that mount a
  cache work in a fresh clone. They create `.cache/gocache`, `.cache/gomod`
  and `.cache/npm` as the caller first, where Docker made them owned by root
  and the build then could not write to them (#323).
- Added: an issue's description and its comments are written in Markdown
  through the API. Creating and changing an issue take `descriptionMarkdown`
  and a comment takes `bodyMarkdown`, beside the document fields, which may
  not be sent with them. Headings, lists, code, quotes, emphasis and links
  come through as the editor shows them, and the rest as text; an empty
  Markdown description clears it (#329).
- Added: the suite demo, the GitHub issues of Armature, Exciter and Stator as
  the projects `ARM`, `EXC` and `STA`, each under its GitHub number, so
  Exciter's and Stator's suite demos link to them (`EXC-261` is Exciter's
  issue 261). It keeps the issues' bodies, labels, milestones and closing
  dates, and turns "Depends on" into blocking links, across projects too.
  `make seed` adds it, and an administrator adds it under Settings,
  Installation, which the worker builds in the background.
  `GET` and `POST /installation/suite-demo` say how far it got and add it.
  The issues come from a snapshot in the repository, which
  `make demo-snapshot` takes again (#300).
- Added: the chart sets a real installation up without anybody signing up.
  With `jobs.bootstrap.enabled`, a Job after every install and upgrade makes
  `bootstrap.admin.email` the installation's owner, or adds it as an
  administrator to one set up before, with the password from a Secret the
  chart generates (`<fullname>-bootstrap-admin`) or one you name. It also lets
  `bootstrap.members` in as admins or members before their first sign-in.
  Sign-up is then closed by default, so nobody who reaches a new
  installation first can own it. A rerun changes nothing, and a password
  changed in Armature is kept (#324).

## 0.1.0 - 2026-10-21

- Fixed: installing the chart with `values-demo.yaml` no longer fails now
  and then at the seed Job. The seed asked once for the attachment bucket,
  and on a fresh install SeaweedFS may still be starting and refuse it; the
  seed now gives the object store two minutes, as it gives Gitea one. A seed
  run again on an installation it made, by the Job's retry, a second
  `make seed` or an Argo CD sync, tops it up again instead of stopping with
  "set up by somebody other than the demo's Ada" (#314).
- Fixed: the backend takes `golang.org/x/net` v0.60.0, which fixes four
  advisories in the HTTP/2 server it serves with: a crash from an HPACK
  encoder race (GO-2026-6617), a double flow control refund (GO-2026-6612),
  CPU spent on repeated window changes (GO-2026-6611) and memory spent on
  trailer headers (GO-2026-6603). `x/crypto`, `x/sync`, `x/sys` and `x/text`
  move up with it (#313).
- Added: GitHub runs the tests, as it does for Stator, Exciter and Rotor. The
  CI workflow runs `make test`, the web tests and typecheck, the chart's lint
  and golden render, `make test-integration` and `make test-e2e` side by side
  on every pull request, on main and nightly. The Chart install workflow
  installs and upgrades the demo chart in a kind cluster
  (`make test-chart-install`), and the Deps workflow looks for known
  vulnerabilities in the Go and npm dependencies (`make audit-deps`) (#311).
- Fixed: a dialog taller than the window, such as New issue with many labels,
  scrolls like a page. The wheel, touch or keys move it wherever they land,
  over its title, its buttons or the dimmed page around it. Its title and
  buttons stay pinned to the window's edges, and on a window too short for
  that they scroll along with the form (#294).
- Fixed: installing the chart with the bundled Postgres no longer fails at
  the roles Job, whose SQL reached Postgres as `DO $` because Kubernetes
  reads `$$` in a container's arguments as an escaped `$` (#306).
- Added: an administrator may let people in by their groups, under Settings,
  Access, Sign-in (`admitByGroup` on `PUT /api/v1/oidc-provider`, off by
  default). Somebody single sign-on vouches for who has no standing yet, and
  whose provider group holds a role, is then let in as a member with that
  group's roles instead of asking to join, both at sign-in and when Stator or
  Exciter exchanges their Keycloak token, so a partner connects somebody who
  never opened Armature. Anybody in no such group still asks to join, and
  somebody switched off is refused. The audit log records each as
  `member.admitted` with the group (#301).
- Added: `/meta` lists `oauth.token-exchange@1`, so Stator and Exciter can
  tell from the capabilities whether to trade a person's Keycloak token for a
  grant at sign-in. It promises the token exchange at `POST
  /api/v1/oauth/token` that came with connected apps, answering an access
  and a refresh token within the person's rights and never
  `webhooks.partner` (#302).
- Fixed: a connected app keeps the scopes a person gave it when the same
  person signs in to it again through Keycloak or allows it with fewer scopes.
  A new consent or exchange adds to the grant's scopes instead of replacing
  them, so the app's `webhooks.partner` survives; a narrower `scope` on
  refresh still narrows the grant (#298).
- Added: a connected app an administrator allowed sets up its own webhook,
  so no secret is copied by hand. With a grant holding `webhooks.partner` of
  somebody who administers Armature, it makes one endpoint at
  `/api/v1/webhooks/partner`, its secret shown once, and changes, tests,
  rotates and removes it; its topics are named, never everything. Settings,
  Webhooks marks it with the app's name, and an administrator may switch it
  off, test it or remove it but leaves its address, topics and secret to the
  app. It goes with the app. `/meta` lists `webhooks.partner@1` (#281).
- Changed: a webhook's address is judged by the outbound guard when it is
  set, not on its first delivery: one inside Armature's network is refused
  with 422 `outbound_refused`, naming what to add to
  `ARMATURE_OUTBOUND_ALLOW`, and one that does not resolve with 422 `invalid`.
- Added: partner tools such as Stator, Exciter and Rotor connect a person in
  one click, with their consent. Armature is an OAuth 2.0 authorization server
  for them: authorization code with PKCE `S256`, rotating refresh tokens,
  revocation, token exchange for a narrower and shorter token or for a token
  of the shared Keycloak realm, and both metadata documents under
  `/.well-known`. The consent page at `/oauth/authorize` says what the app may
  do in words; a grant is one row in the person's token list, its scopes
  narrowing what they may do, and Disconnect ends it. Administrators add apps
  under Settings, Connected apps, or allow one that registered itself;
  `ARMATURE_OAUTH_REGISTRATION=closed` (chart `oauth.registration`) keeps
  registering to them. `/meta` lists `oauth.partner-tokens@1`, and
  [docs/oauth.md](docs/oauth.md) is the contract (#280).
- Changed: the workflow library, its schemes, the Roles tab and the labels
  page come a page at a time, each with a search; the library's and the
  labels page's search and page are in the address, and a link to one
  workflow opens the library searched for it. A project's workflow page and
  the scheme editor pick a workflow, and the project's scheme, by searching
  for it rather than listing them all, and a new mapping names no workflow
  until one is chosen. `GET /api/v1/workflows`, `/workflow-schemes`,
  `/role-assignments` and `/labels` answer 50 unless `limit` asks for up to
  200, with `total`, `limit` and `offset`, and take `q`; the grants also take
  `role`, and `/labels` with neither `q` nor `limit` no longer lists every
  label. `GET /workflow-schemes/{id}` reads one scheme, and
  `GET /projects/{key}/workflows` names the project's scheme and the default
  (#276).
- Fixed: the swimlane editor offers only the statuses of the workflows the
  project follows, and its "states not on the board" warning counts only
  those. A lane given a status no workflow of the project holds is refused
  with a sentence naming it; one it already holds it keeps. The new
  `GET /projects/{key}/statuses` lists a project's statuses (#277).
- Changed: the portal answers with a shape of its own, holding what a
  customer reads. A request is its key, summary, description, status and its
  category, priority, request type, team, company and whether it is shared,
  its dates and `commentCount`, the replies the customer can read. The
  assignee, the reporter, the authors of replies and the people who attached
  a file are named by `id`, `name` and `avatarUrl`. `GET /portal/requests/{key}`
  answers `{request, comments, throughCompany}`, the request type's name
  inside `request`. The followers are `userId` and `name`, and `email` only
  for the reporter, for the people they added (#287).
- Changed: every place that picks a person searches for them as you type
  instead of loading everyone: an issue's assignee and reporter, the new issue
  dialog, watchers, bulk edit, a component's lead and default assignee, a
  role grant, the group and team cards, a project's lead, a dashboard's
  assignee filter and each name of an import. Mentions ask the server for the
  name being typed. The users page and the access page's members come a page
  at a time with a search over the name and the address, kept in the address,
  as is the access page's tab. `GET /api/v1/members` and `GET /api/v1/users`
  answer 50 people unless `limit` asks for up to 200, with `total`, `limit`
  and `offset`, and take `q` to find people by part of a name or an address,
  those whose name starts with it first; `staff=true` leaves customers out of
  the members, and the assistant's `list_members` pages the same way (#274).
- Fixed: a space typed into the project switcher's filter no longer closes
  it, so a project named in two words can be found (#283).
- Fixed: a label field no longer draws every label everyone has coined. The
  new issue dialog, the issue page, bulk edit's Add labels, a rule's Add a
  label action and the project issue list's label filter show none until
  something is typed, then suggest the labels holding it, those starting with
  it first, and offer to create the typed word. Arrow keys move through them,
  Enter or a comma adds one, and Backspace in an empty box takes the last off.
  `GET /labels` takes `q` and `limit` for it; without either it lists every
  label as before (#273).
- Fixed: a dialog taller than the window, such as New issue with the built-in
  arrangement on a laptop, keeps its title and buttons in view and scrolls its
  fields between them; an error it shows sits just above the buttons and is
  scrolled to. Menus, popovers and the lists of people to mention scroll too,
  so the project switcher reaches its last project on a short window (#272).
- Added: a service desk groups its customers into companies, on the desk's
  settings page, by address or by domain: a customer at a company's domain
  joins it with their first request there. Raising a request, a customer may
  share it with their company, whose members find it under "Shared by" in the
  portal and may read it, reply and attach; they never see notes and are not
  mailed about it. Agents see the company on the request and in the queue,
  narrow the queue to one company, and query `company = Globex` in search and
  reports. The export lists a customer's companies and erasure takes them out
  (#78).
- Added: customers raise a new request by mailing the desk. Each desk is
  reached at a plus address of the inbox, `support+cp@` for `support@`, shown
  on the service desk page and in the portal. A desk that names a request type
  for mail turns a new mail there into a request by its sender: the subject is
  the summary, the words the description, and the files come along. Machines,
  agents, forged senders and domains the desk does not trust raise nothing; a
  desk without a mail request type answers as before. Upgrading: the api reads
  `ARMATURE_MAIL_INBOX` too, to show each desk its address (#72).
- Added: a desk's business hours take their days off from a holiday calendar,
  chosen under Holidays from on the service desk page, beside the desk's own
  extra days. The clocks skip its days, keep the first half of the open hours
  on a half day, and follow a change to the calendar from then on. A holiday
  calendar a desk uses is not deleted; the refusal names the desks, as does
  the confirmation on the Holidays page. `GET` and `PUT
  /api/v1/projects/{projectKey}/business-calendar` carry `holidayCalendarId`
  and `holidayCalendarName`, and a holiday calendar carries `desks` for an
  administrator (#77).
- Added: a kanban board leaves out work finished more than a number of days
  ago, set per board beside its swimlanes, 0 keeping it there. The board says
  how many finished issues it left out and links to a search for them, and a
  swimlane's limit counts only the cards it shows. A done issue with no
  resolved date leaves by its last change. Upgrading: existing kanban boards
  start leaving out work finished more than 14 days ago; set 0 on a board to
  keep it (#54).
- Changed: the projects list comes a page at a time, with a search over the
  key and the name; the page, the search and Show archived are in the
  address. `GET /api/v1/projects` answers 50 projects unless `limit` asks for
  up to 200, with `total`, `limit` and `offset`, and takes `q` to find
  projects by key or name; the assistant's `list_projects` pages the same way.
  The sidebar's project switcher, the command palette and every project
  picker ask the server for what was typed instead of loading every project,
  so an installation of thousands stays fast (#261).
- Added: mail goes through a relay that wants a login, such as SES, Microsoft
  365 or Gmail. `ARMATURE_SMTP_USER` and `ARMATURE_SMTP_PASSWORD` are sent as
  PLAIN over TLS only, and `ARMATURE_SMTP_TLS` is `starttls` (the default, as
  before), `implicit` for port 465, or `none`. A refused login is logged once
  as a sentence naming the settings to check, and the mail is tried again.
  The chart takes `mail.smtpUser`, `mail.smtpTLS` and `secrets.smtpPassword`
  (#73).
- Changed: the search page offers as chips only the searches you starred or
  made, and the one running; what others share is on All saved searches
  until you star it, so a few dozen shared searches no longer push the
  results down the page (#262).
- Added: an archived project can be restored from its row in the projects list,
  with Show archived on, and from its settings page, whose forms stay closed
  until it is. Archiving no longer asks first; its toast offers Undo, which
  restores the project. The audit log records who archived and who restored a
  project (#112).
- Added: the type in an issue's header is a picker for anybody who may edit
  it, offering the other types at the same level. When the new type's
  workflow has no place for the issue's status, the page says which status
  it moves to and asks first; a refusal is shown as the server's sentence,
  and the history reads "changed the type from Task to Bug" (#114).
- Added: quick filters above a board and a sprint's board: Only mine, an
  assignee, label, type or epic, and a box matching a card's key or summary.
  They narrow the cards already loaded and say how many they hide; a lane's
  count reads "3 of 7" while filtered, since its limit still counts every
  card. The filter and the board on screen are kept in the address, so a
  filtered board can be linked (#63).
- Added: `make release` reads the next version from the commits since the
  last tag when none is given, and prints the rule it applied and the commits
  that decided it; `DRY_RUN=1` prints that and the notes and changes nothing.
  It refuses a checkout off `main`, with changes, or behind or ahead of
  `origin/main`, and opens a pull request for `release/vX.Y.Z` instead of
  merging into main itself. `make release-tag` tags that merge with the notes
  and pushes the tag, and on the tag the Release workflow publishes the images
  to `ghcr.io/cloudster1/armature-<image>`, the chart to
  `oci://ghcr.io/cloudster1/charts` and the GitHub release, and tells Dynamo.
  The chart pulls those images by default (#240).
- Fixed: what somebody types in a project's settings stays when the project
  is fetched again, for instance right after the previous save; a field nobody
  touched still follows what the server has (#252).
- Changed: the stack's Keycloak imports the realm `dynamo` from
  `deploy/keycloak/realm.json`, the same file Stator and Exciter import, in
  place of the realm `demo`. Its people are alice, bob, carol and dave, each
  with the password `<name> password`; Ada, Grace, Alan and Rita sign in with
  their local accounts only. The seed points single sign-on at the new realm
  with the development secret `armature-dev-secret`, and a unit test refuses a
  seed naming what the realm does not hold. `make realm-check` compares the
  realm with Dynamo's canonical copy (#241).
- Upgrade note (#241): `make up` imports the new realm. Run `make seed` again,
  or set the identity provider's issuer to
  `http://localhost:8180/realms/dynamo` and its secret to
  `armature-dev-secret` under Settings, Access (#241).
- Added: branches are named `<type>/<issue>-<slug>` and commit subjects are
  Conventional Commits, so the release script can read the next version from
  them. `make hooks` has git refuse a push outside the convention and the
  Conventions workflow refuses such a pull request, each saying what to write
  instead; `make release` cuts `release/vX.Y.Z` (#243).
- Added: `GET /api/v1/meta` lists the integration capabilities a partner checks
  for by name: `issue-embed@1`, `remote-links@1`, `webhooks@1`,
  `theme-export@1`, `reports@1`, `plan@1` and `calendar@1`, each with what it
  promises in `docs/integration.md`. A test refuses a capability naming an
  operation or a topic the build does not have (#239).
- Fixed: `api/openapi.json` gives each public operation, `/meta` among them,
  an empty `security`, so a generated client calls it without credentials
  instead of reading it as needing a session or a token (#239).
- Fixed: ticking a row on the search page no longer moves the rows. The bar
  for the ticked issues opens below them and stays at the foot of the screen,
  so a second tick lands on the row it was aimed at.
- Added: `GET /api/v1/meta` says which build answers, as Stator and Exciter
  do, without a sign-in: the product, the version, the API's own version and
  its capabilities, for now none. The commit and when it was built are added
  for somebody signed in. The foot of the sidebar reads it (#238).
- Deprecated: `GET /api/v1/build`, read `/meta` instead. It keeps answering
  behind the session for at least one more release (#238).
- Added: a webhook's envelope carries its version, `version`, and the
  `X-Armature-Webhook-Version` header says the same, so a receiver can refuse
  one it does not know before parsing it. An endpoint is registered for a
  version, the current one, 1, unless `POST /webhooks` names another, and an
  edit keeps it; a retry or a redelivery keeps the version its body was built
  in. The envelope, its headers, the versioning rule and every topic's payload
  are in `docs/webhooks.md`, and the `webhooks` section of the OpenAPI
  document describes the delivery (#242).
- Added: `issue.transitioned` names the categories of the statuses it moved
  between, `fromStatusCategory` and `toStatusCategory` (`todo`,
  `in_progress` or `done`) beside `fromStatus` and `toStatus`, so a
  subscriber keeps an issue's category without reading it again. Nothing is
  taken away, so a subscriber that ignores them goes on as before;
  `docs/webhooks.md` lists the topic's fields (#224).
- Added: a remote link says what it is to its issue. `relation` on a
  remote link, its request and the `issue.remote_link.added` and
  `issue.remote_link.removed` payloads is `page` or `requirement`; a new
  link sent without one is a page, and a resend that leaves it out keeps
  the one the link has. The issue page heads requirements apart from pages,
  pages first, and its rail names the section References. `/meta` lists
  `remote-link-relations@1`, so a partner can tell before it sends one (#223).
- Changed: the organization's tables are named for the installation. `org`
  is `installation`, `org_role` is `role`, `org_invite` is `invite` and
  `org_join_request` is `join_request`, with their constraints, indexes,
  triggers and functions. A membership is the account's standing,
  `app_user.standing`: owner, admin, member, customer, or NULL for somebody
  who is no member, and `org_member` is gone. An invitation's and a role's
  `org_role` and `org_wide_only` are `standing` and `installation_wide_only`.
  `/auth/me` names the installation, its id and name (#218).
- Deprecated: the `org` of `/auth/me`, which names the same installation,
  read `installation` instead. It goes in the next release, together with
  the deprecated `orgId` of invitations and the redirects from the old
  addresses. The `orgId` of a webhook's envelope and of the `member.joined`
  event goes with envelope version 2, and an endpoint registered for version
  1 keeps getting it for at least one Dynamo release (#218, #242).
- Upgrade note (#218): a report, export or script that reads the database
  directly and names `org`, `org_member`, `org_role`, `org_invite` or
  `org_join_request` breaks with this release. Read `installation`, `role`,
  `invite` and `join_request`, and a person's standing from
  `app_user.standing`, where no membership is NULL. The upgrade stops,
  names what it found and changes nothing at a view, trigger, rule or key of
  yours on `org_member`, and at a function of yours that names an old
  table; change or drop them first. Taking the upgrade back makes
  `org_member` again from the standings and every name as it was.
- Changed: the tables no longer carry the organization. `org_id` goes from
  every table, and so do the two session columns an earlier release emptied
  and the functions that answered for them. The installation's own row is
  kept to one, and its five built-in roles come from `builtin_roles()`
  (#218).
- Upgrade note (#218): the upgrade stops, names what it found and changes
  nothing at a second organization, at anything else that depends on an
  `org_id` column, such as a view or an index you made, and at a function of
  yours that reads one. Drop or change them first. A report or export that
  reads the database directly and names `org_id` breaks with this release;
  take the column out of it. Taking the upgrade back adds the columns again,
  filled with the installation's id.
- Changed: no query names the organization any more. Rows are written
  without `org_id`, which the installation fills by default, no filter asks
  for it, and the code passes no organization's id along to reach the
  installation's rows. The global administrator's built-in description says
  it administers the installation, where it said the tenant; one an
  administrator rewrote is kept (#218).
- Changed: the api and the worker no longer carry the organization. No
  transaction is bound to one, a row written without `org_id` is the
  installation's by default, the worker's passes run once over the
  installation instead of once per organization, events name no
  organization on their way through the stream, and the request log has no
  `org` field (#218).
- Deprecated: the `orgId` of a webhook's envelope, of the `member.joined`
  event's payload and of an invitation in the API. Each holds the
  installation's stable id, the same id it held before; read nothing from
  them. A later release removes an invitation's. A webhook's go with
  envelope version 2, and an endpoint registered for version 1 keeps
  getting them for at least one Dynamo release (#218, #242).
- Upgrade note (#218): drain the outbox and the event stream before
  upgrading, and do not run workers of both releases side by side. The
  previous release's worker skips an event this release relayed, because it
  names no organization. Stop the api, let the worker run until
  `armature_outbox_pending`, `armature_stream_lag` and
  `armature_stream_pending` are 0 for every group, stop it, then upgrade.
- Changed: the database's triggers keep their rules without the
  organization. Each still holds a row to its project, its service desk or
  its member, and none reads `org_id` or fires on it changing; the nine that
  held a row to its organization alone are gone. Deleting an account that
  granted itself a role no longer trips the role assignment's trigger, and
  the database's refusals say "here" or "the installation" where they said
  "the organization" (#218).
- Changed: names and keys are unique in the installation rather than in each
  organization: a project key, and so an issue key, names one project, and
  the keys and indexes of the schema no longer carry `org_id`. The upgrade
  checks first and stops at rows that would share a key, naming them; with
  one organization there are none. A desk's address can no longer answer
  `desk_ambiguous`, and a mail once recorded as `ambiguous` is now
  `unmatched` (#218).
- Changed: the database keeps no row level security. Its policies kept
  organizations apart, and an installation holds one; the walls inside it,
  projects, the desk's customers and the owners, stay in the api and in the
  triggers, as they were. The upgrade drops only the policies Armature made
  and stops at any other, naming it, so drop or move a policy you added
  yourself before upgrading (#218).
- Changed: the integration suite works in the one installation instead of
  an organization per test. A test makes its own people, project keys and
  names, removes them when it ends and asserts only on what it made; the
  tests that proved nothing but organizations apart are gone, and those that
  change something everybody lives with run alone and put it back. The audit
  log records an act whose actor is gone by the time the worker copies it,
  without the actor, instead of retrying it until it is set aside (#217).
- Upgrade note, for an installation that had organizations (#208): the
  upgrade refuses a database with several and names them, so delete the
  extra ones with the previous release first (#210). Bookmarks and links to
  `/api/v1/auth/oidc/{slug}/start`, `/desk/{slug}?desk=KEY` and
  `/settings/organization` lead to `/api/v1/auth/oidc/start`,
  `/desk?desk=KEY` and `/settings/installation` until the next release, which
  removes them; point them at the new addresses before then. What the
  identity provider has registered stays right. `ARMATURE_NAME` names an
  installation only when its first account sets it up, so an upgraded one
  keeps its organization's name until an administrator renames it under
  Settings, Installation. `ARMATURE_SIGNUP` now defaults to `first`; set
  `open` to keep sign-up open after the first account (#216).
- Changed: the README, SECURITY.md, the privacy record, the design notes and
  the chart describe one installation. The seed's messages speak of the
  installation it fills, the chart's notes say what was seeded or that the
  first person to sign up sets the installation up, and the compose stack
  hands `ARMATURE_NAME` to the seed as well as the api, since either may make
  the first account.
  `docs/decisions.md` adds "One installation is one team's tracker" and marks
  the entries it supersedes (#216).
- Changed: Armature says "everyone" or "all projects" where it said "the
  organization", on screen, in the guide, in error messages and in the API
  document; only the Installation page names the installation. The settings
  hub's second group is Everyone. A project follows "the default" workflows
  and arrangement, and a scheme or theme is made the default for everyone.
  The permission `org.administer` keeps its key and reads "Administer
  Armature". `GET /themes/active` answers `source: "everyone"` and an
  arrangement's `origin.scope` is `everyone`, where both said
  `organization`. The API document's `organization` tag is split into
  `installation`, `users`, `issues` and `workflows`. The audit log keeps the
  stored actions `org.created` and `org.second_factor_required`, so the export
  and a filter by action still name them, while the page reads them as
  `installation.created` and `installation.second_factor_required`. The mail
  to an account that signs in by single sign-on no longer asks for an
  organization (#215).
- Fixed: the browser suite's mention scenario names the person by the unique
  word of their name. In the shared installation "ben" matched more people
  than the picker lists, so the newest Ben was never offered.
- Fixed: a key typed straight after picking a word in the search bar stays
  where it was typed. The caret was put back after the picked word a frame
  later, so typing "to" quickly could come out as "ot".
- Changed: the service desk's door is named by the desk. Its address is
  `/desk?desk=KEY`, or `/desk` for the installation's, and the API follows:
  `GET /desk?desk=KEY` returns the installation's name and the desk with
  whether its door is open, and `POST /desk/codes` and `POST /desk/sessions`
  take `desk` in the body. The old `/desk/{orgSlug}` addresses answer 308 to
  the new ones, and the web page sends the browser on, until the next release.
  A portal code is no longer bound to the organization, so codes mailed before
  the upgrade stop working; they lived ten minutes, and asking again is enough.
  The code mail names the desk it was asked at, the rating page's `orgName` is
  now `desk`, and a reply by mail no longer ends as `ambiguous` (#214).
- Fixed: `make seed` runs to the end again. The demo's sprints belong to the
  Platform team, and since a sprint keeps to its own team's work the seed
  stopped at the first sprint; it now commits only Platform's work to them.
  The browser suite's holiday scenarios also pass on a seeded installation:
  they empty the default calendar first and put its days back afterwards.
- Changed: the organization settings page is now Installation, at
  `/settings/installation`. Administrators rename the installation there,
  and the sidebar's head shows the new name at once; members read it.
  `/settings/organization` leads there until the next release (#213).
- Added: "Add the demo project" on the Installation page and `POST
  /installation/demo-project`, for administrators. It fills the installation
  with a made-up plant, a production line (`LINE`) and its maintenance
  (`MAINT`), and records `installation.demo_added`. It runs once: a second
  time, or with a project of the installation's own under either key, it is
  refused with 409 before anything is made. Unlike the demo organization it
  replaces, it leaves the holiday calendars alone (#213).
- Removed: deleting the organization, with `DELETE /organization` and its
  dialog, and the `org.created` event. A webhook subscribed to `org.created`
  keeps working, but has to drop it before it is saved again (#213).
- Changed: signing in lands in the installation. Nobody is asked which
  organization, the sidebar shows the installation's name without a switcher,
  and the profile lists no organizations. `POST /auth/switch-org` is gone, and
  so are `organizations` on `/auth/me` and `orgName` on `/auth/sessions`. A
  session names no organization and migration `00931` holds the database to
  that; the `session_bound` refusal is gone. A session through an open desk
  door may not export or erase the account behind the address (#212).
- Changed: single sign-on starts at `/api/v1/auth/oidc/start`, at the
  installation's one provider, and the sign-in page offers "Continue with
  single sign-on" without asking for an organization. The old
  `/auth/oidc/{orgSlug}/start` sends the browser to the new address until the
  next release. The callback address is unchanged (#212).
- Changed: the second-factor rule moved from `/organization/second-factor` to
  `/settings/second-factor`, with no stand-in at the old address, and reads
  off, for administrators or for everyone. A held session is offered setting
  it up or signing out (#212).
- Removed: "Create a demo organization" and `POST /organizations/demo`, which
  moved the session into the organization it made; #213 brings the demo back
  as a project in the installation (#212).
- Changed: accounts belong to the installation. Sign-up asks for the person
  alone: on an installation with no organization yet, the first account sets
  it up, named by the new `ARMATURE_NAME` (`Armature` unless set), and
  administers it. After that `ARMATURE_SIGNUP` decides: `first`, now the
  default, closes sign-up; `open` lets people sign up as members; `closed`
  never offers it. `POST /auth/signup` refuses `orgName` and `orgSlug`, and
  `GET /auth/signup` also says whether the next account sets the installation
  up. Invitations lead into the installation and their mail and page name it;
  the invitation preview's `orgName` is now `installation`. The development
  stack keeps sign-up open (#211).
- Changed: letting a member go switches their account off. `DELETE
  /members/{id}` ends their sessions and tokens and keeps the account, which
  switching on brings back, and "Remove from organization" is gone from
  Access. An administrator renames, resets and switches off every account,
  not only those in no other organization, and a switched-off account is told
  "Your account has been switched off. Ask an administrator." (#211).
- Changed: the installation keeps an active administrator rather than an
  owner: the last one cannot be switched off or erase their account, refused
  with `last_administrator`, and migration `00930` holds the database to the
  same. Pictures are seen by everybody signed in, API tokens are the
  person's, and acts on an account are recorded once (#211).
- Changed: the browser suite runs as the seed's Ada unless
  `E2E_ADMIN_EMAIL` and `E2E_ADMIN_PASSWORD` say otherwise, and on an
  installation nobody has set up it signs her up through the sign-up page
  first (#211).

- Changed: nobody is signed in without the organization. A password
  sign-in, its code step or an API token of somebody with no membership left
  is refused with "Your account has been switched off. Ask an
  administrator.", and an open session ends with the membership instead of
  showing a page that says there is no organization. The `no_organization`
  error is gone (#210).
- Added: `GET /installation` tells anybody signed in what the installation is
  called, and `PUT /installation` lets its administrators rename it, in the
  audit log. Its name and the settings there is one of, the default theme,
  the second-factor rule and the automation account, are read and written in
  one place, ready to move off the organization (#210).
- Upgrade note: an installation now holds one organization, and the upgrade
  refuses a database with several. The migration stops, names them (the first
  ten by address and name, and how many more) and changes nothing. Delete the
  extra ones with the previous release, under the organization's settings,
  and upgrade again; nothing merges them, because their project keys,
  statuses, roles and people would collide (#210).
- Changed: the browser suite keeps its scenarios apart without an
  organization each. A run sets up one administrator, who makes the person
  each scenario signs in as; projects take unique keys and shared things
  unique names. Scenarios that change what everybody shares run serially
  after the rest and put it back, the runner refuses such a change from the
  parallel pool, and `SHUFFLE=1` runs the pool in a random order (#209).
- Fixed: entries on the calendar open. A click on an issue opens it in the
  drawer beside the calendar, whose arrows walk the month's issues, while a
  drag still only moves its dates; a sprint opens its board, a milestone the
  milestones and a version the releases (#120).
- Added: issues are exported from where they are. An issue's More button
  has Export as CSV for that issue, readers included; the bar over ticked
  rows exports them, before any change is chosen and past the bulk limit;
  and a project's issue list exports every page of what it shows. All open
  the same column choice as search's Export (#121).
- Added: a comment's author edits it in place or deletes it, and whoever
  administers the project may delete anybody's; a work log entry is
  corrected in place, its duration, day and note together. An edited
  internal note answers as one again, a comment is found only on the issue
  its address names, and a missing one answers 404 instead of 422 (#113).
- Added: the profile lists where you are signed in, each device with its
  address, organization and when it was last active, and signs one out or
  every one but this; no password change is needed, so an account the
  identity provider signs in can do it too. Both are in the audit log
  (#106).
- Added: two-step sign-in. A person sets up an authenticator app from their
  profile, by QR code or by typing the key, confirms it with a code and gets
  ten single-use recovery codes, shown once; from then on a password sign-in
  asks for the app's code or a recovery code, with five tries per sign-in and
  a brake of its own. An organization can require it of its administrators or
  of everyone under Access, Sign-in, and a session without it is held to
  setting it up; single sign-on relies on the provider's own second step where
  the organization trusts that provider. An administrator can turn it off for
  somebody who lost both phone and codes. The key is sealed with the new
  `ARMATURE_SECRET_KEY`; without one, setting it up is refused (#105).
- Fixed: swimlanes reordered under Configure swimlanes stay where they were
  put. A project whose oldest board had gone to a team opened on that board
  while the panel edited the project-wide one, and a second arrow press
  before the first had landed sent the order from before it; the board a
  project opens on is now always the first it lists, and the arrows wait
  for a move to land (#119).
- Added: search and a project's issue list page through every issue they
  match, a hundred at a time, with the page kept in the address; the
  project's list says how many of the total it shows, and a bulk selection
  says it covers the page on screen (#90).
- Added: "Forgot your password?" on the sign-in page mails a link that sets
  a new password once, within an hour, and ends every session and API token
  the account holds; every address gets the same answer, an account the
  identity provider signs in is told to use it, and asking is braked per
  caller and per mailbox (#104).
- Changed: integration tests run in parallel; the ones that share the
  replica, Redis, the mailpit box, the per-address brakes or a pass over
  every organization run one at a time first, each listed with its reason,
  and `make test` refuses a test that is neither (#187).
- Fixed: saving notification preferences answers with what was saved, not
  the preferences from before, while a replica lags behind.
- Fixed: an API key used the moment it was made is no longer refused at
  `/auth/me` as an expired session while a replica lags behind.
- Changed: the integration suite opens its database cluster, superuser
  pools, telemetry and attachment bucket once, in `TestMain`, instead of in
  every test, and `TestMain` now refuses a run that left an operation
  unexercised; the suite runs in about 55s instead of 140s (#187).
- Added: `ARMATURE_DOOR_WINDOW` sets how long the brakes on portal codes,
  portal entries and password or invitation guesses remember an address,
  ten minutes unless set; the development stack uses one minute, so browser
  test runs in a row are not refused (#187).
- Changed: the browser suite signs up, signs in, and makes the projects and
  issues a scenario needs through the API instead of filling in forms, which
  saves most of its page loads; the scenarios about those forms still drive
  them (#187).
- The browser suite and `make screenshots` load the app from the production
  build behind its nginx and CSP, as it ships, instead of the Vite dev server,
  so a page load is a few files rather than every module, and a scenario
  fails if the CSP refuses anything on its pages; `E2E_WEB=dev` keeps
  the dev server for following a scenario through hot reload (#187).
- Faster tests: the browser suite waits for what it checks instead of
  sleeping, so the swimlane, drawer and mailed reply scenarios go on the
  moment the save, the key or the reply has landed (#187).
- Browser suite timings: a run ends with its ten slowest scenarios and its
  number of full page loads, and leaves each scenario's time and page loads in
  `.cache/e2e/timings.json`; `ONLY` also takes a scenario file's name, as
  `ONLY=board` (#187).
- Fixed: a scrum master or project administrator can change or remove only
  the absences they recorded themselves, never the person's own or an
  administrator's, and the person is told in their inbox, and by mail if they
  want it, whenever somebody else records, changes or removes their days
  (#39).
- Fixed: deleting a team that still has sprints or request types routed to
  it is refused with a sentence saying what to do, instead of handing its
  sprints to the project or failing; a team that has run a sprint is kept,
  and the database refuses the delete too (#55).
- Fixed: an issue handed to another team leaves the old team's open sprint
  for the new team's backlog, with a line in its history, instead of
  vanishing from the Sprints page while still counting in the sprint; another
  team's sprint is refused, by hand, on completion and by the database (#56).
- Fixed: an out-of-office or other automatic reply to a request is no longer
  posted as the customer's answer; the request stays Waiting for customer
  with its clocks held, and the mail is logged as automatic (#65).
- Fixed: a squash or merge commit no longer carries out the branch's smart
  commands a second time: a comment is posted once and a "#close" is not
  tried again, while a new command in the squash commit still runs (#68).
- Fixed: a merged branch the host deletes, as GitHub and GitLab do on
  merging, stays on its issue and says "merged, deleted"; an unmerged one is
  removed, and the same name pushed again is a new branch (#71).
- Fixed: deleting an epic or any parent issue now also removes the files of
  the stories and subtasks under it from storage, instead of leaving them in
  the bucket with nothing pointing at them (#86).
- Fixed: an issue moved more than once is still found at every key it has
  had, in the app, over the API and by a reply to a desk mail, not only at
  the key it left last (#88).
- Fixed: a reply by mail that could not be posted, say during a short
  database outage, is posted on a later poll instead of being deleted from
  the mailbox; one that fails ten times stays in the box and an internal note
  on its request tells the agents why (#94).
- Fixed: somebody in two organizations gets each one's digest, both at 08:00
  for a daily one; a digest whose hour was missed goes at the next pass that
  day, and a restarted worker neither sends one twice nor skips it (#97).
- Fixed: an organization made before an upgrade on a database migrated as a
  plain owner now has its default holiday calendar, its automation account and
  the Initiative type, like a new one; the upgrade adds them where missing
  (#98).
- Fixed: the Resources page opens on, and "Today" goes back to, the week of
  the reader's own date, also just after midnight on a Monday or on a Sunday
  evening away from UTC (#49).
- Fixed: importing an .ics file while a calendar's table holds unsaved days
  asks first ("Import and discard 3 unsaved days?") and keeps the typed days
  until the answer is yes and the import has arrived (#50).
- Fixed: a dashboard saved as a template no longer keeps the version a
  Releases or Release burndown widget was pinned to, so in another project
  they show its unreleased versions or ask for one; templates saved before
  read the same way (#57).
- Fixed: a request resolved without a public reply no longer misses its first
  response goal hours later; resolving completes every running clock, a
  breach that already happened is kept, and reopening restarts only the
  resolution clock (#66).
- Fixed: behind a load balancer, a person always sees their own latest change:
  an older write finishing last no longer lets a lagging replica answer their
  next read (#59).
- Fixed: a desk's business hours no longer take two spans of one day that
  overlap, and say which two to join; hours saved that way before count the
  overlap once, so goals no longer run out early (#76).
- Fixed: on a shared dashboard the Sprint history widget no longer offers a
  Curve button that only led to an error; the link shows the sprints and
  nothing a visitor cannot open (#58).
- Fixed: a description or comment started in the side panel stays with its
  own issue: stepping to another issue no longer shows it there or saves it
  there, and stepping back finds the draft where it was left (#109).
- Fixed: the project calendar shows holidays and absences on the days of the
  months before and after that its six weeks include, and an absence that
  runs past the grid is drawn as continuing instead of ending there (#47).
- Fixed: a burndown shows a dot on a sprint's first day, and a sprint started
  early or ended late keeps every day on the chart, with the first and last
  of them named under it (#61).
- Fixed: the release burndown counts an issue only from the day it was
  filed, so work added to a version later lifts the line instead of showing
  from its first day, and a released version's line stops on its release
  day (#60).
- Fixed: a pull request moves its issue only when it is merged; an edit, a
  label or a redelivery of an already merged pull request no longer moves a
  reopened issue to Done again (#67).
- Fixed: only the customer who raised a request finds the rating link in its
  resolution mail; followers and watching agents are told it is resolved
  without one, so nobody can rate it before the customer does (#79).
- Fixed: a CSV export holds every issue the search matches, up to 5000,
  instead of the first 50; past 5000 the file ends with a row saying it was
  cut and to narrow the query (#80).
- Fixed: searching a number or date custom field with ~ or !~ looks for the
  typed text instead of failing, and every operator on those fields answers
  even beside fields that hold words (#83).
- Fixed: a sprint completed while the hourly snapshot was running keeps the
  burndown's last point that completing it wrote, instead of ending at nothing
  remaining with its scope equal to the work done (#62).
- Fixed: a text search for `50%` or `snake_case` finds those characters
  rather than any 50 or any letter; a CSV file Excel saved with a byte order
  mark and quoted headers imports; and two people tagging with the same new
  word at once both get the label instead of one being refused (#92).
- Fixed: `text !~ "foo"` and `description !~ "foo"` now return the issues
  with no description, and every negation (`!=`, `NOT IN`, `NOT`) keeps the
  issues where the field is empty, such as `assignee != currentUser()`
  returning the unassigned ones (#87).
- Fixed: erasing an account takes the person off every watcher list instead
  of leaving "Former user" there, and "Watch the issues I report or am
  assigned" on the profile now decides whether those issues tell you what
  happens; off, only a watch, an assignment or a mention does (#107).
- Fixed: with read replicas, the invitations list shows an invitation the
  moment it is sent and drops it the moment it is withdrawn; a webhook's
  delivery log shows a test or redelivery with its outcome, and a project
  given its default dashboard on first sight shows it at once (#100).
- Fixed: an assignee, priority, summary, label or time changed in the issue
  panel shows at once on the board, the plan, the calendar, the resources and
  sprint planning beside it, instead of after the page is opened again (#110).
- Fixed: Escape closes only what is on top: a menu or a confirm closes and
  the drawer or dialog under it stays, focus goes back to what opened it, and
  the issue drawer no longer closes while a comment is being written (#111).
- Fixed: the work log starts on today in the profile's time zone, a share
  link set to expire on a day lasts to the end of that day there and reads
  back as that day, and dashboard week labels name the week's own Monday
  wherever the reader is (#115).
- Fixed: a read replica that is down when the api or worker starts joins the
  rotation once it answers, instead of being left out until a restart, and
  `/readyz` lists it, out of rotation, until then (#96).
- Fixed: a change the server refuses, such as an assignee, a priority, an
  archived project or a saved search, says why in a toast in the server's
  words, and a field that shows its own error still says it only once (#116).
- Fixed: a pull request's CI badge adds up every check on its head, so one
  failed check reads failed even when another passed after it, and the badge
  opens the list of checks behind it; a commit's badge reads the same (#70).
- Fixed: mail with umlauts or Japanese in its subject, sender name or text
  arrives as written, encoded so that every relay takes it, and the mails
  about one request share a thread in the customer's mailbox (#74).
- Fixed: the Sprints page shows every issue committed to a sprint however old,
  offers Delete only for a sprint the server says is empty, and lists the
  whole backlog in rank order without done work; the parent picker searches
  the project instead of offering its 200 newest issues (#53).
- Fixed: a customer's reply to a resolved request, in the portal or by mail,
  and from the reporter or a follower, reopens it and puts it back in the
  agents' Open queue with a fresh resolution clock; a workflow with no way out
  of done leaves it resolved and tells the agents in their inbox (#64).
- Fixed: changing a request's priority moves its open SLA clocks to the new
  goal and keeps the time already run; moving an issue out of a service desk
  stops its clocks, and moving one in starts the desk's (#75).
- Fixed: moving an epic to another project takes everything under it, its
  stories' subtasks too, each with a new key, its status and a history entry,
  and their old keys still find them; a descendant whose status the target
  does not have stops the move and is named (#81).
- Fixed: moving an issue to another project keeps its answers to organization
  fields; only those to the old project's own fields are dropped, each named
  in the history and listed in the move dialog before it is confirmed (#82).
- Fixed: when two people edit an issue's description or summary at once, the
  later save no longer erases the earlier one: it is refused with a sentence,
  the draft is kept and the newer text is shown beside it, and a save that
  changes nothing records nothing in the history (#89).
- Fixed: a large import no longer makes notifications, webhooks, rules and
  the audit log skip issues: the event stream keeps what a consumer has not
  read, and the relay waits for the slowest one; its lag and theirs are on the
  metrics (#95).
- Fixed: one organization's slow webhook endpoint no longer holds up everyone
  else's webhooks; deliveries go out side by side, at most two to any one
  endpoint, an endpoint that keeps timing out is paused for five minutes on its
  own, and `ARMATURE_WEBHOOK_CONCURRENCY` (8) sets how many go at once (#99).
- Fixed: a replica leaves the rotation on the median of its last
  `ARMATURE_DB_REPLICA_LAG_SAMPLES` lag readings instead of one slow check;
  staging and production refuse to start while `ARMATURE_APP_URL` or
  `ARMATURE_OIDC_REDIRECT_URL` points at localhost, and the README lists
  every variable the server reads (#102).
- Fixed: the audit log records who made, changed or removed a webhook or a
  rule, rotated a webhook's secret, sent or withdrew an invitation, defined,
  redefined or removed a role, or removed a group, and every sign-in with a
  password; the secrets these hand out are never in it, and the audit page
  filters by each (#103).
- Fixed: milestones, chart axes, sprints, the calendar's month, the
  Resources weeks, invitations, work logs and anything older than a month
  are written in the language set in the profile, not in British English or
  the browser's language (#117).
- Fixed: a session that ended in another tab or expired now sends the next
  action to sign in, which returns to the same page; the project switcher on
  a sprint board opens the other project's Sprints instead of this sprint; a
  comment posts once however often Ctrl+Enter is pressed; and sprint and issue
  dates save once a whole date is typed, or on leaving the input (#118).
- Fixed: an issue bulk edit lists as refused is left exactly as it was, with
  no new priority, label or history row from the steps before the refusal,
  and an applied issue announces its field changes once (#84).
- Fixed: a desk's queues reach every request, not only the 200 newest:
  Breached lists every overdue request, Open starts with the most overdue
  however old it is, and a queue longer than a page is paged and says how many
  requests it holds (#69).
- Fixed: the worker no longer loses an event it failed to handle, or one it
  held when it was stopped: audit entries, inbox items, mails, webhooks and
  rules are tried again, and an event that fails five times is set aside in
  the organization's audit log as `event.dead_lettered` (#93).
- Fixed: an absence recorded more than a year ahead stays in the list it was
  saved from, which now runs from a month back with no last day and says when
  it holds too many to show them all (#46).
- Fixed: changing an issue to a type whose workflow lacks its status records
  the move to that workflow's first status in the history, announces it to
  automation, mail and the SLA clocks, and clears or sets the resolution, so
  a reopened bug is no longer counted as resolved (#85).
- Fixed: Configure swimlanes edits the board on screen, not the project's
  first one, and its grouping changes only that board; the board picker can
  rename a board, change its type and delete it after asking by name. Lanes
  are written through `/boards/{boardID}/swimlanes` now, and the routes under
  `/projects/{projectKey}/board/swimlanes` and `PATCH
  /projects/{projectKey}/board` are gone (#52).
- Fixed: one issue dated far in the future no longer makes every plan read
  that far: the load is read over at most ten years, and issue dates
  outside 1900 to 2199 are refused with a sentence, also by the database (#40).
- Share of the week: a project administrator sets how much of each person's
  week the project has, and the Resources page counts their hours at it, per
  person and inside their team; a person given more than a whole week across
  projects is warned about, never refused (#43).
- Fixed: Themes has an icon of its own, a palette, instead of the sun the
  light mode wears, in the sidebar, the settings page and the command palette
  (#122).
- Fixed: somebody who has left the organization no longer shows on the
  Resources page with a full week free; work still assigned to them is counted
  as nobody's, and the project calendar no longer counts them among its people
  (#42).
- Fixed: importing an .ics file again no longer turns a calendar's half-day
  holidays into whole days off; a day the calendar has takes only the file's
  name (#45).
- Fixed: an .ics event that gives its length with DURATION (such as P3D or
  P1W) is imported for every day it covers, and one whose end is its start day
  is that one day instead of refusing the file (#44).
- Fixed: the Resources page counts work once per row, at the highest open level
  that carries hours, so a story and its subtasks are no longer booked twice
  (#41).
- Resource planning: a Resources page sets each week's scheduled hours against
  the hours left after holidays and absences, per team, or per person in a
  kanban project, hatches the days off, lists a week's issues on a click and the
  unscheduled and unestimated work below; projects gain a planning method and a
  grouping, and a scrum project plans by team (#31).
- Holidays and absences reach planning: the project calendar shades the
  default calendar's holidays, lists the other calendars' and who is away, and
  hides them all at a switch; the timeline shades the holidays; a team's load
  falls on its working days and its weekly capacity shrinks with its members'
  days off; sprint planning shows the team's available hours as a hint (#30).
- Absences: a person records the days they are away on their profile, and an
  administrator or the scrum master of their team may record them too; every
  colleague sees the days, never a reason, and customers see nothing (#29).
- Holiday calendars and working weeks: an organization keeps named calendars of
  days off, typed in or imported from an .ics file, and an administrator gives
  each person a calendar and their hours per weekday (#28).
- The application is versioned: `VERSION` names the release, `make release`
  cuts one, and a build past a tag says how far past it is (#27).
- Everything before this point: projects and issues, workflows, roles,
  planning, git, the service desk, NQL, dashboards, notifications, webhooks,
  automation, themes and the API, as the README describes them.
