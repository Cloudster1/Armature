# Changelog

What changed in each release, newest first. Versions follow
[semantic versioning](https://semver.org); `make release VERSION=x.y.z` moves
everything under Unreleased into a section of its own, so a change is written
here in the branch that makes it.

## Unreleased

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
