# Decisions

Short notes on choices that are not obvious from the code, and what would change
our minds. They are grouped by what they are about rather than by when they were
made. Not every entry ends with a **Reconsider if**: where the cost is already
named in the argument, that is the whole of it.

- [Data and tenancy](#data-and-tenancy)
- [Issues and workflows](#issues-and-workflows)
- [Planning, sprints and teams](#planning-sprints-and-teams)
- [Projects, git and the service desk](#projects-git-and-the-service-desk)
- [Reading the work](#reading-the-work)
- [The interface](#the-interface)
- [Identity, permission and the edges](#identity-permission-and-the-edges)
- [The API, the worker and the suite](#the-api-the-worker-and-the-suite)
- [The product's name](#the-products-name)

## Data and tenancy

### Read replicas with read-your-writes, rather than reading only the primary

Reading everything from the primary is simpler and correct. It also puts every
board load, search and dashboard on the one machine that must also accept every
write, which is the first thing to fall over. The write ahead log position is
what keeps replicas honest: a user is never shown a stale answer to a question
about their own change. The fallback direction matters. A read that cannot be
served freshly by a replica goes to the primary, and is never made to wait for
replication, so a lagging replica costs latency and never correctness.

**Reconsider if** replica lag is routinely under a millisecond and the pinning
machinery is buying nothing, or if the primary is so overloaded that fallback
reads become the problem they were meant to solve.

### Row level security in addition to tenant filters in queries

Application-level scoping alone means every query is one forgotten `WHERE`
clause away from a cross-tenant disclosure, forever, in code nobody is reviewing
that closely any more. RLS turns that class of bug into an empty result set. The
cost is real: policies per table, no superuser connection, and an explicitly
exempt role for the few genuinely cross-tenant operations.

**Reconsider if** we move to a database per tenant, which makes the policies
redundant.

### A transactional outbox rather than publishing events directly

Publishing to Redis inside a request means the event can be sent for a
transaction that then rolls back, or lost for one that committed. Writing the
event to a table in the same transaction removes the disagreement entirely, at
the cost of a relay and at-least-once delivery.

**Reconsider if** we adopt a broker with transactional guarantees against
Postgres itself.

### Hand written SQL and sqlc rather than an ORM

The expensive queries in a tracker are issue search, board load and permission
resolution, which are exactly the queries an ORM writes badly and hides. Types
come from `sqlc` generating against the real schema, so the compiler still
catches column drift.

### Go pinned to 1.26, and no S3 client library

See the build notes in the README. Both are workarounds for the endpoint
protection agent on the development host, not preferences, and both should be
retested periodically. The S3 client is a few hundred lines of signing over
`net/http`, which is also the smaller thing to read.

### The attachment row guards the object, and the bytes go through the API

One bucket serves every tenant, laid out by organization and issue, and nothing
about the bucket knows who may read what: the row in `attachment` does, under
the same row level security as everything else. A presigned URL would be faster
for large files and would put a second access control system beside the first.

An upload writes the row and then the object, in one transaction; a delete does
the reverse, row inside, object after the commit. Both orders leave at worst an
object nobody can reach, and neither ever leaves a broken link. Orphans are not
forgotten: the transaction that removes a row writes a tombstone per object key,
and the worker's reaper works from those. The bucket is never listed, because
the database is the one place that knows what should exist.

### A custom field is defined per project, and its value is JSON per issue

A column per field would make every new field a migration on the largest table
in the database; a table per kind would make the issue page six queries. JSON
with a kind beside it is one query and no migration, and a value that does not
fit is refused at the one door values come through. Values are never joined or
sorted on, so nothing is lost by not typing them in SQL. Deleting a select
option stops it being chosen again and does not rewrite the issues that chose
it: an answer is what somebody said.

### Labels are the organization's, coined where they are used

A label belongs to the organization, not to a project, so one word has one
colour wherever it appears and a list across projects can filter by it. It is
coined the first time somebody types it on an issue, so tagging never means
visiting a settings page first; that page is for renaming a word everywhere and
for retiring one. Names have no spaces, which keeps a label a word.

## Issues and workflows

### A parent is exactly one level above its child, not merely above it

If a task may hang straight off an initiative, a roll-up has to decide what to
do about the levels that were skipped, and two people reading the same tree
disagree about what belongs to what. The strict rule also makes a cycle
impossible to express, since every step towards a parent strictly increases a
bounded level. The cost is that reorganising a tree takes more steps.

The rule is a database trigger as well as a service check. The service produces
the refusal a person reads ("CP-3 is an epic, which cannot be the parent of an
epic"); the trigger is what makes the rule true for a migration, a script or a
future writer that reaches the table another way. `is_subtask` is a generated
column so it can never disagree with the level it comes from.

**Reconsider if** users are routinely creating one-issue epics purely to satisfy
the rule, which would mean the levels are wrong rather than the rule.

### Progress rolls up direct children only

An epic's bar counts its stories, not its stories' subtasks. Counting every
descendant means a story broken into eight subtasks outweighs a story broken
into one, so the bar tracks how finely the team writes subtasks rather than how
much of the epic is done.

**Reconsider if** teams start asking for estimate-weighted roll-ups, which is a
different feature rather than a different sum.

### A project's workflow scheme overrides the organization's per issue type

A project that names its own scheme is asked first, but only for what it
actually says; anything it does not map falls through to the organization.
Jira's schemes are all or nothing, so a team that wants bugs handled differently
ends up restating the other five types, and restating a default is how defaults
drift. Within one scheme the type it names beats the fallback; across scopes the
project beats the organization, because anything else makes "this project works
differently" quietly untrue.

The cost is that reading a project's configuration resolves two levels rather
than one, so the answer says where it came from and the settings page is a table
with a "decided by" column. The organization always has an answer, and that is
the database's business too: at most one default scheme is a partial unique
index, at least one with a fallback mapping is a deferred constraint trigger.
Without that invariant every caller resolving a workflow needs a story for an
issue type with no workflow at all.

### Saving a workflow reconciles rather than rewrites

An edit keeps the rows for statuses and transitions that are still there and
deletes only what the editor removed. Deleting the graph and re-inserting it
would be far less code, and would silently throw away every condition, validator
and post-function on a transition nobody touched. Rules are the expensive part
of a workflow to configure and the easiest to lose without noticing.

A transition's rules are replaced when sent and kept when not, so absence means
"no opinion": templates and the seed send rules only on transitions they create,
and the designer always sends the whole set.

### Changing a scheme leaves open issues where they are

An issue whose status is not in its project's new workflow keeps that status and
is offered the transitions available from anywhere. Migrating issues into the
nearest equivalent would need a mapping nobody supplied; refusing the change
while any issue is in a status the new workflow lacks would make configuration
impossible in exactly the projects that most want it.

### A status is coined where it is needed, and a board follows its workflow

Statuses are the organization's and shared by every workflow, which is what lets
two projects agree on what In Progress means. The designer's panel coins one
with a name and a category, and the category is for keeps: everything that
counts what is done reads it, so changing it later would silently reclassify
every issue standing in it. Saving a workflow tells the board package which
statuses are new, in the same transaction, so every board that now reaches a
status grows a lane rather than losing cards to a lane that does not exist.

### Where a status is drawn belongs to the workflow, not the browser

The layout could have lived in local storage. It lives on `workflow_step`
because a workflow is something several administrators look at, and the point of
arranging it is that the next person sees the arrangement. Null means never
placed; a check constraint keeps a lone coordinate out, since half a point is
nothing anybody can draw.

### A project administrator maps a type, and the scheme is the bookkeeping

To say "bugs go through triage here" a project used to need an organization
administrator to name and build a scheme. Now each row of the table has a select
offering the organization's workflows and "Follow the organization". The scheme
is still underneath, because the resolver and the guards speak scheme, but
nobody names one to change one row: the first row a project decides makes a
scheme named after the project, a scheme shared with another project is copied
before it is touched, and a project that hands its last row back loses the
scheme with it. Drawing a workflow stays with the organization.

### An arrangement is the project's, per issue type, in one flat table

Which fields an issue shows, where, and in what order is a row in
`issue_arrangement` keyed by project and issue type, either of which may be
null: null project is the organization's, null type is every type. Resolution
is one statement ordering by how specific the row is, the same shape the
workflow resolver already uses, and no row at all is the built-in arrangement,
which lives in Go beside the templates.

It is deliberately not the workflow scheme's pair of tables. A scheme exists
because it is a named object several projects point at, and nothing here asks
to name an arrangement or share one; copying the pair would have brought
`EnsureOwnScheme`, `freeSchemeName` and `SchemeIsEmpty` along for nothing. What
that gives up is exactly sharing: two projects that want the same unusual
arrangement set it twice. Making them share it later is additive, a scheme
table and a `project.issue_arrangement_id`, and nothing already written would
have to move.

Three things fall out of the shape. `otherFields` is a slot rather than a list,
so every custom field the arrangement does not name appears in the project's
own order and a field defined next month needs no migration. The slot
vocabulary is a Go type published as an OpenAPI enum, so a slot the server
knows and the browser does not fails `npm run typecheck` rather than drawing
nothing. And the create form reads the same arrangement, flattening the areas
in order and skipping what is hidden, because a field nobody shows is not a
field anybody should be asked for. That form cannot share the page's adapters,
though: the page edits an issue that exists, the form has no issue yet, so
there are two registries over one vocabulary and a slot may answer null in the
form while drawing a row on the page.

**Reconsider if** somebody asks to name an arrangement or reuse one across
projects, or if required fields arrive, which is a column on the slot and a
check at create time rather than a change to any of this.

### Time is minutes, logged against an estimate

Every stretch of work is a worklog row, and what an issue has had spent on it is
the sum of those rows, computed when the issue is read. A stored total would
need every entry, edit and deletion to keep it right, and a total that disagrees
with its entries is the kind of error nobody notices until the invoice. Minutes,
not fractional hours, because a quarter of an hour is the smallest thing anybody
honestly logs and 0.1666 is not a number people type.

Logging work takes time off what remains: an explicit remaining figure comes
down by the minutes logged, an issue with only an estimate gets one from what is
left of it, and neither goes below zero. Deleting an entry leaves the remaining
figure alone, because it was a judgement and not arithmetic.

### The document is the record, markdown is a way of typing it

Descriptions and comments have been ProseMirror documents since the first
migration, so that mentions and embeds could be nodes rather than parsed out of
prose. The editor keeps the document canonical and gives it two faces: rich is
TipTap, because a contenteditable surface is the one thing nobody should
hand-roll, and markdown is our own pair of converters over exactly the node set
the schema names, because a parser that accepts more than the renderer shows
would let a person type what they cannot then see. The server refuses any node
the renderer would drop, and rendering goes to elements, never to HTML, so
nothing typed is ever parsed.

**Reconsider if** customers ask for formatting, at which point the portal sends
a document as comments do, or if a node markdown has no spelling for is wanted.

## Planning, sprints and teams

### The plan warns and never refuses

Every other write in the product is validated. Scheduling is different: a plan
is drafted by moving things around and looking at what breaks, and a timeline
that refuses to let a bar overlap its blocker forces the person to resolve the
conflict before they can see it. So conflicts are computed and listed, and the
dates are always saved. The exception is a range that ends before it starts,
which is not a conflict but a nonsense, and is refused twice.

**Reconsider if** teams start using the warning list as a backlog they never
clear, which would mean the warnings are noise rather than information.

### Roll-up stops at a value somebody typed

An epic with no dates spans its children; an epic with dates keeps them, even
when a child runs past the end. An estimate follows the same rule. Stretching
the parent to fit means a date somebody deliberately chose can be changed by
somebody else editing a different issue, and the disagreement that mattered
disappears silently.

**Reconsider if** people routinely set epic dates and then expect them to track
the work, which would suggest the field means "target" and wants a name saying so.

### Work in a sprint is counted once, at whichever level carries it

An issue whose ancestor is in the same sprint contributes through that ancestor
rather than again on its own. Jira sidesteps this by estimating at one level and
hiding subtask points, which works until a team estimates an epic and its
stories both, as every team eventually does. The estimate on a parent is a
statement about all the work beneath it, so adding the children counts it twice.
Moving one child into a different sprint therefore changes what two sprints
hold, which is surprising until you see that it is exactly right.

### Capacity warns and never refuses

Committing twenty-one points against a capacity of sixteen is allowed and
produces a warning naming both numbers. A tool that refuses the twenty-first
point makes the over-commitment invisible rather than preventing it: the work
still exists, it just sits somewhere the number does not reach. The unestimated
count is shown beside every total for the same reason, since eight points with
five unsized issues is not eight points.

### A completed sprint's report is stored, not recomputed

Committed and completed totals are written to the sprint row when it closes.
Recomputing them on read is less code and always consistent with the current
issues, which is exactly the problem: re-estimating an issue in January would
change what last October's sprint is reported to have delivered.

### One running sprint per stream, and planning is the team's work

A partial unique index on `(project_id, team_id)` with `NULLS NOT DISTINCT`
keeps one sprint running per team, and one for the project's own unassigned
work. Parallel sprints on one board make "the sprint" ambiguous in every
sentence a team says and in every piece of code that has to decide which sprint
an issue belongs to. Creating, starting and completing sprints is open to any
member: the dividing line the rest of the codebase uses is whether an action
changes how the tool behaves for everyone or moves the work along, and a sprint
is work moving.

### A team narrows access and never widens it

Only somebody already in the organization can be put on a team, enforced by the
service and by a trigger. The alternative is a team that can pull people in,
which turns every team into a way around the organization's own membership.
Deleting a team with work in it is refused rather than quietly returning the
work to the project, because work that was somebody's and is now nobody's is
discovered weeks later and the fix by then is archaeology.

### A board is scoped by team, and its type is behaviour

`board.team_id` decides what a board draws, and a null means the whole project.
Jira boards are backed by a saved filter, which is more general and is the right
answer eventually; it is also a second query language to validate and explain,
where the overwhelmingly common case is one board per team. The cost is that a
board cannot yet show, say, only bugs.

The type is not a badge. A scrum board draws the running sprint in its stream, a
kanban board everything in its scope, so choosing it is a real decision and a
scrum board between sprints is deliberately empty. A board with no type takes
after the project's first board, which is the one the template chose; a project
with no board at all falls back to kanban, the type that shows work without a
sprint. A backlog is derived, not stored: storing it would allow an issue to be
in a backlog and a sprint, or in two backlogs, or in neither.

### Team capacity is points per week, spread over the days a ticket is scheduled

Capacity lived only on the sprint, which says nothing about a project that plans
in weeks and months. A team says how many points it can take in a week, in the
unit its issues are already estimated in; hours would have put a second unit
beside the first. Load is read off the schedule, each estimate spread evenly
between start and due date and summed into Monday weeks, with the same
count-once rule the sprints use. A week over its capacity is a warning with the
team as its subject, never a refusal.

The load is rows under the plan rather than a band over it: a band per team
would swallow the calendar in a project with several, and rows read where a
manager reads them, under the work they summarise.

### Tickets are made on the plan where they will sit

Filing a ticket used to mean leaving for the project page and coming back to
schedule it. Each open sprint group and the backlog now end with a line to add
to. Filing into a sprint is one request rather than a create and a move, so a
sprint that refuses leaves no half-committed issue; because committing is what
it is, it takes the sprint permission.

A drag across the calendar makes a ticket only where nothing else could be
meant: on an add row, or on an unscheduled issue's own row. A scheduled row's
empty space is where bar drags begin and end, so a drag there means nothing.

### Closed work leaves the plan after a number of days the reader chooses

A project's plan used to carry every ticket it ever had, and a year in, most
rows were finished. The cut is a number of days read against the resolved time
each item already carries, so it is a reading of the same response and the
totals still count the whole plan. It is kept in the browser like the theme,
because it is how one person likes to look and not something the team agrees on.

**Reconsider if** people want the cut to follow them across browsers, or a
second per-person setting appears: two is when a preferences table pays for
itself.

### The plan moves a ticket by dropping it, and the server still decides

On the plan the hierarchy is already drawn, so a row is lifted and dropped: one
level up is "into", the same level is "beside", above the rows is the top level.
The client refuses what the one-level rule and the cycle check would refuse, in
the server's own sentences, so a bad drop is answered where the pointer is; but
it is the server, and under it the trigger, that decides. A drag begins only
after the pointer has travelled, and never from the key or the chevron.

### A blocks link cannot come back around, and the database says so too

Nothing stopped "A blocks B, B blocks C, C blocks A" while links were made one
at a time, because nobody did it. Dragging both ends on the plan makes it a slip
of the hand, and a plan with a circle in it has a warning for every issue in the
circle and no order to draw them in. The service refuses it and a trigger
refuses the same row, walking the same links, bounded at thirty-two. Relates and
duplicates are symmetric, so a triangle of those means nothing and is allowed.

### Dependencies are drawn where they are seen, and the graph is a third reading

A dependency is made by dragging from the dot at the end of a bar, because the
plan is where the two bars are seen side by side. Undoing follows the same rule:
a cross on the arrow itself, where the hand already is, and the issue page still
lists its links.

The dependencies view is the same plan response read a third way, through the
same filters and cut, because a second endpoint for the same links would be a
second thing to keep true. Columns are longest path from the tickets nothing
waits on, which is what a person means by "how far down the line is this"; rows
are pulled towards their predecessors in two passes, since a full crossing
minimisation would draw a slightly tidier picture at a cost in code nobody would
read. Tickets with no dependencies sit in a grid underneath, because a graph
that only shows what is already linked cannot be where the first link is made.

### Plan views are readings of one response, not endpoints

The management and sprint views could each have asked the server for rows shaped
the way they draw them. The plan endpoint already carries everything either
needs, and a second shape of the same data is a second thing to keep true.
Grouping, filtering and the numbers are pure functions over the plan the client
already has, which is also what makes them testable without a browser. A sprint's
board is the same idea: the stream's board with the card scope pinned to that
sprint, rather than a board per sprint.

The plan opens on a Fit zoom that measures the pane and spreads the whole plan
across it, down to a floor below which it scrolls. The fixed zooms open on today
rather than on the first day of the plan, which is usually the past, and the
chart publishes its density in the DOM so the browser tests drag by days.

### Milestone progress is counted, never stored

A milestone could carry a percentage the issue service updates on every
transition. It does not, because that number has two sources of truth the moment
an issue is reassigned, deleted or moved, and every one of those paths would
have to remember the milestone. Three correlated counts cost nothing at the
sizes a milestone has and cannot be wrong. Sprints take the other route only
because a completed sprint's totals are meant to stop moving.

Setting and running milestones takes the sprint permission; assigning an issue
to one takes the issue permission, because saying "this piece of work is part of
the release" is the kind of thing anybody who edits issues does.

### A day is written down, not replayed from the changelog

Burndown and cumulative flow both want to know what was true at the end of a
day. The changelog could say it, and does not, because it stores names rather
than ids: a renamed status or sprint would quietly bend a chart drawn months
later, and walking a project's whole history on every read is the wrong price
for a widget. The worker writes the day's counts, the day's last write stands,
and a day with no row is rebuilt from the changelog once and marked as rebuilt.

Today is read live and every earlier day from its snapshot, so the chart agrees
with the widget to the second and the browser suite is deterministic without a
scheduler. Created and resolved times are columns and never renamed, so the
reports over them need no snapshot; the control chart measures from the first
step into an in-progress status, because the question is how long work takes
once somebody is on it, and the resolution histogram from creation, because the
question is how long the asker waited.

## Projects, git and the service desk

### Templates are code, not rows

A table of templates invites editing them, and an edited template raises the
question of what happens to projects made from the old version: nothing, in
which case the edit is misleading, or something, in which case a template is a
live dependency of every project. In code, what there is to choose between
changes when the product does, in a commit, and a project only records which one
it began from.

A template with a workflow of its own finds it by name or creates it, in the
same transaction as the project, so the second project shares the first one's
rather than the organization collecting identical copies. Kanban is the default
because a scrum board is empty until a sprint has been planned and started,
which is three pages to visit before a new project shows anything.

### A template decides what a project is for, and the project may change its mind

A service desk carried Sprints, Plan, Milestones, Releases, Hierarchy and
Repositories because nothing had ever said it should not. The template already
decides the kind, the board and the workflow, so it decides the pages too. The
list lives on the project rather than on the kind, because Scrum and Kanban
share a kind and a kanban team may still run a sprint. Off means hidden and
refused, never deleted: making more is refused by the service and by a trigger,
what exists stays, and reads stay open because a link is a promise.

**Reconsider if** a feature starts needing configuration of its own, at which
point it is a module and a list of names is not enough.

### A webhook proves itself; a session is not involved

The webhook endpoint is public. A delivery is authenticated against the
repository's own secret, and only then does the work run inside its
organization. An API token per repository would make the host a user with a
session, which it is not, and would put a long-lived tracker credential into the
host's configuration. Using the host's own mechanism means the secret the host
holds is one it already knows how to keep.

### Smart commits act once, and the pull request is told by the worker

A commit's commands are carried out on its first arrival, so merging a branch is
not a second "#close". A transition the workflow refuses goes into the receipt
with its reason and the commit is recorded regardless, because the recording is
what the history needs.

Comments back to a pull request are posted by the worker reading the event
stream. Posting from inside the transition would make every transition wait on a
host, and fail when the host does. A host that refuses is logged and
acknowledged, not retried forever.

### A branch made from a ticket is the ticket's, whatever it is called

Commits and pull requests used to reach an issue only by naming its key, which
works as long as everybody types the key into every message. A branch made from
the ticket already knows which issue it is for, so the branch row carries it and
a push links its commits through the row. The key in a message still works, and
is still how a branch made by hand is picked up. A branch nobody made for an
issue leaves no trace beyond the keys its commits carry.

The stack runs a Gitea of its own, so the browser suite makes a real branch on a
real host and the delivery that brings it back is the host's own. GitHub and
GitLab could only ever be exercised against a stub.

### A request is an issue, and a note is a flag

The service desk adds no second table for requests or for the conversation. A
request table would need everything an issue has, and then the board, the plan,
the workflow and the history would need to learn it. A request is an issue with
a request type on it, in a project whose kind is service; an internal note is a
comment with a flag. The cost is that the flag has to be respected everywhere a
comment is read, which is one method with one parameter.

The request type is also the template: a name in the customer's words that
becomes an issue of a type at a priority, with the category, the details
skeleton and the team as columns on it. Its team is checked twice, because a
misrouted request type would quietly hand one project's requests to another
project's team.

### The clocks move inside the issue's transaction

The desk observes the issue service, and its timers change in the same
transaction as the status or the comment that changes them. Moving the clocks to
the event stream would make "resolved" and "resolution clock stopped" two facts
that can disagree for a while, or forever if the worker is down. The one thing
that cannot happen inside a transaction is noticing that time has passed, so
breaches are the worker's, on a thirty second look, recorded once each.

A goal measured in business hours is arithmetic over a zone, a set of daily
spans and a list of holidays, which in SQL means either a table of every working
minute or a function taught about daylight saving. So the database finds the
suspects with a wall-clock comparison, which contains every calendar breach and
some false ones, and the Go clock decides.

### Customers are refused uniformly, not route by route

A customer's role holds no permission, so most of the agents' API refuses them
already. The remainder is refused by one check that lets only the portal
through, because relying on the permission table alone would leave the reads
that ask for no permission open, each one found later being a hole fixed by
hand.

### A portal code is a sign-in, not an account

A person who proves a mail address by typing the code mailed to it becomes a
customer member with an ordinary session, rather than a second kind of principal
with its own scope and its own gaps. "No account" is a row without a password,
which is already what a single sign-on user is. The code is the one mail the API
sends itself, because the person is waiting for it and a one-time code has no
business in the event stream. Asking for a code answers the same way for every
well-formed address, so the door reveals nothing about who works at the desk.

### An unproven address is a customer of one desk

A desk may let people in without the code. The code is what makes a portal
session safe to be organization-wide, so without it the address is a claim, and
a claim must not open what proof opened. A session that came through an open
door carries the desk it came through, and every portal read asks: that desk's
request types and its own requests there, and nothing of the organization's
other desks. Turning the code back on ends every session that skipped it. The
database holds the same line through the trigger that guards the door.

### A desk trusts domains, not addresses

The list of domains a desk takes requests from sits on the desk, beside its code
switch, and serves the door, the portal and the mailbox alike, because a claim
about an address is the same claim however it arrives. An empty list means
everyone. The refusal names the domains, which is the desk's published policy
and reveals nothing about whether a person exists. Whoever is already on a
request stays on it: the list decides who may come in, not who may stay.

**Reconsider if** a desk asks for per-address lists or a deny list, at which
point the column becomes a table with a verdict per row.

### A follower is a user, and a mail address is enough to be one

An address becomes a user with no password, the same row a portal code makes, so
"mail or account" is one column and the person named can later enter the portal
with a code and find what they were added to. Who may add is who owns the
conversation; who is told is the reporter and the followers, never the person
whose act it was.

Because a person can be made a follower without being asked, the first mail
carries a link that ends the following with one press and no sign-in, keyed on a
token the row keeps only as a digest. It is a button on the page, not an action
on arrival: a link must not change anything just by being opened.

### Replies come in by POP3, and are accepted from whoever could have typed them

An IMAP library is a large tree of code for six commands, on a host whose
endpoint security kills binaries it finds suspicious. POP3 is served by every
provider and by Mailpit, and a desk's mailbox is a queue rather than an archive,
so fetch and delete is the right shape. The reader consumes only what is
addressed to the inbox, remembers the rest by uid, and deletes on QUIT, so a
connection that drops leaves the box as it was.

A mail names a request by the key in its subject or the id of the mail it
answers. A key alone is ambiguous across tenants, so the sender is part of the
question: the reply is accepted only where the address belongs to the reporter,
a follower or an agent, and in exactly one organization. Every consumed mail is
recorded by message id, so a redelivery is refused by the database however many
workers are reading, and machines are never answered, so no loop can form.

### A customer's files are the request's, and only their own come off

A file on a request is part of what was said, and the conversation is one thing
read by two audiences, so the customer sees every file on their request, the
desk's included. There is no internal file the way there is an internal note: a
file the desk must keep to itself does not belong on a request. Removal follows
authorship rather than role.

**Reconsider if** a desk asks for files it can attach without showing, at which
point attachments grow the flag comments have rather than a second list.

## Reading the work

### NQL compiles to SQL from a catalog, and never prunes the plan on the server

Every field, operator, function and ORDER BY expression is a row in code, every
literal becomes a bind parameter, and the compiler checks the query against the
catalog before any SQL exists, so the database only ever sees clauses the code
wrote. The clause names only the four tables both issue queries join, reaching
everything else through subselects, which is what lets one compiled query serve
the count, the page and the plan.

The plan is returned whole, with the keys the query matched beside it, and the
client cuts the rows. Pruning on the server would change the totals, the sprint
numbers and the warnings with the query.

**Reconsider if** a project outgrows the two thousand rows the plan reads at
once, at which point the server has to select rows before it rolls them up.

### A query error points at a character

A query that fails is answered with a sentence saying what to do and the 1-based
character the trouble starts at, and the client draws a caret under the query as
it was typed. A message alone leaves the reader scanning a line of operators for
the one that is wrong. Positions are counted in characters rather than bytes,
because they are for a person looking at text.

### The search bar finishes the sentence

The bar offers, as it is typed, the words the parser would accept at the caret,
from the same catalog the compiler reads, so a suggestion is never something the
query cannot say. Names that live in tables are looked up live, scoped to the
project when there is one. Issues are offered beside the words, since the person
typing into a search bar is as likely to want the ticket as the query. Enter
with nothing highlighted runs the query as it always has.

**Reconsider if** typo tolerance is asked for, which is the trigram extension
and an index.

### A saved filter is a query with a name, and everything else reads it live

The recurring complaint about Jira's filters is copies: a board made from a
filter that was later edited, a gadget stuck on last month's JQL. A saved filter
here stores nothing but the text and the name, and every use of it compiles that
text when asked, as the person asking. A subscription mails what the subscriber
would see, not what the owner would.

Bulk edit is one transaction per issue, so a workflow that does not offer a
transition refuses its own issue and names itself while the others are already
done. A moved issue keeps its row and gets a new key, because history, comments
and files hang off the row; the old key stays on the row so the address that was
mailed around still finds it.

### A dashboard is an arrangement, and every number is computed on read

A widget is a kind, a title, a width, a position and its parameters; its report
is one query when it is drawn. Storing computed numbers would need a job to keep
them right and a story for when it falls behind, and they would still be wrong
between runs. The kind is text rather than an enum, because an enum would make
every new report a migration for a constraint the service already enforces.

A dashboard's filter is a query the tiles agree on: the controls write the same
language the search page and the plan speak, and the composed clause rides along
with every report as the same `q` a search carries. On the server it runs in a
subquery that owns the aliases the catalog binds to, which is what lets a dozen
hand-written reports take it without being rewritten. The live filter lives in
the address, so a dashboard somebody sends is the one they saw.

### Charts are six rectangles and an arc, drawn here

A stack is rectangles with a gap of surface between them, a donut is one arc
path a dozen lines of trigonometry produce, and the line already existed for the
burndown. One grouped query serves every shape, with short whitelists for what
to group by, split by and measure, so the page never sends SQL. Colour follows
the label rather than its rank, so a filter that shrinks a group does not
repaint the rest; a seventh group folds into Other; and colour never carries the
identity alone, because a validator rather than an eye says which neighbours a
colour-blind reader can tell apart.

### A dashboard template is a copy, in code or in rows

A dashboard made from a template is a copy, so nothing happens to it when the
template changes. The built-in templates are code, keyed by a word; an
organization's own are rows, because an arrangement somebody built on a Monday
is theirs to keep and the product cannot know it in advance. They copy widgets
and drop what belongs to one project, team, sprint or milestone by id, while the
filter's names travel. A template is not edited: it is removed and saved again.

A milestone's dashboard is the same copy pinned to a name and an id, set in one
transaction so they cannot disagree; closing or deleting the milestone leaves
the dashboard where it is.

### A share link is a read that names its tenant

A dashboard on a wall in the lobby has nobody signed in to it. A share link is
the first anonymous read: a random token whose digest finds the organization and
the dashboard, after which the ordinary report code runs inside that
organization. What a visitor can ask is each widget of that one dashboard, by
id, with the widget's own stored settings; there is no route that takes a kind
and parameters. The filter is frozen when the link is made and compiled as the
sharer, so a visitor cannot widen the view. The address is shown once, like an
API token, and the access log writes the path with the token blanked.

A PDF is that shared page printed by the browser the product already ships for
its own tests, told a path and nothing more, behind a link good for two minutes.
A deployment without the service answers that export is not set up, which is a
sentence and not a broken button.

### A version is a milestone that ships, and an issue can name two

A milestone is a target and a version is a thing, so a version is its own table
with a lifecycle: planned, released, archived. An issue counts towards one
milestone but can fix one version while affecting three, and the two roles are
one relation with a role column, so the guard that keeps a version to its own
project exists once. Progress is read off the issues, never stored, and release
notes are a reading of the same rows. Components follow the same shape with one
twist: an issue filed into a component with nobody named takes its default
assignee, decided at creation and written into the history.

### An import is the only writer that may date its own rows

Everything that writes an issue stamps the time and the person for itself, which
is right for work being done here and wrong for work being copied from somewhere
else. An import that cannot say when something happened turns four years of a
tracker into one afternoon: every issue filed today, by whoever ran the import,
in the status new issues start in, under a key nothing else in the file refers
to. The history is most of what a tracker is worth.

So an import is given what a create stamps: the key it had, the days it was
filed, last touched and resolved, the person who filed it and the status it
stands in. The permission is a field on the actor that only the importer sets,
the routes ask for project administration, and the status still has to be a step
of the issue's own workflow. Nothing is told: no event, no observer, no
notification. The source's own name for a row is kept beside it, which is what
makes a second run a correction rather than a copy. That name identifies the row
inside the project it was imported into, not inside the organization: two
projects may both be told the same file, and the second must make its own issues
rather than correct the first one's.

**Reconsider if** anything but an import needs dated writes, for instance a
migration tool driving the API from outside. The flag would then have to become
a permission an administrator grants, and the audit log would have to show that
a row was written as somebody else.

### A file is mapped by position, and its words are matched once

A mapping used to say which column name filled which field, which works until a
file names five columns Labels, nine Sprint and eleven Comment, as every Jira
export does. A mapping now says which column positions fill a target, several
where the target holds several, and the page shows a column's first values
beside its name.

The file's words are not ours either: its types, statuses and priorities are its
own vocabulary, and its people are usernames, which cannot be accounts here
because an account is an address. So the file is read once before anything is
written, the person importing confirms what each word and each name means, and
the run applies it. A name nobody answers to costs the issue its assignee and is
named in the report rather than refusing the row. Where attribution matters
more, that name can be given an account at a domain the operator names, with no
password and inactive, and only ever at an address that is free.

**Reconsider if** files start arriving on a schedule rather than from a person
at a page, at which point a mapping wants to be saved with the project.

### An import brings the whole issue, in the order the file allows

The file says which epic an issue is under, which sprint and version and
component it is in, what it blocks, and what was said and spent on it. Read row
by row in the file's own order none of that can be written, because a parent may
not exist yet and neither may the issue at the other end of a link. So rows are
sorted by the level of the type they name, each issue is written with a running
note of what the file's key became here, and links, comments and worklogs wait
for a second pass. Sprints, versions, components and teams are made by name
where the project has none, and the report says what was made.

An export is usually a filtered view, so it names epics it does not carry: the
file this was written against referenced thirteen and contained five. A parent
that is neither in the file nor already in the project therefore costs the issue
its parent and is named in the report, rather than costing the issue its place,
which is the rule the unmatched people already follow. The dry run answers that
same question from the keys the file itself carries, so what it promises is
still what the run does.

An issue belongs to one sprint here and to several in Jira, so it keeps the
first its row names and a sprint that is never first on any row is not made.
Two entries written in one minute are two entries, so where a cell sits in its
row is part of the name that makes a second run a correction: without that, the
second of two worklogs at one timestamp was quietly dropped.

Markup is read as far as the document allows: headings flattened to three
levels, lists, quotes, code blocks, bold, italic, code and links, a table kept
as its cells, a mention kept as the name it was. Anything unrecognised stays as
text, so no description is emptied by markup nobody taught this to read. A
struck-through word is the one piece deliberately not read: Jira writes it with
a hyphen, and a hyphen in prose is far more common than a strike.

**Reconsider if** attachments become part of an import, which needs the old
instance's bytes, or if a changelog can be imported, which would mean accepting
dates from outside for more than the one row that says the issue arrived.

## The interface

### The interface is a tool, not a landing page

Warm neutrals, hairline borders, one accent, and tables where there are columns.
Primary actions are ink; the accent marks what is current. The first version of
every screen had the same shape: a title, a sentence explaining the screen, and
a stack of rounded cards on white with a blue button, which is what a generator
produces when nobody has decided anything. Surfaces barely differ from the
canvas, so structure comes from spacing and type rather than boxes; lists with
more than one fact per row are tables; and a page header has no room for a
sentence about the page. If a screen needs explaining, the empty state is where
a first-time visitor reads it.

### The palette is stone and cobalt, and the kit is one file per component

The interface grew a feature at a time, and its kit stayed at fourteen pieces
while the product reached fifty pages: no dialog, no menu, no toast, no icon, so
every screen drew its own. The typeface is Inter with JetBrains Mono, bundled:
the previous face was wide at 13px and cost a column in every table, and a
tracker is mostly tables. The accent is cobalt because the previous teal sat
between the amber of "in progress" and the green of "done", so the one colour
that meant "current" looked like a status.

One file per component with a barrel that keeps every old name, so the fourteen
pieces became thirty without a single import changing. The type scale is seven
tokens with their line heights. The icons are drawn here on a 16px grid rather
than taken from a library, for the reason the workflow canvas and the charts
were: one weight, one corner radius, and nothing to update.

### One kit, no exceptions

Eight branches in, the kit had every piece a page needs and the pages still
typed buttons, selects and sizes by hand. The sweep replaced them, and a test
keeps it so: it reads the source and fails on a raw control, a hand-typed size
or the old accent name anywhere but in the kit, which is the only place a change
to how a control looks is now made. The exception is the hidden file input
behind an upload button, which nobody sees.

### The shell is a rail beside a map

One column of chrome had to be both the places every page reaches from and the
map of where you are, and folding it to a rail lost the map without gaining
anything. Two columns say two things: the rail is what is always there, the
sidebar is the map, and folding the map leaves the rail whole. The groups fold
and remember it; Project Admin folds by default since setup is visited less than
work. A project's pages are in the sidebar with a switcher that keeps the page
kind, so Board stays Board in the other project, and the command palette is the
keyboard's sidebar. Below 1024px the sidebar becomes a rail of icons.

The page's head draws into a strip over the content rather than moving out of
the thirty-five pages that call it, and floats on glass so the title and the
tabs stay while the list scrolls under them. The dark theme's neutrals are slate
under the same cobalt, because a dark warm brown reads as a stain where a dark
slate reads as a night.

**Reconsider if** a third column is wanted, at which point the rail is the wrong
first column.

### Destructive actions confirm with the noun; undoable ones toast with Undo

Nothing in the first interface asked before it deleted. Anything that cannot be
undone in one click now goes through one dialog: the title is the question, the
body names what follows, and the button says the verb and the noun, so the
reader knows what goes before it goes. What can be undone is not asked about: a
question before a reversible act is friction, and a question before an
irreversible one is care.

### Creating something takes you to it

A project made from the projects page appeared as a new row the maker had to
find; an issue made from the project page blanked the form and said nothing.
Both now end where the next thing happens. The projects page became a table with
the facts a person scans for, because a page of projects is a page of projects
and not a form.

### An issue is one page in two columns, and the same page opens beside the list

The issue page had grown to ten panels down the left and seventeen facts down
the right, because every feature had added its panel where the last one ended.
The two columns stay, because the facts on the right are what a reader glances
at while reading the left, but the header sticks, the summary edits in place, a
rail jumps between sections, and the facts fold into four groups. The
conversation is read and added to near the top; the history is its own last
section, because how the issue got here is what a reader scrolls down for.

The same component opens beside a list or a board rather than over it: a click
on another row swaps the issue, Up and Down walk the list in the order the page
shows it, and the filters and the drag go on working. The list tells the panel
its order rather than the panel guessing. The drawer stays for narrow screens,
because a column needs room to be one.

### The plan has one toolbar; the cut and the filters are one popover

The plan had grown four bands of controls and two ideas of narrowing that a
reader could not tell apart: the cut, which every view respected, and the
filters, which only one did. Now one toolbar holds the view, the query, one
Filters button and the zoom; the filters and the days of done work live in the
same popover, because to a reader they are one question, what is drawn. The
filters live above the views, so switching views keeps them.

### A warning is worn by the row it is about

The plan listed its warnings under the card, each naming a key, which is right
and is not where the reader is looking. The row wears its warning, and so does
its bar, in two colours: a contradiction is red, an omission is yellow, because
they ask different things of the reader. The list stays, because a sprint
committed past its capacity belongs to no row.

### The project page shows the drawing, and only the designer changes it

"Bug triage" is a name; whether it has a review step is the drawing, and an
administrator weighing one workflow against another was sent to the
organization's designer to find out. The page now draws the workflow with the
boxes where the designer left them. It is the designer's canvas with a read-only
switch, not a second renderer, so the two can never disagree about what a
workflow looks like. A status card says its category three ways, in the tint, in
the badge and in the subtitle, because one cue is easy to miss.

**Reconsider if** statuses gain an icon or a colour of their own, at which point
the card wears those.

### The workflow canvas is React Flow, and the geometry is still ours

The canvas was hand-drawn SVG first, and that entry argued for it: a workflow is
a handful of boxes and arrows, and a graph library brings its own state model
and bundle. What the hand-drawn version did not have was pointer capture, the
keyboard and touch, each a fresh hole to fill. React Flow fills them and does
not get the model: `designer/graph.ts` still decides where a status may land and
what is saved, `lib/graph.ts` still bends two arrows apart and clips them to the
borders, and `flow.ts` only translates. Zoom is locked at one and the canvas
scrolls, because the drawing is a page to read, not a map to explore.

**Reconsider if** the library's bundle or its own state model start dictating
how the designer works, at which point the SVG is a few hundred lines away in
this repository's history.

### Dates are the reader's, not the browser's

Every date was written the way the browser felt like writing it, while the
account had carried a time zone and a language since the first migration and
nothing read them. One module now makes the formatters from the profile. The
short relative forms stay, because a list column reads "2h ago" in any language.
The language changes formatting only, and the page says so: translating the
interface is a different undertaking, and a setting that promises it and does
not would be worse than none.

### A person can see and change what is theirs

The profile page shows the name, the address that signs them in, the zone, the
language, the picture and the organizations they belong to. The picture goes
into the same bucket as attachments, under a key that is the user's and not any
organization's, and comes back through the API, where the session decides who
sees it: a bucket policy would have made the faces of a private tracker public.
The bytes are read to two megabytes, the format is what the bytes say rather
than what the upload claimed, and the sides are checked from the header before a
pixel is decoded. The stored URL carries a version, so a browser may keep a face
for a year and still shows the new one the moment it changes.

## Identity, permission and the edges

### Roles carry no inheritance

Each of the five roles lists its permissions outright, rather than a scrum
master being "a user plus sprints". Inheritance reads well until the first
exception, and a chain broken in one place stops being a chain. Listing each
role costs a few lines and means every question about a role is answered by
looking at that role. Permissions are resolved once per request rather than per
guard, because the answer cannot change mid-request, and the scope is a project
key because that is what every question arrives as.

### The owner always administers their own organization

A standing grant, outside the assignment table. Roles are data, and data can be
edited: an owner who removes their own last administrator role would otherwise
have locked themselves out of a tenant they own. The last-administrator check
catches the same mistake from the other side, and neither is enough alone.

### Authenticating is not being let in

Somebody the identity provider vouches for who has not been invited is refused.
Letting anybody with an account at the configured provider in is how single
sign-on is usually demonstrated, and it means the organization has no
membership. The provider answers "who is this" and the product still answers
"should they be here". Group membership from the provider is replaced at every
sign-in rather than merged, because access granted through a group must not
survive being removed from it; groups maintained by hand are untouched.

Verification uses a reviewed library rather than hand-written JWT parsing. The
failure modes of the hand-rolled version are well known and quiet, and the
integration suite drives a stub provider that produces each of them.

### The provider's public address is asked for through its reachable one

A browser on the developer's machine reaches Keycloak at localhost:8180, so that
is the issuer its tokens name, and the api container cannot connect to that
address. The api connects to the address it can reach and sends the Host header
of the address the browser uses. The rewrite is one environment variable, off by
default, and only the api's requests to the provider go through it.

### A read token is refused in the api, before any handler

A token has always been its owner in miniature. The scopes column sat unread
since the first migration because no scope had a meaning anyone enforced; the
first one that does is read. The refusal lives in one middleware right after
authentication rather than in each handler, so a route added next year keeps the
promise without knowing it exists. The database is not asked to help: row level
security answers who may see a row, not which credential is asking.

### A key is narrowed where the permission set is built

A key was always its owner in miniature, resolved live: the role comes from
org_member on every request and the grants from role_assignment, so demoting
somebody narrows their keys before they finish the sentence. What was missing
was a way to make one narrower still, and a limit on what "everything its owner
can" includes.

Both are one edit, because the permission set is built in exactly one place and
every guard, every listing filter and both MCP and the assistant read it from
there. A key drops the permissions only a global administrator holds, and is
confined to the projects it names by rewriting organization-wide grants into
per-project ones. A route added next year is covered without knowing it exists,
which is the same bargain the read scope struck.

Two acts stay session only rather than being narrowed: archiving a project,
which takes it away from everybody, and making a key, because a key that can
mint a key outlives the revocation of the key it was made with. Account erasure
was already refused to keys and is now one of three rather than the only one.

What the database cannot help with is said plainly: no policy consults
role_assignment, so row level security isolates tenants and nothing else. The
confinement is proven instead by attacking it from outside, with the owner's own
session reaching what the key is refused, so the refusal is the key's and not
the person's.

**Reconsider if** a key needs to act for something other than a person, at which
point it wants an identity of its own rather than a narrowing of somebody's.

### A sign-in reaches what its proof vouches for

An account is global and a person may belong to many organizations, while
anybody can sign up and own one. Four doors of an organization opened a session
for an existing account without the person proving anything: an invitation
accepted with nobody signed in, an open desk door, a provider an owner
configured, and a six digit code that could be guessed at leisure because asking
again reset the count. Each of those sessions could then switch into the
person's other organizations.

A session now remembers how it was opened. A password, or an invitation that
made the account, reaches every organization of the person's; a mailed code, an
open door and a provider reach the organization that asked for them, and so do
export and erasure, which act on the whole person. The database keeps the rule
as well, because a column the code forgets to read is not a rule. An invitation
for an address that has an account is that account's to accept, signed in. The
open door admits only an address that was never more than a customer. Wrong
codes are counted per address for an hour across reissues.

**Reconsider if** signup gains address verification, at which point a verified
account could be let through more doors.

### A project is a wall inside the organization

The database wall between organizations was solid, and there was no wall at all
between projects inside one: `perm.Read` was defined and never checked, sixteen
routes addressed by an object's id asked only whether the caller held the
permission somewhere, and the lists answered across every project.

One guard now sits over every route below the organization: a project, an issue
or an object whose project the caller may not read is not there, and a path that
mixes two projects is not there either. Not found rather than refused, because
which projects exist is itself something to know. Lists narrow to the projects
somebody may read. The wall is the application's, not the database's, because a
project grant is a role somebody holds rather than a row a policy can compare;
the sweep over every operation in the API document is what keeps it honest.

**Reconsider if** a project ever needs to be readable by somebody who may not
read its issues, which would make Read a permission per kind of thing.

### The assistant proposes, the person acts

Ask lends the model the product's own tools under the asking person's
credential, which is what makes its answers true. It lent the writing tools too,
and ran them without asking. Everything the model reads was written by people,
so a sentence planted in an issue or a customer's request was an instruction
that ran with the reader's permissions.

A tool that changes something is now a proposal, and the change happens when the
reader presses Confirm. Reading is unchanged. Confirming is not a second
authorization: the person's permissions decide, as they always did. It is the
moment a person chooses, which is the thing a planted sentence cannot forge.

**Reconsider if** somebody wants the assistant to work unattended, at which
point the thing to confirm is a standing rule with a scope, not each change.

### The edges refuse what a browser would not send

A mail header was built by joining strings, so a newline in a name or a summary
wrote a header of the sender's choosing into everybody's mail; every header
value is now kept to one line. A write carrying the session cookie has to say it
is JSON, which a cross-site form cannot, and in production to come from an
origin of ours. Guessing at a password or an invitation is braked by the address
guessed at, not the guesser's own, since an office shares one address and a
guesser changes theirs freely; hashing is bounded, since argon2 is meant to be
expensive. A CSV cell that begins with an operator is a program in a
spreadsheet, so it is written as text. An unmapped database error no longer
becomes a 422 with its own words in it, readiness says whether rather than why,
and a link in a mail is followed only when it resolves to a path here.

**Reconsider if** the API is ever called by a browser on another origin, which
would make the origin rule a list rather than a yes or no.

### The server calls out only where the operator allows

A webhook endpoint, a git host and an identity provider's issuer are addresses
somebody types and the server then calls, and inside a server's network things
answer without asking who is calling. An address is now checked after it
resolves and dialled as the address that was checked, so a name that answers
twice cannot swap one for the other in between; loopback, private, link-local,
carrier grade and multicast are refused, at every hop of a redirect. An operator
who really does run a Gitea inside the network names it. What a host answered is
no longer read back to the caller, since the caller chose the address.

Inbound mail is the same question from the other side. A From header is a claim,
and the desk acted on it. Now mail the receiving server says is forged is
refused, and mail from a colleague's address is filed as an internal note,
because nothing proves it and agents answer in the app.

**Reconsider if** a deployment needs to reach many internal hosts, at which
point the allow list wants to be per organization rather than per process.

### A person is erased, their work is not

Erasure replaces the account with a tombstone and keeps the work attributed to
it, because the record belongs to the team: a comment with no author is a worse
record than one by "Former user", and an issue that loses its reporter loses the
one person the desk was answering. What only the person could use goes with
them. The last owner of an organization is refused, since erasing them would
orphan everybody else, and erasure is from a browser session only, so a leaked
token cannot erase its owner. An administrator removes a membership, not a
person. Retention windows are configuration with defaults, because what a law or
a contract asks differs by installation, and the record of processing is a
document in the repository, because a policy nobody can read is not one.

**Reconsider if** a legal hold is asked for, at which point a sweep needs an
exception and erasure needs a queue.

## The API, the worker and the suite

### The OpenAPI document is derived from the code, not written beside it

A YAML file maintained by hand is a second description of the API that starts
out true and drifts. Instead the router, the handler request types and the
domain response types are the description, and a table names which belongs to
which route. A test walks the router and refuses a route the table does not
know, or a row the router does not serve; another refuses a checked-in document
that differs from the generated one. What is lost is prose; what is gained is
that it is never wrong.

Every response the integration suite sees is validated against the schema for
its status, and the last test refuses a run in which any operation was never
answered successfully or never refused. That makes "each endpoint is tested" a
property the suite proves about itself. The validator is written here rather
than imported, because the schemas use a small part of JSON Schema and a
validating library would bring its dependencies into a binary the endpoint
protection agent may then kill.

### MCP is the same table, dispatched in process

A tool is a row of the operation table marked with a name and a sentence, and
its input schema is that row reflected by the same builder that writes the
document. A tool call is the HTTP call it stands for, built in this process and
run through the whole middleware chain as the caller, so the read-only rule, the
permission guards and the read-your-writes pin apply to it without knowing it
exists. The protocol is spoken with the standard library over one stateless
endpoint, which is one the integration suite can hold to the document like any
other.

### The assistant is an index before it is a model

A model would answer well; it would also need a provider, a key, a network and a
bill, and it would answer differently twice, which is a thing the browser suite
cannot hold to. So the guide is a catalogue in code: every page, setting and
action with a sentence and the words a reader might use, and the questions about
work that are really queries, compiled to the search page. Below a threshold the
guide says it has no answer rather than sending the reader somewhere plausible.

When a deployment names a model provider, the question the guide cannot answer
is offered to the model, in the same palette and nowhere else. The model is lent
the MCP tools as the person asking, so it sees exactly what they see, and it is
made to answer by calling one tool with a path from the guide's list, so it
cannot invent a page. The stack ships with no provider and the browser suite
asserts the unset state, because a model's answer is not a thing a test can hold
to; the integration suite holds the conversation to a fake provider instead.

### One fan-out, two channels: the inbox is the record and mail is a copy

One consumer of the event stream decides who is on an issue and why they should
hear, and writes one row per person per reason, keyed by the event's id. That
key is the whole idempotency story: the stream is at-least-once, the inbox is
exactly-once, and a replay cannot double-mail because mail is only sent when the
row was new. A preference is therefore a filter on delivery and never on what
happened, and a digest is the same rows read later. The desk keeps its own
notifier for customers, whose mails are worded for people without an account.

### A webhook is an endpoint, and a rule may aim at one

An endpoint is the object: an address, a secret, the topics it subscribes to,
and a log of every attempt. A subscription is the stream's consumer queuing a
delivery per matching event; a rule's "send a webhook" is the same delivery
queued by a different hand. Signing, retry with backoff, giving up after six
tries and redelivery therefore exist once, and the log an administrator reads is
complete whatever asked for the send. The secret is stored as it is, because
signing needs it; the git integration's inbound secrets stay digests, because
verifying does not.

### A rule runs as an account of its own, once per event

Running a rule as the person whose act triggered it is wrong: the person may
have gone, the rule may have been written by someone else, and every event the
rule causes would look like theirs. Every organization has an automation
account, an inactive member that never signs in. That is also the loop guard: an
event whose actor is the automation account does not start a rule unless the
rule says it may, with an hourly cap behind that. A run is claimed by inserting
a row keyed by rule and event before anything is done, so at-least-once delivery
is exactly-once for the rule. The automation's own topics never reach a webhook,
because an endpoint subscribed to everything and posting into an incoming hook
would call itself forever, and the browser suite found exactly that loop.

### The audit log is the outbox read by an administrator

Every administrative act already emits an event, because the webhooks and the
automation needed them, and an audit entry is that event seen by an
administrator. The worker copies the administrative topics off the stream, each
once by its event id, so the log, the webhooks and the rules all say the same
thing. Issue traffic is left out on purpose: each issue's history already holds
it, and a log that repeats every transition is one nobody reads. The acts that
happen before or beside a tenant are written by the act itself.

### Tracing is OTLP over HTTP, metrics are Prometheus on a private port

The metrics listener is its own server on its own port rather than a route: a
route would be in the API's document and in its coverage verdict, and the
exposition format is nobody's JSON. Traces leave over HTTP because a collector
on 4318 is the one thing every collector has, and because it means no gRPC
server has to be opened anywhere in this repository. The outbox row carries the
traceparent of the request that wrote it, so the span a consumer group opens is
a child in that trace: one trace for a request is the request, its statements
and everything the worker did because of it. Nothing runs unless asked.

**Reconsider if** a deployment wants metrics pushed rather than scraped, at
which point a collector takes both and the code changes one exporter.

### The page says when it is settled; the suite does not listen for silence

The browser suite waited for every page load with `networkidle0`, which is half
a second with nothing on the wire, paid on sixty-odd loads per ten scenarios.
Silence is a guess about the past; the page itself knows at once. React Query
counts what is in flight and the router says when it is between pages, so a
component writes the two into one attribute on the body. The signal is written
pessimistically: busy the instant any fetch starts, settled only from an effect
that runs after every child's, reading the live count. The suite reads it across
three animation frames rather than once, because React does not batch a query's
answer with the render that consumes it. Frames rather than a timer, because
they stretch with a busy main thread, which is exactly when the gap grows.

With the wait gone the scenarios were cheap enough to run four abreast, which
also caught a flaw the sequential suite never could: the freshness position was
read inside the writing transaction, before its commit record, so a replica
could be past the position without holding the change. It is read after the
commit now, and an integration test holds the two positions apart.

## The product's name

### The name is Armature, and nothing of NoJira was kept working

NoJira was a working title: it said what the product was not. Armature is the
frame a thing is built on, which is what a tracker is for a team's work.

The name was not only on the login screen. It was the Go module path, the
environment prefix every binary reads, three Postgres roles and the database
itself, the Compose project and therefore the network the test suites attach
to, the API token and webhook secret prefixes, the webhook signature header,
the session cookie, every Prometheus metric, the Redis stream key, the MCP
server's name, seven localStorage keys and three mail domains.

Nothing was aliased. There are no old environment names accepted beside the
new, no dual-emitted metrics, no deprecated-but-honoured token prefix, and no
read-the-old-key fallback for the browser's stored preferences. Anyone looking
for a compatibility layer should stop: it was never written. That was only
defensible because there was no remote and no deployed instance, so the only
cost was one developer's saved theme and a `make clean`. A rename after either
of those exists is a different job, and a much longer one.

It was done as five branches rather than one commit, each green on all four
layers before merging: the module path alone, then the strings spoken over a
wire, then the environment names, then the database and stack identity, then
what a person reads. The order matters in one place. `make clean` has to run
*before* the Compose project name changes, because the project prefix owns the
volumes and the network; renamed first, `clean` targets a project that does
not exist and leaves six volumes orphaned while the old containers keep the
ports.

The database branch had one failure mode worth writing down. Every role
reference in a migration is wrapped in `IF EXISTS (SELECT 1 FROM pg_roles ...)`
because `CREATE POLICY` against a role that does not exist is an error. The
roles are created by `deploy/postgres/init-roles.sh`, which Postgres runs only
on an empty volume. So a migration naming a role the script did not create is
*skipped rather than refused*: `ENABLE`/`FORCE ROW LEVEL SECURITY` and the
tenant isolation policies sit outside the guard and survive, but the admin
bypass that signup, login and the outbox relay need disappears without a word.
`TestEveryGuardedTableHasAnAdminBypass` now refuses that, taking the role's
name from the admin pool's own `current_user` so the assertion cannot drift
from the configuration, and catching any future table that forgets a bypass.

Two things the four layers could not see, both found by hand. The Keycloak
realm's client secret was renamed without the matching constant in the seed,
which would have broken the demo's single sign-on: no layer exercises OIDC.
And the documents are prose, so a stale metric name or bucket default in the
README is invisible to every test. The gate for the whole job was therefore
`git grep -i nojira` returning nothing, which is also what caught the Redis
stream key, spelled with a colon where everything else used an underscore.
