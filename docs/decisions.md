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
The pin only works if every write hands its position to its handler, so a
unit test refuses a service that throws one away, unless it is listed with
why no read of the caller's depends on it.

A session's position only ever rises. Requests finish out of order, so behind a
load balancer the older of two writes can report last; the shared tracker keeps
the greater position with a compare-and-set script in Valkey, as the
in-process tracker always has, rather than letting the late one walk it back.
replication, so a lagging replica costs latency and never correctness. A
replica leaves the rotation on the median of its last few lag readings
(`ARMATURE_DB_REPLICA_LAG_SAMPLES`), not on one: a single slow health check is
noise, and a replica flapping in and out on it sends reads to the primary in
bursts. A receiver that has stopped streaming takes it out at once.

**Reconsider if** replica lag is routinely under a millisecond and the pinning
machinery is buying nothing, or if the primary is so overloaded that fallback
reads become the problem they were meant to solve.

### Row level security in addition to tenant filters in queries

**Superseded** by [No row security: the walls inside an installation are the
api's and the triggers'](#no-row-security-the-walls-inside-an-installation-are-the-apis-and-the-triggers)
(#218).

Application-level scoping alone means every query is one forgotten `WHERE`
clause away from a cross-tenant disclosure, forever, in code nobody is reviewing
that closely any more. RLS turns that class of bug into an empty result set. The
cost is real: policies per table, no superuser connection, and an explicitly
exempt role for the few genuinely cross-tenant operations.

**Reconsider if** we move to a database per tenant, which makes the policies
redundant.

### A backfill lifts forced row security around itself

**Superseded** by [No row security: the walls inside an installation are the
api's and the triggers'](#no-row-security-the-walls-inside-an-installation-are-the-apis-and-the-triggers)
(#218): from `00932` on nothing hides a row from the owner, so the rule holds
for the migrations before it, and the check covers only those.

FORCE means the owner obeys the policies too, and a cluster migrates as a plain
owner with no organization set, so a migration that reads `org` there reads
nothing and fills nothing, without a word. The local stack migrates as a
superuser and never notices. A backfill therefore wraps itself in
`ALTER TABLE ... NO FORCE ROW LEVEL SECURITY` on `org` and on every table it
fills, and forces them again at the end, as `00904` does. Three backfills
written before the rule were rerun that way by `00926`.
`TestBackfillsReadingOrgLiftItsRowSecurity` refuses a migration that reads
`org` without the lift, and `TestMigrationsRunAsPlainOwner` migrates as such
an owner and checks an older organization comes out like a new one.

### No row security: the walls inside an installation are the api's and the triggers'

Row security kept organizations apart, and an installation holds one, so the
policies kept nothing apart. Migration `00932` drops them, the admin role's
bypass and FORCE with them, rather than replacing them with policies per
project (#218). It drops only the policies the migrations made, and stops at
any other, naming it, so a rule an operator added is not lost unseen.

The walls inside an installation are projects, the desk's customers and the
owners, and they were never the policies' to keep. A project grant is a role
somebody holds, a key narrows it and an open desk door scopes a session, and
all three live in the request, not in a row a policy can compare. Policies per
project would be a second copy of the permission model in SQL, run on every
row of every query, and still blind to the narrowing. So the api keeps the
project wall, with the sweep over every operation in the API document to keep
it honest, and the triggers keep the rules that must hold whoever writes: an
issue's sprint and versions are its own project's, a customer shares no
filter, the installation keeps an administrator. The tables kept `org_id`
until `00937` dropped it (#218).

What is given up is the database's second opinion on organizations, which
had nothing left to say with one in an installation. A query that forgets its
project's filter was never an empty result in the database; the per-route
guard and the sweep catch it, as before.

**Reconsider if** one installation needs a group inside it that the others
must not see at all, where a forgotten filter would be a disclosure rather
than a bug: a policy on that one wall, keyed on what a session can carry, is
the shape that would take.

### Keys are the installation's, without the organization in them

A name or key that was unique in each organization is unique in the
installation, and since an installation holds one, that is the same rule said
without `org_id`. Migration `00933` builds every primary key, unique index,
the absence overlap rule and the role assignment's foreign key again without
it, so a project key, and with it an issue key, names one project in the
installation (#218). The indexes that only led with `org_id` are built again
without it, except two that an index already there covers. `oidc_provider`
had nothing but the organization to key it by, so a column that can only be
true keeps it to one row.

The upgrade checks every new key before it builds one, and stops at rows that
would share it, naming them. With one organization there are none, and
`00929` refused several; the check is there for a database somebody wrote to
by hand. Its way back puts `org_id` into each key again.

An `ON CONFLICT` names the columns of the index it means, so the Go statements
changed with the keys. A desk's key can no longer name two desks, so the
`desk_ambiguous` refusal went, and so did the `ambiguous` outcome of a mail.
A mail recorded so reached no request, so the upgrade records it as
`unmatched`, which says that. The columns themselves went with `00937`.

### Triggers keep their rules without the organization

A trigger that held a row to its organization said nothing once an
installation holds one, so migration `00934` takes that part out of each
(#218). What a trigger holds a row to is kept: an issue's milestone, sprint,
team, versions, components and fields are its own project's, an article or a
rating belongs to a service desk, a customer joins no team and shares no
filter or theme, a role over a project is not one that answers for the whole
installation, and the installation keeps an active administrator and a default
workflow scheme. The membership checks read `org_member` by person, as its key
already did, and since `00938` the account's standing. Nine triggers held a row to its organization and nothing else;
they are gone, and the foreign keys keep what they also said, that the row
pointed at exists.

No trigger fires on `org_id` changing any more, so dropping the column later
takes none of them with it, and only the functions of the organization's own
table still read it. The role assignment's trigger fires on the role, its scope
and its subject, not on `created_by`: forgetting a granter as their account
goes grants nothing, and an account that granted itself a role could not be
deleted where the foreign keys happened to act in that order. Tombstones no
longer name an organization, which nothing read.

### The code no longer names the organization

Nothing binds a transaction to the organization any more (#218): the tenant
on the context, the `app.org_id` setting and the refusal of a transaction
without one are gone. Migration `00935` gives every `org_id` that must name an
organization a default, `installation_org_id()`, which reads the one `org`
row when a row is written. A fresh database has no row when it migrates, so a
literal would not do, and with two rows the default refuses rather than
guess. `current_org_id()` answers the same. No query names either any more:
a row is written without `org_id`, no filter asks for it, and no function
passes an organization's id along to reach the installation's rows. A unit
test reads every string in the Go module and refuses one that names them,
outside the tests about the column itself and those that run migrations from
before it went. A mail that matched no request and a file's
tombstone keep naming no organization, and so now does a mail placed on a
request, since nothing read which organization it was placed in.

The worker's passes ran once per organization, each inside its own; they run
once over the installation, retention as one statement per kind of row. An
event names no organization: the stream carries no `org_id`, and what reads an
event acts in the installation. A webhook's envelope keeps `orgId`, holding
the installation's stable id, the id of the `org` row, which the last step
keeps, so that receivers do not break on this upgrade. It is deprecated, and
goes with envelope version 2, as [A webhook's envelope has a
version](#a-webhooks-envelope-has-a-version-and-an-endpoint-keeps-its-own) says. The `member.joined` event's own
payload and an invitation in the API keep their `orgId` the same way. The previous release's consumers skip
an event without an `org_id`, so an upgrade drains the outbox and the stream
first, and workers of the two releases never run side by side.

### The tables no longer carry the organization

Migration `00937` drops `org_id` from the 94 tables that carried it, the two
session columns `00931` emptied with their check, and `current_org_id()` and
`installation_org_id()` with them (#218). By then nothing wrote or read
them: no query named the column, no key, index or trigger took it into
account, and every row was the installation's.

Postgres drops what depends on a column together with it, without a word, so
the upgrade looks first. It stops at a second organization, since no row
could then say which it belongs to, at anything that depends on the columns
beyond each one's own default, not-null rule and foreign key to `org`, such
as a view somebody made, and at a function whose body reads `org_id`, which
the catalog does not track. Each refusal names what it found and leaves the
database as it was. The drop itself runs without `CASCADE`, so what the
check did not foresee stops it too.

The organization's own functions kept their rules without its id: the five
built-in roles come from `builtin_roles()`, and the default theme has to be
a shared theme. The row is the installation's settings, and with nothing
naming it nothing could tell a second one apart, so a unique index keeps it
to one. Its way back adds every column again, filled with the installation's
id, so the release before this one runs on it unchanged.

What was left of the organization was names: the `org` row and the tables
`org_member` and `org_role`, which the code reaches without an id, and the
deprecated `orgId` that webhooks carry for one more release. `00938` renamed
them, as [The organization's tables are the
installation's](#the-organizations-tables-are-the-installations) says.

**Reconsider if** a column naming a group of rows is wanted again, for a wall
inside an installation: that is a new column with its own name, keyed on what
the wall is, not `org_id` back.

### The organization's tables are the installation's

The last of the organization was names, and migration `00938` gives them the
installation's (#218). `org` is `installation`, still one row. `org_role` is
`role`, and its `org_wide_only` is `installation_wide_only`. `org_invite` is
`invite`, and `org_join_request` is `join_request`. Every constraint, index and
trigger named for an old table is renamed with it. That includes the not-null
constraints Postgres 18 names after table and column, which a table's rename
leaves as they were: the installation's id was held by `org_id_not_null`.
Nothing in the schema is named for the organization any more, and a test
reads the catalog to keep it so.

`org_member` held one row per person, keyed by the account, with a standing
and nothing else anybody read. So it is a column of the account:
`app_user.standing`, owner, admin, member or customer, and NULL for somebody
who is no member, such as a follower known by a mail or a person erased.
Switching somebody off is still `is_active = false`, which keeps the
standing, so switching them on gives it back. A column needs no join to ask
whether somebody is here, and a check keeps it to the four. It is
`standing`, not `role`, because a role is what `role_assignment` grants
over projects; the standing answers whether somebody holds a seat and may
administer the installation. An invitation's `org_role` is its `standing`
too. The triggers that read the membership read the standing, with the same
rules.

A table's rename moves what depends on it, but a function body names a table
as text and breaks only when it runs, and what depends on `org_member` would
go with it unseen. So the upgrade stops first at a view, trigger, rule or key
on `org_member`, and at a function that names an old table, and names them.
Its way back makes `org_member` again from the standings and every name as it
was. A membership's own dates were never read, so they were not kept: the
way back dates each membership by its account.

The API keeps its words for one more release. `/auth/me` names the
installation, its id and name, and still carries `org`, marked deprecated, as
the webhook envelope, the `member.joined` event and an invitation still carry
`orgId`. The next release removes `org` and an invitation's `orgId`, with the
redirects from the old sign-in, desk and settings addresses. Removing a field
a webhook receiver reads is a break, so the envelope's and the event's `orgId`
go with envelope version 2, and an endpoint registered for version 1 keeps
them for at least one Dynamo release (#242). `orgWideOnly`, the `org.administer`
permission and the `condition.org_role` rule type are part of what clients
and stored workflows say, and keep their names.

**Reconsider if** a person needs more than one standing at once, such as a
customer of one desk who works on another: that is a table of standings
again, keyed by person and by what they stand in, not `org_member` back.

### Two database roles keep the secret tables apart

Row security was one reason for an admin role beside the app role; it went
(#218), and the roles stay for the other. `armature_app` does the ordinary
work. `armature_admin` serves sign-up, sign-in, the account paths and the
outbox relay, and it alone can read the secret tables: password reset
links, second-factor keys, recovery codes and sign-in challenges.
`00927` and `00928` take those tables away from the app role, so a query in
an ordinary path that reaches for one fails, rather than hands it over.

Outside development the api and the worker still refuse to start when the
app role is a superuser or may bypass row security, because a superuser
reads the secret tables past their grants. `TestOnlyTheAdminRoleReadsTheSecretTables`
holds both roles to their grants through SQL.

**Reconsider if** the secret tables move out of this database: the two roles
would then hold the same grants, and one would do.

### One installation is one team's tracker

Armature is self-hosted, and an installation serves one team. Organizations
were a layer such an installation never used: founding one at sign-up,
choosing and switching between them, slugs in sign-in and desk addresses,
rules for a person in several, and one copy per organization of settings
there is really one of. They are gone from everything people and the API see
(#208). The first account sets the installation up, a sign-in lands in it,
the desk's door is named by its desk, and the product says "the
installation", "everyone" or "all projects" where it said "the
organization". People who must not see each other's work run an
installation each.

Several organizations in one database are refused at the upgrade rather than
merged, as [One installation holds one organization, and an upgrade with
several stops](#one-installation-holds-one-organization-and-an-upgrade-with-several-stops)
says. Their project keys, statuses, workflows, roles and people would
collide, every rule for which one wins would be wrong for somebody, and a
migration would be choosing for them with nobody watching. Refusing leaves
the choice with the operator, who can delete what they no longer need with
the release that still knows how.

Inside, the product went first and the schema followed. The one organization
row held the installation's name and settings until `00938` named it the
installation, and since `00937` no table carries `org_id`. That kept each step shippable with all four test
layers green. The integration suite works in the one organization too, its tests
kept apart by people, project keys and names of their own (#217). The schema
ticket (#218) took the tenant out in steps: row security went first, then
the keys, the triggers, the code's own tenant, the columns and the names. The separation row security argued for, between teams, is now
between installations.

This supersedes the entries that described organizations as part of the
product. Three were marked by the step that changed them: [The owner always
administers their own organization](#the-owner-always-administers-their-own-organization),
[A sign-in reaches what its proof vouches for](#a-sign-in-reaches-what-its-proof-vouches-for)
and [An administrator makes a local user, and manages only what is
theirs](#an-administrator-makes-a-local-user-and-manages-only-what-is-theirs).
The rest are marked here. An entry that calls labels, fields, workflows,
roles or themes "the organization's" was written while that was the
installation's name, and its argument stands.

**Reconsider if** one installation routinely needs a group inside it that the
others must not see at all: that is a wall around people and settings, the
way a project is a wall around work, not organizations coming back.

### One installation holds one organization, and an upgrade with several stops

**Superseded in part** by [The organization's tables are the
installation's](#the-organizations-tables-are-the-installations) (#218): the
row is the `installation` table's now, and the `installation` package reads
it there. The refusal stands.

Armature is self-hosted, and an installation serves one team; running more
than one team takes more than one installation (#208). The organization row
stays inside as the installation's settings, and the
`installation` package is the one way to its name and to the settings there is
one of: the default theme, the second-factor rule and the automation account.
Moving them off `org` later changes that package and nothing that calls it.

A database that already holds several organizations is not merged. Their
project keys, statuses, workflows, roles and people would collide, and no
rule for which one wins would be right for everybody. Migration `00929`
counts the organizations and, with more than one, stops: it names the first
ten by address and name, says how many more there are, and tells the operator
to delete the extra ones with the previous release before upgrading. It
changes no schema, so a refusal leaves the database as it was. It lifts
forced row security on `org` to count, as every migration reading `org` did
while there was row security, and `TestTheUpgradeRefusesSeveralOrganizations`
refuses eleven as the plain owner a cluster migrates as.

**Reconsider if** operators with several organizations ask for a way across
that is better than deleting: an export of one organization's projects into
another installation is the shape that would take.

### The demo is two projects an administrator adds, once

The demo was an organization of its own, made and switched into with one
press, and went with switching (#212). An installation holds one team, so the
demo now comes into it: "Add the demo project" runs `demo.Factory` for an
administrator and leaves a production line and its maintenance beside the
installation's own projects, under `LINE` and `MAINT`. It goes through the
same services a person would, so it is not one transaction; both keys are
checked first, and a taken one refuses the whole demo with a sentence before
anything is made. That makes it once only, since a project is archived rather
than deleted and its key is never free again. The maintenance workflow, its
scheme and its waiting status are used where the installation has them by
name. The holiday calendars are left alone: days off are the installation's,
and the seed fills its own.

Nothing inside deletes the installation. `DELETE /organization` went with
the organizations it chose between; taking an installation down is its
operator's, with its database.

**Reconsider if** people want the demo out again: archiving its two projects
hides it, and removing it would have to take its statuses and workflow along
only where nothing else uses them.

### A transactional outbox rather than publishing events directly

Publishing to Redis inside a request means the event can be sent for a
transaction that then rolls back, or lost for one that committed. Writing the
event to a table in the same transaction removes the disagreement entirely, at
the cost of a relay and at-least-once delivery.

The worker keeps the promise to the end. A consumer acknowledges an event only
once it was handled, or once its handler says no attempt can help; anything
else stays pending, and every group claims what sat unacknowledged for a
minute, on start and every half minute, so a failover or a restart costs a
delay rather than the event. A shutdown finishes the event in hand on a context
of its own and leaves the rest of the batch to the next worker. After five
attempts the event is set aside: acknowledged, and written into the
organization's audit log as `event.dead_lettered`. The audit log is the
dead-letter record rather than a table of its own because its administrators
already read it, it is kept and exported on its own schedule, and the event
itself stays in the outbox.

**Reconsider if** we adopt a broker with transactional guarantees against
Postgres itself.

### The stream is trimmed by what was acknowledged, and the relay waits at the cap

The stream used to be capped by length, which trimmed whatever was oldest
whether every consumer group had read it or not: a 15,000 row import outran
the groups that write once per event, and their reads skipped the trimmed
issues without a word. Now the relay trims below the oldest entry one of the
worker's groups has not acknowledged, and adds nothing while the slowest group
is the cap behind. The cap still bounds what Redis holds; the outbox, which is
the record anyway, holds the rest until there is room. Holding back alone would
not have been enough, since a trim by length can still take an entry a group
was handed and has not acknowledged. A group no longer in `events.Groups` is
not waited for, or a feature removed would stop every event behind it.

**Reconsider if** a consumer needs to replay the stream from further back than
its group's position, at which point the stream wants a retention of its own.

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
about the bucket knows who may read what: the row in `attachment` does, behind
the same project wall as everything else. A presigned URL would be faster
for large files and would put a second access control system beside the first.

An upload writes the row and then the object, in one transaction; a delete does
the reverse, row inside, object after the commit. Both orders leave at worst an
object nobody can reach, and neither ever leaves a broken link. Orphans are not
forgotten: the transaction that removes a row writes a tombstone per object key,
and the worker's reaper works from those. A trigger on the table writes it, not
the service, because rows also leave by cascade, with an issue's whole subtree
or with the organization, and no service sees each of those. The bucket is never listed, because
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

### A type change that moves the status is recorded as a move

An issue given a type whose workflow lacks its status starts over in that
workflow's first status, and that is a status change like a transition's: it is
in the history, announced as `issue.transitioned` (named "Type change"), told to
the observers so the desk's clocks follow it, and `resolved_at` is set or
cleared by whether the new status is done. Asking for the target status, as a
move to another project does, would put a question in front of every type change
for the rare one that leaves the workflow; a silent move left a reopened issue
counted as done by NQL `resolved` and the reports.

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

### An edit names the copy it began from, and the later of two saves is refused

An edit of an issue may carry `expectedUpdatedAt`, the `updatedAt` of the copy
the editor opened. The service compares it under the row lock and answers 409
when the issue has moved on, so two tabs on one description cannot erase each
other's words without anybody noticing. The issue's own `updatedAt` is the
version, not a counter of its own, because the column and its trigger already
exist and every write moves it. The browser takes the stamp when editing
begins, not at save, and on a refusal keeps the draft and shows the newer text
beside it; saving again after that replaces what was shown. A client that sends
no stamp is let through as before, since scripts and imports set fields they
did not read. A save that changes nothing, a description equal as jsonb or the
same due date, writes no row, no changelog entry and no event.

**Reconsider if** people edit different fields of one issue at once often
enough that a refusal for an unrelated field gets in their way; then the stamp
becomes per field.

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

A sprint holds only its own stream's work. Committing an issue to another
team's sprint, or carrying unfinished work into one, is refused by the service
and by a trigger comparing the issue's team with the sprint's. Handing an issue
to another team takes it out of an open sprint of its old stream and into the
new team's backlog, and the changelog says so. Refusing the handover instead
would make the person find and empty the sprint first, and deciding when the
work is planned is the receiving team's job anyway. A closed sprint keeps its
work whoever carries it later, since its report is already written.

### A team narrows access and never widens it

Only somebody already in the organization can be put on a team, enforced by the
service and by a trigger. The alternative is a team that can pull people in,
which turns every team into a way around the organization's own membership.
Deleting a team with work in it is refused rather than quietly returning the
work to the project, because work that was somebody's and is now nobody's is
discovered weeks later and the fix by then is archaeology.

### A team that has run a sprint is kept

Deleting a team is refused while it still has sprints or request types routed
to it, by the service with a sentence and by the database, whose foreign keys
from `sprint.team_id` and `request_type.team_id` no longer null the column.
Nulling them made a team's running sprint the project's, or collided with the
project's own running one, and quietly stopped requests routing. Sprints that
have not started can be deleted and request types routed elsewhere, after which
the team goes. A sprint that has run is the record of what the team delivered
and cannot be deleted, so a team that has run one is renamed rather than
deleted. Deleting its sprints with it would rewrite history, and archiving
teams is a feature of its own that nothing yet needs. The keys are NO ACTION
rather than RESTRICT so that an organization deleted with everything in it
still takes its teams and their sprints along.

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
in a backlog and a sprint, or in two backlogs, or in neither. It is the
unfinished work in no sprint, in rank order; done work in no sprint is history.
The Sprints page reads the backlog and each sprint from the server, every page
of them, rather than filtering the project's newest issues: a page of those
lost older committed work, and a sprint that looked empty was offered for
deletion.

Swimlanes, their limits and the grouping belong to one board, so they are
written through routes that name it (`/boards/{boardID}/swimlanes`, `PATCH
/boards/{boardID}`). The project-addressed routes they replaced could only mean
one board, which was always the first, and the grouping one regrouped every
board in the project; they are gone rather than kept as a second address for
the first board.

### Team capacity is points per week, spread over the days a ticket is scheduled

Capacity lived only on the sprint, which says nothing about a project that plans
in weeks and months. A team says how many points it can take in a week, in the
unit its issues are already estimated in; hours would have put a second unit
beside the first. Load is read off the schedule, each estimate spread evenly
over the working days between start and due date and summed into Monday weeks,
with the same count-once rule the sprints use. A week over its capacity is a
warning with the team as its subject, never a refusal.

A working day is one some member's week holds and the default calendar does not
take whole; a team of nobody works Monday to Friday. Spreading over every
calendar day put two sevenths of a week's work on Saturday and Sunday, so a
Monday to Friday ticket read lighter than it was. A ticket scheduled on no
working day at all keeps its points over its calendar days rather than losing
them.

The points a team said are what it can take with everybody there. Each week's
capacity is scaled by the minutes its members have left after holidays and
absences against the minutes their weeks give, so a fortnight with two people
away warns where it would otherwise have read as fine. The scaling is a ratio,
not a conversion: points stay the one unit, and the sprint shows the team's
hours only as a hint beside them. Absences move capacity, never the work, since
a ticket does not reschedule itself because somebody is on leave.

The load is rows under the plan rather than a band over it: a band per team
would swallow the calendar in a project with several, and rows read where a
manager reads them, under the work they summarise.

The load is read over at most ten years of the window, from no earlier than two
years before today, and an issue is scheduled between 1900 and 2199, in the
service and in a CHECK. The plan read everybody's every day across the window,
so one due date typed as 2206 for 2026 made each page view read two centuries
of availability; work planned a few years ahead still has its load.

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
A sprint's last word is the one completion wrote, before its unfinished work
moved on: the worker writes a sprint's day only while holding the sprint's lock
and finding it still running, so a sprint completed after the worker listed it
keeps its point.

Today is read live and every earlier day from its snapshot, so the chart agrees
with the widget to the second and the browser suite is deterministic without a
scheduler. Created and resolved times are columns and never renamed, so the
reports over them need no snapshot; the control chart measures from the first
step into an in-progress status, because the question is how long work takes
once somebody is on it, and the resolution histogram from creation, because the
question is how long the asker waited.

The release burndown counts an issue on a day only if it had been filed by
then, so work added to a version halfway lifts the line rather than hiding in
its first day, and a released version's line stops on the day it was released.
An issue that joined the version after it was filed still counts from its
filing: which version an issue fixed on a given day is in the changelog only
by name, for the reasons above, so the join day is not read.

### Holidays are named calendars a person is given, and a week is minutes per weekday

A team spread over several countries does not share its days off, so one list
per organization would be wrong for half of it. A calendar is named, a person is
given one, and whoever has not been given one keeps the organization's default,
which every organization is made with and which cannot be deleted while it is
the default. A day off is a date rather than an instant, so no time zone moves
it, and a half day halves what the person would have worked.

The week is minutes per weekday rather than a flag per day, because part-time is
the common case and a flag cannot say "mornings on Wednesday". Minutes because
worklogs and estimates are already minutes. Somebody with no week set works the
standard one, eight hours Monday to Friday, so nothing has to be filled in before
planning can read it; a portal customer has none, and a trigger says so too.

An .ics import takes all-day events and refuses a recurring one rather than
expanding the rule: RRULE is a language of its own, and a wrong guess about a
moving feast is a day off in the wrong week that nobody notices. Calendar
applications export the days written out, and the refusal says to do that.
An import lands on the server, not in the table being edited, so while that
table holds unsaved days the import asks before it discards them rather than
merging the file into a draft the server has not seen.

This is not the service desk's business calendar, which says when a desk is
open for its clocks and answers a different question. A desk could later point
its calendar at a holiday calendar instead of keeping a list of its own.

### An absence says when, never why, and nobody approves it

An absence is a first day, a last day and whether it is half of a single day.
There is no reason column and no note: a colleague planning a sprint needs to
know that somebody is away, and nobody needs to know whether it is leave, a
doctor or a funeral. Leaving the column out makes "colleagues see only that you
are away" true by construction, where a hidden field would be one query away from
showing. The events say an absence was recorded and for whom, never the days,
since the stream reaches webhooks.

Nothing is approved. The tracker plans around absences; it does not grant them,
and an approval step would make it the leave system of record, which every
company already has somewhere else. So the person records their own, and the
people who plan with them may record one on their behalf: an administrator, or
whoever holds the permission to manage teams in a project where the person is on
a team. Leading a team is not enough, since a lead is a person to ask rather
than a permission. Two absences of one person over the same day are refused by
an exclusion constraint, so the plan never counts somebody away twice.

A manager may change or remove only the absences they recorded themselves.
The same manager decides who is on their teams, so "manages a team the person
is on" is a door they can open for anybody, an administrator included, in one
request; checking how long the person has been on the team would only mean
waiting a minute. So the reach is narrowed instead: a manager adds days, and
the person's own entries and an administrator's are out of it. The person and
an administrator change any of them. A refusal names the person, so the
manager knows who to ask. Who recorded an absence is what decides, so the
database never lets it be rewritten to somebody else; it becomes nobody only
when that account goes. And it is made visible: whenever somebody other than
the person records, changes or removes one of their absences, the person is
told in the inbox, and by mail if they want it, with who did what and never
the days, since a mail leaves the building.
An absence may start any day ahead, so the list a person changes theirs in
runs from a month back with no last day: one saved fourteen months out is
listed the moment it is saved. A list from a day holds at most
`MaxOpenAbsences` and says when it was cut; a list between two days covers two
years at most and is never cut.

**Reconsider if** an organization needs leave approval inside the tracker, which
would be a state on the absence and a person who decides, not a reason.

### Scrum plans by team; kanban chooses team or person

A project has a planning method, scrum or kanban, and a grouping for its
resource view, team or person. Scrum is planned by team because a sprint is a
team's commitment: setting one person's hours against work the team took on
together reads a shared promise as one person's debt. A kanban project has no
sprint to keep together, so it may set the work against each person's own week,
which is how a small project of specialists is usually run.

The method is backfilled from the boards, scrum where a project has a scrum
board, and a new project takes it from its template. It is a column of its own
rather than read off the boards, because a project may have boards of both types
and the resource view needs one answer. A check constraint holds "scrum plans by
team", so a row written past the service is refused too. Turning a project to
scrum takes its grouping back to teams rather than refusing the change; asking
for people on a scrum project is refused with a sentence that says to switch it
to kanban first.

### Resource planning is in hours; team capacity stays in points

The resource view sets each open issue's remaining time, or its estimate where
nothing remains to say, against the hours its team or person has in each Monday
week after holidays and absences. Hours, because availability is measured in
them: a working week is minutes per weekday, and a day away takes hours, not
points. Points measure a team's pace against itself, and a person's day has no
pace; converting one into the other would need a rate nobody can state.

The team load under the plan stays in points, scaled by the same holidays and
absences, so a team that estimates in points is not made to estimate twice.
Work is spread evenly over the row's working days between start and due, as the
team load is; a person's working days are their own, so an absence moves their
share of an issue onto the days they are there. Work with hours but without both
dates, and work with no hours at all, is listed rather than guessed into a week.
A week over its hours is an over-allocated warning, never a refusal. Work is
counted once per row, at the highest open level that carries hours: a story
sized at sixteen hours speaks for its subtasks, as an estimate does in a sprint,
and a subtask handed to another team or person counts there on its own.

A project may have only part of somebody's week. A project administrator sets
the share in percent, and the person's available and nominal hours are counted
at it, on their own row and inside their team's; nobody set gives the project
the whole week, which is what every project assumed before. The shares a
person has elsewhere are added up beside it, so a week given out at 130% is
seen and warned about, as capacity is, and never refused: the share is what
the project was told, and the warning is the place to argue about it. The
total names no other project, so a walled one is not found through it.

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
not a second "#close". A squash or merge commit arrives as a new commit carrying
the branch's messages, so each command carried out is also recorded against the
repository and the issue, by its kind and its text with the spacing evened out.
The same command from the same repository on the same issue is then left alone,
whichever commit repeats it, while a line the squash commit adds still runs. A
command the workflow refused was not carried out, so it is not recorded and a
later commit may ask again. Saying the same thing again on purpose means saying
it from the issue, or in other words. Only a hash of the command is kept, since matching is all it is
for. A transition the workflow refuses goes into the receipt with its reason and
the commit is recorded regardless, because the recording is what the history
needs.

Comments back to a pull request are posted by the worker reading the event
stream. Posting from inside the transition would make every transition wait on a
host, and fail when the host does. A host that refuses is tried again like any
failed event, but not once another pull request was told, since a retry would
tell that one twice; the event is then set aside, not retried forever.

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

### A merged branch outlives its deletion; an unmerged one does not

GitHub and GitLab both offer to delete a branch once its pull request is merged,
and most teams say yes, so deleting the row with the branch took the merge off
the issue the moment it happened. A merged branch the host deletes is kept and
marked deleted, and the panel says "merged, deleted" without linking to an
address the host no longer has. An unmerged branch that is deleted is still
removed: it was abandoned, and its commits stay linked to the issue on their
own. The database holds both halves: a deleted mark needs a merge, and a name is
unique only among the branches still there, so the same name pushed again later
is a new branch with its own count, not the old one revived.

**Reconsider if** people want to see abandoned branches too, at which point an
unmerged one is kept and marked the same way and the check goes.

### A commit's CI is every check on it, added up

A host sends a run per job and per workflow, so the run started last says
nothing about the others: a failed test followed by a passing lint read green.
The answer for a commit, and so for a pull request's head, takes the latest run
of each check name, and a failure outweighs one still running, which outweighs
a cancellation, which outweighs a pass. A cancelled check never passed, so it
does not read as green. The checks behind the answer travel with it, and the
issue's panel lists them, so a red badge leads to the check that failed.

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

Moving to the done category completes every clock still open, first response
as well as resolution, because a closed request has nobody waiting for an
answer and a clock left running would only ever end in a breach. A breach that
had already happened stays recorded, and one the worker had not seen yet is
recorded as the clock completes, so closing a request never hides a missed
goal. Reopening starts a fresh resolution clock; first response stays answered
once it has been, by a reply or by resolving.
The clocks follow the request, not only its status. A new priority or request
type looks the policy up again and moves each open clock to the goal it now
has, keeping the time already run: the customer has been waiting that long
whatever the label says. A goal the clock has already used up is a breach at
once, a breach already recorded stays, a finished clock keeps the goal it
finished against, and a priority the policy does not measure takes its open
clock off. Editing a policy is different: that rewrites the desk's promise to
everybody, so clocks already running keep the goal they started with.

Moving an issue out of a desk takes its clocks off with the sprint, the versions
and everything else that belonged to the old project, so nothing is left for
the watch to breach. Moving it into a desk starts that desk's clocks from
nothing, paused when it lands in a pause status and not at all when it lands
done. Refusing the move instead would make a misfiled request impossible to
put right, and carrying the old clocks over would hold one desk to another's
goals. Subtasks that come along are treated the same way.

A goal measured in business hours is arithmetic over a zone, a set of daily
spans and a list of holidays, which in SQL means either a table of every working
minute or a function taught about daylight saving. So the database finds the
suspects with a wall-clock comparison, which contains every calendar breach and
some false ones, and the Go clock decides.

Two spans of one day that overlap are refused when the hours are saved, naming
both, because the person who typed them meant one stretch and should say so.
The database does not refuse them: the hours are a JSON document it does not
read, and a calendar saved with overlaps before the rule must still load. The
clock merges a day's spans before counting, so such a calendar counts each open
minute once.

### A customer's reply moves the request back, by the workflow's own way

The resolution mail tells the customer that a reply brings the request back, so
a reply to a request in a done status takes the workflow's way out of it as the
customer: the transition called Reopen, else the first one from that status
that ends outside done. It is a transition like any other, so the resolution
clock starts over and the rating stays as history. A follower's reply, by mail
or in the portal, counts the same as the reporter's. A workflow with no way out
keeps the request where it is, since the desk's workflow is the desk's to
design, and the agents on it, or whoever finished it, are told in the inbox.

### A queue is ranked by the database, by the wall clock

A queue is ordered and paged in SQL: breached first, then the least time left,
then requests with no clock running, newest first, with the total beside the
page. Reading a fixed number of the newest requests and sorting them afterwards
lost the oldest ones on a busy desk, and those are the ones most likely to be
overdue. The time left is the goal less the banked seconds less the wall time
since the clock last started, which is exact for a goal in plain hours. For a
goal in business hours the wall clock counts the closed hours too, so such a
request can rank above where its own clock would put it but never below, and
it counts as breached once the watch has recorded the breach, within its
thirty seconds. Storing a due time instead would be exact, at the price of
keeping it right on every pause, resume, goal and calendar change.

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

### A rating is asked of the reporter alone

A resolved request invites one rating, through a link minted once and good for
one answer, so whoever holds it can rate. The reporter's copy of the resolution
mail carries it; followers and watching agents are told the request is resolved
without it, because the score is the customer's verdict on the desk and an
agent or a colleague who rated first would leave the customer's click with
nothing to do. The page behind the link asks for no sign-in, which is what
makes one click enough and why the link goes to one person.

**Reconsider if** a desk wants followers' opinions too, at which point each
recipient gets a token of their own and the report counts people, not requests.

### Replies come in by POP3, and are accepted from whoever could have typed them

An IMAP library is a large tree of code for six commands, on a host whose
endpoint security kills binaries it finds suspicious. POP3 is served by every
provider and by Mailpit, and a desk's mailbox is a queue rather than an archive,
so fetch and delete is the right shape. The reader consumes only what is
addressed to the inbox, remembers the rest by uid, and deletes on QUIT, so a
connection that drops leaves the box as it was.

A mail names a request by the key in its subject or the id of the mail it
answers, and the sender is part of the question: the reply is accepted only
where the address belongs to the reporter, a follower or an agent. A key names
one request in an installation, so there is no "ambiguous" outcome; were a
person in several organizations, the one they joined first would be the one, as
a session's is (#214). Since #217 not even the integration suite makes a
second. Every consumed mail is recorded by message id, so a redelivery is
refused by the database however many workers are reading, and machines are
never answered, so no loop can form.

Nor is a machine's mail taken for the customer's word. An out-of-office that
reaches a request waiting on its customer would otherwise be posted as their
reply, move the request back and restart its clocks. A mail marked by
`Auto-Submitted` (other than `no`), `X-Autoreply`, `X-Autorespond` or a
`Precedence` of `auto_reply`, `bulk`, `junk` or `list` is recorded as
`automatic`, taken out of the box and never posted.
A mail is deleted only once it is decided. The row is written before the work,
so an attempt that fails marks it failed with its error and the next poll takes
it up again; a row another attempt still holds leaves the mail in the box. After
ten failed attempts the desk gives up: the mail stays in the box, since the body
lives nowhere else, and an internal note on its request tells the agents why.
What goes out is seven bit: headers that are not ASCII are RFC 2047 words and
the body is quoted-printable, since a relay without SMTPUTF8 refuses anything
else. Every mail about a request answers one id, `<KEY.request@host>`, that no
mail carries, so a client files the receipt, the replies and the resolution as
one thread whether or not the receipt was ever sent. Each mail keeps an id of
its own beginning with the key, so a reply matches whichever one it answers.

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

### A negation keeps the issues with nothing there

A field an issue leaves empty matches no positive test, so `!=`, `!~`,
`NOT IN` and `NOT` all count the issue on the "not" side: an issue with no
description does not mention payment, and an unassigned issue is not
yours. Jira leaves such issues out of `!=`, which is the surprise people
ask about; here `IS NOT EMPTY` says it when that is what is meant. The
compiler wraps each negated test in `COALESCE(..., false)`, since SQL's
`NOT NULL` would drop the row.

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
and files hang off the row. Every key it leaves is kept beside it, one row each
and unique in the installation, so every address that was mailed around still
finds it however often it moves again, and a reply by mail under one still
lands on the request.
done. Every step on that issue runs in its transaction, through the `In`
variants of the single-issue methods, so a refused issue keeps nothing of the
edit: no field, no history row, no event. Its fields are recorded and announced
as one change, the way one edit is, and a transition after them as its own. A
moved issue keeps its row and gets a new key, because history, comments
and files hang off the row; the old key stays on the row so the address that was
mailed around still finds it. Everything under it moves too, at every depth and
parents first, in the same transaction. A descendant whose status its workflow
in the target does not have refuses the whole move and is named, rather than
landing in a status nobody chose or staying behind under a parent elsewhere.
mailed around still finds it. What the move leaves behind is only what belongs to
the old project: answers to organization fields go along, since those fields
apply in every project, and each answer to one of the old project's own fields
is named in the history as it is dropped, so nothing disappears unsaid.

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
and drop what belongs to one project, team, sprint, milestone or version by id,
while the filter's names travel. A template saved before the version was dropped
is read without its ids too, so no row needs rewriting. A template is not
edited: it is removed and saved again.

A milestone's dashboard is the same copy pinned to a name and an id, set in one
transaction so they cannot disagree; closing or deleting the milestone leaves
the dashboard where it is.

### A share link is a read that names its tenant

A dashboard on a wall in the lobby has nobody signed in to it. A share link is
the first anonymous read: a random token whose digest finds the dashboard,
after which the ordinary report code runs. What a visitor can ask is each widget of that one dashboard, by
id, with the widget's own stored settings; there is no route that takes a kind
and parameters, so a widget that opens more on a click, like a past sprint's
curve in Sprint history, offers nothing to click on a shared page. The filter is frozen when the link is made and compiled as the
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

### An export is cut at five thousand issues, and says so

An export asked the issue list for five thousand rows, and the list, which
keeps a page to two hundred so one request cannot hold the database, gave it
its default fifty without a word. The export now reads the list a page of two
hundred at a time, up to five thousand issues. A longer result is cut there
rather than refused, since the first five thousand in key order are still
useful, but the file ends with a row saying how many of how many it holds and
to narrow the query, and the response carries `X-Export-Truncated: true`. The
list breaks ties by the issue's id after its key number, because key numbers
repeat across projects and a page boundary between two equal ones would
otherwise show one issue twice and drop another.

**Reconsider if** somebody needs a whole organization in one file, at which
point an export becomes a job that writes to storage and mails a link.

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
kind, so Board stays Board in the other project (a sprint's board becomes the
other project's Sprints, since the sprint is not theirs), and the command
palette is the keyboard's sidebar. Below 1024px the sidebar becomes a rail of icons.

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

### A refused change says why, once

An assignee, a priority or a saved search the server refused flipped back and
said nothing, because each control called `mutate` and nobody looked at the
error. Most controls do show their own failure beside themselves, so a toast
for every failure would say most of them twice. The query client's
`MutationCache.onError` toasts the server's sentence unless the caller shows
the failure itself, and every mutation is built on `api/mutation.ts`, which
knows: a caller that reads `error`, `isError` or `mutateAsync`, or passes
`mutate` an `onError`, has taken the failure on. `meta: { handlesError: true }`
says so outright for a caller that reports it some other way. A unit test
refuses a mutation built straight on TanStack's hook.

The alternative was marking each of the hundred-odd mutations that show their
error by hand. That is a mark one new control forgets, and a hook shared by a
silent field and a talkative one cannot carry both answers.

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

The same holds for the dates a form works out: "today" and "the end of that
day" are the profile zone's, so a work log starts on the day the reader is in
and a share link lasts until midnight where they are. A week that starts at
midnight UTC is a date, not an instant, and is read in UTC so no zone moves it
onto the Sunday before.
Pages kept slipping back to en-GB or the browser's language one helper at a
time, so a unit test now reads the source: outside `lib/format.ts`, no date
or number is written with `undefined`, no locale at all, a literal locale or
`navigator.language`. Helpers take `format.locale` instead.

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

**Superseded** by [The installation keeps an active administrator](#the-installation-keeps-an-active-administrator)
(#211): the owner keeps the standing grant, but what is kept is somebody active
to administer, owner or not.

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

### The suite signs in through one realm, and Dynamo holds it

Stator, Armature and Exciter each ran a Keycloak realm of their own, with their
own people and secrets, so one sign-in never carried across and Dynamo could
not start a single Keycloak for the three. They now import the same file,
`deploy/keycloak/realm.json`: realm `dynamo`, a client per tool, the same
redirect patterns and `groups` claim, and the people alice, bob, carol and dave.
Each tool uses only its own client; the others are there so one file serves the
suite. Armature's groups sit beside the partners' ones, and the seed lets the
four people in and gives the groups their roles.

Dynamo holds the canonical copy and each tool's is the same file, keys sorted.
`make realm-check` compares the two and fails naming the file to update; while
Dynamo has no realm yet it says so and passes, and Armature's copy is the one
Dynamo will start from. The secrets in it are for development only. The seed
and the realm once drifted unnoticed (see the product's name, below), so a unit
test reads the realm and refuses a seed whose issuer, client, secret, groups or
people it does not hold.

**Reconsider if** the tools need people or groups the others must not see: then
each keeps its realm, and Dynamo gives up starting one Keycloak for all three.

### A read token is refused in the api, before any handler

A token has always been its owner in miniature. The scopes column sat unread
since the first migration because no scope had a meaning anyone enforced; the
first one that does is read. The refusal lives in one middleware right after
authentication rather than in each handler, so a route added next year keeps the
promise without knowing it exists. The database is not asked to help: row level
security answered who may see a row, not which credential is asking.

### A key is narrowed where the permission set is built

A key was always its owner in miniature, resolved live: the role comes from
the account's standing on every request and the grants from role_assignment, so demoting
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

What the database cannot help with is said plainly: no policy consulted
role_assignment, so row level security isolated tenants and nothing else, and
since #218 there is none. The
confinement is proven instead by attacking it from outside, with the owner's own
session reaching what the key is refused, so the refusal is the key's and not
the person's.

**Reconsider if** a key needs to act for something other than a person, at which
point it wants an identity of its own rather than a narrowing of somebody's.

### Nobody is signed in outside the organization

**Superseded in part** by [The organization's tables are the
installation's](#the-organizations-tables-are-the-installations) (#218): every
principal carries the installation, and a membership is the account's
standing. The refusal stands as written.

Somebody signed in with no organization saw a shell of empty tables, and
every guard had to ask whether there was a tenant at all; a forgotten check
was a query with no tenant scope. Now every principal carries the
organization. A password sign-in, its code step and an API token of somebody
with no membership left are refused with "Your account has been switched
off. Ask an administrator.", the same words whether they were let go or the
organization went. An open session whose membership is gone ends rather than
being refused, so the browser goes back to sign in and hears it there.
Single sign-on already opened no session without a membership: it notes a
request to join instead, which an administrator can admit.

### A sign-in reaches what its proof vouches for

**Superseded** by [A sign-in lands in the installation, with one provider and one rule](#a-sign-in-lands-in-the-installation-with-one-provider-and-one-rule)
(#212): with one installation there is no other organization to reach.

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

### An administrator makes a local user, and manages only what is theirs

**Superseded** by [An administrator manages every account, and letting go is switching off](#an-administrator-manages-every-account-and-letting-go-is-switching-off)
(#211): with one installation there is no other organization whose interest
an account's could be.

Signing up makes an organization and an invitation needs a mailbox at the
other end, so a company that keeps its people off mail, or a desk that wants an
account ready before the person arrives, had no way to make one. An
administrator now types an address, a name, a standing and a password, and the
account exists; the password is told to the person another way and the
profile is where they change it, ending every other session of theirs.

An account is global and a person may belong to many organizations, so what
one organization may do to an account stops where another one's interest
begins: renaming, resetting and switching off are offered only for a person
whose one organization this is. Anybody else is somebody another organization
also relies on, and the most this one may do is let them go. An address that
already signs in anywhere is refused with a pointer to invitations; an address
that is only a passwordless row of this organization, made by an import, a
mailed code or being named as a follower, is claimed and becomes the account.
Only an owner touches another owner, an owner's standing is not changed here,
and a reset or a switch-off ends every session and token the person holds,
because a password that leaked stops working the moment it is replaced.
The last active owner of an organization cannot be switched off: the service
refuses it and a trigger refuses the same row, the way the last-administrator
check is kept twice.

**Reconsider if** an organization needs to suspend somebody it shares, at
which point suspension moves onto the membership rather than the account.

### The first account sets the installation up, and sign-up asks for nobody else

An installation serves one team (#208), so a sign-up has no organization to
name. The page asks for the person alone. On an installation with no
organization yet, the first account makes it, named by `ARMATURE_NAME`, with
the statuses, types and workflow a project needs, and owns it; a lock taken in
the same transaction stops two first accounts racing. After that
`ARMATURE_SIGNUP` decides: `first`, the default, closes the page; `open` keeps
it open, and whoever signs up joins as a plain member; `closed` never offers
it, not even to the first. No sign-up makes a second organization, so the
upgrade's refusal of several (`00929`) never has a new one to count. Invitations
lead into the installation and its mail and page name it. The development
stack runs with `open`, so its sign-up form stays to be tried, and the browser
suite's administrator, the seed's Ada by default, signs up through the page
when it finds the installation empty.

The integration suite kept its tests apart by organization until #217, which
moved every test into the one installation, kept apart by people, project keys
and names of their own. The tests of sign-up itself run against a database of
their own, migrated from nothing, which is the only place an installation is
reliably empty.

**Reconsider if** an installation wants people to sign up only from its own
mail domain, at which point `open` takes a domain rather than growing a fourth
value.

### An administrator manages every account, and letting go is switching off

Every account belongs to the installation, so an administrator renames,
resets and switches off anybody but themselves; only an owner touches another
owner, and an owner's standing is not changed here. Letting somebody go, from
the API's `DELETE /members/{id}` or the users page, is switching their account
off: every session and token ends and the address no longer signs in, with
the same sentence as any other switched-off account. The account, its work,
its groups and its absences stay, so switching on undoes it, and erasure is
what takes a person out. The resource plan leaves a switched-off person out,
and what is assigned to them counts as nobody's. An address that already signs
in is refused with a pointer to the list; a passwordless row, such as a desk's
customer, is claimed and becomes the account.

API tokens and pictures follow the person rather than an organization: a
token is its owner's, and anybody signed in sees anybody's picture. An act on
the account itself, such as a reset password or a second factor turned on, is
recorded once, in the log of the person's organization, rather than once per
organization.

### The installation keeps an active administrator

The last active administrator, owner or admin, cannot be switched off and
cannot erase their account: the service refuses it with a sentence that says
to make somebody else an administrator first, and the trigger
`app_user_keeps_an_administrator` (migration `00930`) refuses the same row,
because a column the code forgets to check is not a rule. An owner beside an
active administrator may now be switched off; the owner's standing grant still
means an owner who is active always administers.

### A sign-in lands in the installation, with one provider and one rule

An installation serves one team (#208), so there is nothing to choose after
signing in and nothing to switch to. `POST /auth/switch-org`, the chooser on
the sign-in page, the sidebar's switcher and the profile's list are gone, and
`/auth/me` no longer lists organizations. A session names no organization: it
acts in the person's own, read from the membership at every request, and the
database refuses a session row that names one (`00931`). The columns stay,
empty, until the tenant goes (#218). Nobody can be in a second organization:
since `00933` a membership is keyed by the person alone. A session through
an open desk door acts in that desk's organization and sees that desk alone.

What made a session reach further is gone with it: `session_reaches`, the
trigger that applied it, the trust between organizations sharing an issuer,
and the `session_bound` refusal. The proof stays, and the database still
refuses rewriting it. An open door takes an address on trust, so its session
may not export or erase the account behind it; a mailed code may.

Single sign-on starts at `/auth/oidc/start`, at the installation's one
provider, and the sign-in page offers it as one link. The address that named
an organization sends the browser there for one release, because links and
bookmarks outlive a deploy. The callback never named one, so what the provider
has registered stays right. A session the provider opened brings the
provider's own second factor and is held by no rule.

The second-factor rule lives at `/settings/second-factor` and reads off, for
administrators or for everyone. The old address goes without a stand-in: the
web is its one caller and changes with it. A held session may set the factor
up, look at it, end other sessions or sign out, and hears nothing about
another organization.

**Reconsider if** an installation needs more than one identity provider, at
which point the start takes the provider rather than an organization.

### The desk's door is named by its desk

A customer has never heard of an organization's slug, and an installation holds
one organization (#208), so the door lives at `/desk?desk=KEY`. The API follows:
`GET /desk?desk=KEY` says the installation's name and the desk's, and whether
its door is open; `POST /desk/codes` and `POST /desk/sessions` take the desk in
the body. Without a desk the door is the installation's, which is where mail
links lead, since a code is the installation's whichever desk it was asked at.

Nobody is signed in at the door, so the desk's key alone says where it leads. A
project's key is unique in the installation since #218, so it names one desk or
none, and the door's tests use keys of their own.

A code's digest binds the address and the code, no longer the organization, so
codes issued before the upgrade stop working; they live ten minutes. The rows
keep `org_id` until #218. The old `/desk/{orgSlug}` addresses answer 308 for
one release, which sends the same request, body and all, and the web page at
the old address sends the browser on with the desk it named.

**Reconsider if** two desks need one key, which cannot happen while a key is a
project's.

### A forgotten password is reset by mail, and the answer names nobody

**Superseded in part** by [An administrator manages every account, and letting go is switching off](#an-administrator-manages-every-account-and-letting-go-is-switching-off)
(#211): an administrator now resets any account but their own, only an owner
another owner's, and a request or a reset is recorded once rather than in
every organization of the person's. The link by mail stands as written.

Somebody who forgot their password had no way back but an administrator, and
an administrator resets only a person whose one organization is theirs: an
owner, or anybody in two organizations, was locked out. The sign-in page now
takes an address and mails a link that sets a new password.

The answer is the same for every address, known or not, and the mail goes in a
send of its own after the answer, so neither the words nor the time taken say
whether an account exists. A server without mail says so for every address
alike. Asking is braked per caller and per mailbox, so the form cannot be used
to fill somebody's inbox. The secret travels after the #, which no server sees
as part of an address; the database keeps only its digest, one link per
person, replaced by a newer one, and it works once, for an hour.

Using it ends every session and every API token the person holds. Somebody may
ask precisely because another has their password, and whatever was opened with
the old one would otherwise outlive it. It opens no session itself: the person
signs in with what they just chose, which proves they know it, and lands as any
password sign-in does.

An account the identity provider signs in has no password to forget, so it is
mailed the way it does sign in rather than a link. A row without a password
that no provider vouches for, a portal customer or a follower named in a mail,
is mailed nothing: there is no account to recover, and its way in is the
desk's code. Each request and each reset is written, without the secret, to the
log of every organization the person belongs to, as an erasure is.

**Reconsider if** somebody needs back into an account whose mailbox they have
lost, which wants a second proof rather than a mail.

### A password is one proof; an organization may ask for a second

**Superseded in part** by [A sign-in lands in the installation, with one provider and one rule](#a-sign-in-lands-in-the-installation-with-one-provider-and-one-rule)
(#212) and [One installation is one team's tracker](#one-installation-is-one-teams-tracker): the rule is the installation's, a held session switches
nowhere, and a session the provider opened is held by no rule. The factor,
its recovery codes, the sealed key and the code step stand as written.

A password that leaked opened the whole organization, and for an owner every
organization they own. A person can now add an authenticator app: the server
makes a key, shows it once as a QR code and as text, and turns it on only when
the app answers with a code. Ten recovery codes come with it, shown once and
kept as digests salted with the person's id. From then on a password sign-in
opens no session: it answers with a challenge, and the session opens when the
app's code or an unused recovery code is given for it. Whether an account has
a factor is read in the same query as its password, after the password is
checked, so a caller without the password learns nothing they would not learn
from any wrong password.

What the factor applies to is the password. Every password session, wherever
it reaches, is opened through it. An invitation that makes an account and a
signup make a person with no factor yet, and are unaffected. A mailed code and
an open door are the portal's doors and prove an address, not a person;
customers are never asked. A reset by mail ends every session but leaves the
factor on, so a mailbox alone is not a way past it.

An organization may require a second factor of its owners and administrators,
or of every agent. A session without one, on an account without one, is not
refused at the door but held: it may say who it is, set the factor up, switch
to another organization or sign out, and is answered 403
`second_factor_required` for everything else. API tokens are not held, since
only a session can make one. An administrator cannot require what they do not
have themselves, so a requirement never locks out the person setting it.

Single sign-on relies on the provider's factor, but only where the provider is
trusted. A session the provider opened is exempt in the organization it was
opened for and in those that trust the same issuer, which is where that
provider's policy is the organization's policy. Anywhere else it reaches, such
as an organization the person owns, it is held like any other session, because
nobody there chose that provider's rules.

The key is sealed with AES-GCM under `ARMATURE_SECRET_KEY`, bound to the
person's id, with the key's id in the sealed value so a later rotation can keep
the old key around to open what it sealed. Without a key, setting a factor up
answers 503 rather than storing a secret in clear, and once anybody has one the
api refuses to start without the key that sealed it. Each step's code works
once: the step used is kept and the database refuses a step that does not move
forward, as it refuses a used recovery code being made unused. A sign-in's code
step takes five wrong codes, kept in the database, and wrong codes count
against the account's password brake as well, so a new challenge does not buy
new guesses.

An administrator may turn somebody's factor off, for a lost phone and lost
codes, within the reach they have over that person's password, and it is
written to the log. An owner who loses both, with no other owner, has nobody to
ask: that is a known limit, and the way back is the operator removing the row.

**Reconsider if** people ask for security keys or passkeys: WebAuthn is the
later work, and fits beside the authenticator as a second kind of factor.

### A theme is a saved redefinition of the tokens, compiled in the browser

Every colour in the interface is one of forty-three names, and the dark theme
already proved that redefining the names reskins the whole product, charts and
canvas included. A custom theme is therefore a document of overrides on those
names, plus the other things a person can want to change: the two faces, the
two radii, the three shadows, the eight cursors, any of the kit's glyphs, the
backdrop, and a block of CSS for whatever the parts do not reach. Only what
the theme names is kept, so the stylesheet stays the one source of what a
token means, and a theme made today keeps working when a token is added.

The document compiles to one stylesheet in the browser, unlayered so it beats
the stylesheet's own layer, with light overrides under the light selectors and
dark under the dark ones, so the Auto, Light and Dark choice keeps working
under a custom theme and keeps living in the browser, where it belongs to the
device. Which theme a person uses lives on the server, because it is the
person's, not the browser's, and because a theme can be shared: it is its
maker's, shared with the organization when they say so, and an administrator
may tidy what anybody shared. This is the second per-person setting the plan's
horizon was waiting for, and it got a table of its own rather than a
preferences table, because a theme is a thing with files, not a value.

A glyph is replaced by masking the drawn one with a picture in the text's own
colour, so an SVG's colours never matter and the replacement follows every
state the glyph has. Everything a theme loads is one of its own files, served
from this origin as a download with a policy that keeps an SVG inert, or an
inline image; the CSS is refused if it imports, reaches another site, or
closes the element it lives in. The application still has no third party at
load time, whoever wrote the theme.

**Reconsider if** an organization wants a default theme for everyone who has
not chosen, at which point the organization's settings column is where it goes.

### A project is a wall inside the organization

**Superseded in part** by [The tables no longer carry the organization](#the-tables-no-longer-carry-the-organization)
(#218): the database wall it starts from was between organizations. An
installation holds one (#216), the wall went with row security, and the
column it stood on went after it, so the project is the wall inside an
installation. The guard stands as written.

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

**Superseded in part** by [The installation keeps an active administrator](#the-installation-keeps-an-active-administrator)
and [An administrator manages every account, and letting go is switching off](#an-administrator-manages-every-account-and-letting-go-is-switching-off)
(#211): the last active administrator is refused rather than the last owner,
and letting somebody go switches their account off rather than removing the
membership. Erasure itself stands as written.

Erasure replaces the account with a tombstone and keeps the work attributed to
it, because the record belongs to the team: a comment with no author is a worse
record than one by "Former user", and an issue that loses its reporter loses the
one person the desk was answering. What only the person could use goes with
them, and so does what would keep telling them about work, watches included:
an erased person on a watcher list is a name kept for nobody. The last owner of
an organization is refused, since erasing them would
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

### The application is versioned by semver; the API by its own number

A release is a tag `vX.Y.Z` on main, cut by `make release`, and `VERSION` at
the root says which one the tree is. Four files carry it: `VERSION`, the web
package and its lockfile, and the chart's `version` and `appVersion`. A test
refuses them disagreeing, because a chart whose `appVersion` names an image
nobody built installs nothing. A build between releases does not pretend to
be one: past a tag it reads `1.2.3+4.gabc1234`, and with no tag in reach
`VERSION+dev`. The bump is a release branch merged like any other, so main
still never takes a commit of its own.

`info.version` in the OpenAPI document stays `1` and moves only when a client
written against it would break. The application releasing weekly is not news
to a script calling the API, and a contract number that moved with every
release would say nothing.

The API is now published for others to build on: Stator and Exciter call it,
and Dynamo releases the combinations of the three that pass together. So both
numbers are reported side by side at `GET /api/v1/meta`, in the shape all three
tools answer: the product, the build, `apiVersion`, which is `info.version` and
comes from the one constant the document is built with, and the capabilities.
The version was already in the path as `/api/v1`, and stays there; a breaking
change would be `/api/v2` and `apiVersion` 2 together.

`/meta` needs no sign-in, because a partner or Dynamo asks it before it has an
account, if it ever has one. The commit and when it was built are left out
unless the caller is signed in and not held at enrolment, as at `GET /build`
before it: the version says which release answers, and the commit behind it
is nobody else's business. `GET /build` answers as
before behind the session, marked deprecated in the document, and goes once a
Dynamo release has read `/meta` instead.

**Reconsider if** an outside caller needs the commit, such as an operator's
monitoring without an account; a token for it is the way, not an open commit.

The capabilities at `/meta` are names with a version, such as
`remote-links@1`, so a partner asks whether a feature is there instead of
working out from release numbers which build had it. Each names the operations
and topics it promises, and a test refuses one naming a route or a topic the
build no longer has, so the list cannot outlive a feature. A breaking change
raises the number and the old name stays reported for a Dynamo release, which
gives a partner one release in which both work. The names are public, as
`/meta` is: they say what the build can do, not what anybody keeps in it. The
list follows the one Dynamo keeps for all three tools
([`docs/integration.md`](integration.md)).

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
happened, and a digest is the same rows read later. Watching one's own issues
is the one preference read earlier: off, the reporter and the assignee are not
on the issue by their role, so only a watch made on purpose, an assignment or a
mention reaches them. The desk keeps its own notifier for customers, whose mails
are worded for people without an account.

A digest is one mail per person, on their schedule and from their queue. When each one last went is a row in the
database, not memory in the worker: a restart neither resends nor forgets, and
the row is claimed in the transaction that sends, so two workers mail it once.
A daily digest goes at the first pass from 08:00 on that has not sent it that
day, so a pass that misses the hour sends it late instead of skipping a day.

### A webhook is an endpoint, and a rule may aim at one

An endpoint is the object: an address, a secret, the topics it subscribes to,
and a log of every attempt. A subscription is the stream's consumer queuing a
delivery per matching event; a rule's "send a webhook" is the same delivery
queued by a different hand. Signing, retry with backoff, giving up after six
tries and redelivery therefore exist once, and the log an administrator reads is
complete whatever asked for the send. The secret is stored as it is, because
signing needs it; the git integration's inbound secrets stay digests, because
verifying does not.

Deliveries are posted side by side, never one after another: a pool of
`ARMATURE_WEBHOOK_CONCURRENCY` places, at most two of them to any one endpoint,
so an organization whose endpoint hangs holds two places and not the queue.
Three timeouts in a row pause an endpoint: everything it has waiting is put off
five minutes, without counting as an attempt, and the pass goes on without it.
The pause is written on the deliveries rather than kept in memory, so a restart
does not forget it and no migration was needed; a test or a redelivery by hand
still goes at once.

### A webhook's envelope has a version, and an endpoint keeps its own

Receivers are other programs, released on their own schedule; Stator reads
what Armature posts, and Dynamo releases the combinations that pass. So the
envelope carries `version`, an integer, and the `X-Armature-Webhook-Version`
header says the same, so a receiver can refuse a version it does not know
before parsing (#242). The version rises only for a change that breaks a
receiver: a field of the envelope or of a payload removed, renamed or changing
its meaning. Adding a field or a topic does not, because a receiver ignores
what it does not know.

An endpoint records the version it was registered for, the current one unless
it names another, and an edit that names none keeps it. When version N+1
comes, an endpoint on N keeps getting N for at least one Dynamo release; one
builder per version, in a map, is what lets both be sent at once. A delivery
records its own version beside its body, since the body is frozen when it is
queued: a retry or a redelivery sends the header that matches what it carries,
not what the endpoint moved to since. Version 1 is the envelope as it was, with
the number added, so no receiver had to change on this upgrade, and `orgId`
is what version 2 will leave out.

The contract is the document's `webhooks` section, built from the envelope's
Go type like every other schema, and `docs/webhooks.md` says what each topic's
payload holds. The integration suite checks every delivery it receives against
the schema, and the header against the body. Armature only sends this
envelope; what it receives, a git host's delivery or a call on an incoming
hook, is the sender's format.

**Reconsider if** receivers want to choose their payload's shape per topic:
that is a version per topic, which this single number cannot say.

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
happen before anybody is signed in are written by the act itself, and so are a
sign-in with a password and the changes to webhooks, rules and invitations,
which no consumer needs as events. Those entries never hold what the act hands
out: a rotated secret is recorded as rotated, a rule's incoming address by its
kind, an invitation without its link, and a webhook's address as its scheme and
host, because a chat service puts its credential in the path.

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

### The integration suite works in the installation, kept apart by what it makes

Each integration test used to found organizations of its own, which kept tests
apart for free: nothing one test made was on any list another read. An
installation holds one organization (#208), so the suite does what a team does
and works in it (#217). `TestMain` finds the database's organization, or makes
one the way the first sign-up would. A test makes its people at the standing it
needs (`h.person`, `h.outsider`), its projects under keys nobody else has
(`h.projectKey`, `h.newProject`), and anything on a list the whole installation
shares under a name nobody else has (`h.name`). Each is removed when the test
ends, since no organization goes with it any more: the test's own cleanups
first, then a sweep of its projects, its people with the invitations and codes
sent to them, and its names, in that order, so nothing still holds what goes.
The records the product keeps of what happened (the audit log, the outbox and
the mail it read) stay, as they would for anybody's work. The worker copies a
test's acts into the audit log after the test removed the actor, so the log
records such an act without them, as it does when an actor goes later. The demo
answers to keys an installation has once, so its test gets a database of its own.

Being apart is no longer the same as seeing nothing of each other. An
administrator sees every test's projects and people, so a test asserts on what
it made rather than on totals. A test that changes what every test lives with,
such as the second-factor rule, a default, the roles, the provider or a field on
every project, runs alone before the parallel ones and puts back what it found,
since the database it runs against is somebody's development installation.
Tests that proved only that organizations cannot see each other were deleted;
where they also proved a project wall, the wall is tested between projects.
Separate databases per test would have kept the old freedom, at a second or more
each, and would not have tested one installation the way it runs.

**Reconsider if** the sequential list grows long enough to dominate the run:
then the shared settings want a scope a test can own, not more tests alone.

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
The test went with row security (#218); the grants still name the roles.

Two things the four layers could not see, both found by hand. The Keycloak
realm's client secret was renamed without the matching constant in the seed,
which would have broken the demo's single sign-on: no layer exercises OIDC.
And the documents are prose, so a stale metric name or bucket default in the
README is invisible to every test. The gate for the whole job was therefore
`git grep -i nojira` returning nothing, which is also what caught the Redis
stream key, spelled with a colon where everything else used an underscore.
