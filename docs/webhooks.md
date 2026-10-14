# Webhooks

An administrator adds an endpoint under Settings, Webhooks (`/settings/webhooks`),
or with `POST /api/v1/webhooks`: a name, an address, and the topics it takes,
or `*` for every one. Armature then posts one JSON request to the address for
each event on those topics, signed with the endpoint's secret, which is shown
once. A rule's "send a webhook" action posts to an endpoint the same way, and
its deliveries land in the same log.

This page is the contract a receiver is written against. The machine-readable
half is the `webhooks` section of [`api/openapi.json`](../api/openapi.json),
whose `event` operation describes the body and the headers; the integration
suite checks every delivery it sends against it.

## The envelope

Every delivery is one envelope, the same for every topic:

```json
{
  "version": 1,
  "id": "0199e1c2-7d4a-7c11-9b1e-3f0a2b4c5d6e",
  "topic": "issue.created",
  "orgId": "0199a000-0000-7000-8000-000000000001",
  "occurredAt": "2026-10-14T09:30:00Z",
  "payload": { "issueId": "...", "key": "CP-12", "summary": "Fix the login", "actorId": "..." }
}
```

| Field | Type | What it is |
| --- | --- | --- |
| `version` | integer | The envelope version, the one the endpoint was registered for. The `X-Armature-Webhook-Version` header says the same. |
| `id` | uuid | The event's id. A retry and a redelivery carry the same id, so a receiver can drop one it has already handled. |
| `topic` | string | What happened, from the list under [Topics](#topics). |
| `orgId` | uuid | Deprecated. The installation's stable id, the same in every delivery. Version 2 leaves it out; read nothing from it. |
| `occurredAt` | date-time | When the delivery was queued, in UTC. |
| `payload` | object | The event's own fields, by topic. |

## Headers and signatures

| Header | What it carries |
| --- | --- |
| `Content-Type` | `application/json` |
| `User-Agent` | `Armature-Webhook` |
| `X-Armature-Event` | The topic, as in the body. |
| `X-Armature-Delivery` | This attempt's id. A retry and a redelivery each have their own. |
| `X-Armature-Webhook-Version` | The envelope version of the body, as in its `version` field. |
| `X-Armature-Signature-256` | `sha256=` and the hex HMAC-SHA256 of the raw body, keyed with the endpoint's secret: the shape GitHub uses. |
| `X-Armature-Timestamp` | When this attempt was sent, in Unix seconds. |
| `X-Armature-Signature-Timestamped-256` | `sha256=` and the hex HMAC-SHA256 of the timestamp, a dot and the raw body. |

Verify a signature over the bytes received, before parsing them. The
timestamped signature also proves when the attempt was sent, so a receiver
that compares the timestamp with its own clock can refuse a replay.

Any 2xx answer marks the delivery done. Anything else, or no answer within ten
seconds, fails the attempt, and the next is tried after 1 minute, 5 minutes, 30
minutes, 2 hours and 12 hours: six attempts in all. Three timeouts in a row put
off an endpoint's waiting deliveries for five minutes. Every attempt is in the
endpoint's log, from where an administrator can redeliver one by hand; **Test**
posts a `ping` now.

## Versions

The envelope carries a version so its format can change without breaking a
receiver silently.

- `version` is an integer, in the body and in the `X-Armature-Webhook-Version`
  header, so a receiver can decide before it parses the body.
- It rises only for a change that breaks a receiver: a field of the envelope
  or of a payload removed, renamed or changing its meaning. Adding a field or a
  topic does not raise it.
- An endpoint is registered for a version. `POST /api/v1/webhooks` takes
  `version`; left out, it is the current one. `PATCH` keeps the endpoint's
  version unless it names another. A version Armature does not send is refused,
  with the versions it does.
- When version N+1 arrives, an endpoint registered for N keeps being sent N for
  at least one [Dynamo](https://github.com/praetorianer777/Dynamo) release, and
  moves when its administrator moves it.
- A delivery is built when it is queued and keeps that version: its retries and
  redeliveries carry the same body and the same header, whatever the endpoint
  is on by then.

A receiver ignores fields and topics it does not know, and refuses an envelope
version it does not know with a clear status, such as 422, and a log line,
instead of misreading it. Armature retries a refused delivery like any other
failure and keeps the status in the endpoint's log.

| Version | Since | What changed |
| --- | --- | --- |
| 1 | the release that numbered it (#242) | The envelope as it was, with `version` added. `orgId` is deprecated. |

Version 2 will leave out `orgId`. Nothing else is planned for it.

## Topics

An endpoint takes the topics it names, or every topic with `*`. Some topics are
sent only to `*`, as the list says. The automation's own topics never reach a
subscription, since an endpoint posting into an incoming hook would feed
itself; a rule's "send a webhook" posts the event that triggered it, whichever
that is.

Ids are uuids, written as strings. `key` is an issue key such as `CP-12`, the
current one. `actorId` is the account that acted, the automation account when
a rule did. A `changes` entry is `{ "field": ..., "from": ..., "to": ... }`,
with the values as a person reads them, and either side left out when it was
or became empty.

### ping

What **Test** posts.

- `message` (string): `Armature can reach this address.`

### member.joined

Somebody became a member.

- `userId` (uuid): who joined.
- `role` (string): `owner`, `admin`, `member` or `customer`.
- `orgId` (uuid): deprecated, the installation's id; it goes in a later release.

### issue.created

- `issueId` (uuid), `key` (string), `summary` (string), `actorId` (uuid).
- `imported` (boolean): `true` on an issue a CSV import made; absent otherwise.

### issue.updated

One event per edit, with every field it changed.

- `issueId` (uuid), `key` (string), `actorId` (uuid).
- `changes` (array of changes): what changed. A link added or removed is a
  change of `link`, its other side as `to` or `from`.
- `movedFrom` (string): the issue's key before it was moved to another
  project; only on a move, where `key` is the new key.
- A link removed carries no `actorId`.

### issue.transitioned

An issue moved from one status to another, through a workflow transition or
because a type change put it in the new workflow's status.

| Field | What it holds |
| --- | --- |
| `issueId` | the issue's id |
| `key` | the issue's key, such as `NOJ-7` |
| `transition` | the name of the transition taken, or `Type change` when a type change moved the issue |
| `fromStatus` | the name of the status the issue left |
| `toStatus` | the name of the status the issue is in now |
| `fromStatusCategory` | the category of the status it left: `todo`, `in_progress` or `done` |
| `toStatusCategory` | the category of the status it is in now: `todo`, `in_progress` or `done` |
| `actorId` | the id of the person who moved it |

The categories let a subscriber keep an issue's category without reading the
issue again.

### issue.deleted

The issue is gone for good.

- `issueId` (uuid), `key` (string), `actorId` (uuid).

### comment.added

- `issueId` (uuid), `key` (string), `commentId` (uuid), `actorId` (uuid).
- `internal` (boolean): an internal note, not shown to a customer.
- `mentions` (array of uuids, or null): the people named in it, who were made
  watchers.

### vcs.pull_request.linked

A pull request names an issue; one event per issue it newly names.

- `issueKey` (string), `repositoryId` (uuid), `number` (integer).

### vcs.commit.linked

A commit names an issue; one event per issue it newly names.

- `issueKey` (string), `repositoryId` (uuid), `sha` (string).

### ci.run.recorded

A repository's host reported a CI run.

- `repositoryId` (uuid), `externalId` (string), `sha` (string).
- `status` (string): `pending`, `success`, `failure` or `cancelled`.

### ci.deployment.succeeded

Offered for subscription; nothing sends it yet.

### sla.breached

A service desk request ran past a target.

- `issueKey` (string), `timerId` (uuid).
- `metric` (string): `first_response` or `resolution`.

### attachment.added

- `issueId` (uuid), `key` (string), `attachmentId` (uuid), `fileName` (string),
  `actorId` (uuid).

### worklog.added

- `issueId` (uuid), `key` (string), `worklogId` (uuid), `actorId` (uuid).
- `minutes` (integer): the time logged.

### watcher.added

- `issueId` (uuid), `key` (string), `actorId` (uuid).
- `userId` (uuid): who now watches.

### issue.remote_link.added

A page or a requirement elsewhere was put on an issue.

- `issueId` (uuid), `key` (string), `linkId` (uuid), `actorId` (uuid).
- `url`, `title` (string): the page.
- `source` (string): the application that put it there.
- `relation` (string): `page` or `requirement`, what the link is to the issue.

### issue.remote_link.removed

A page or a requirement elsewhere was taken off an issue. The same fields as
`issue.remote_link.added`, as the link was.

### request.stays_done

Only to `*`. A customer replied to a finished request its workflow has no way
out of, so somebody at the desk has to move it by hand.

- `key` (string), `commentId` (uuid), `actorId` (uuid): the customer.
- `status` (string): the status it stays in.

### absence.recorded, absence.changed, absence.removed

Only to `*`. Somebody's days away were written down, moved or taken back. Which
days is never said.

- `id` (uuid): the absence.
- `userId` (uuid): whose absence it is.
- `actorId` (uuid): who recorded, changed or removed it.

### automation.scheduled, automation.incoming, automation.manual

Never to a subscription, only from a rule's "send a webhook" on the trigger.

- `automation.scheduled`: `ruleId` (uuid), `at` (date-time): the schedule
  came due.
- `automation.incoming`: `ruleId` (uuid), `key` (string, empty when the call
  named no issue), `body` (any JSON): what the incoming hook was posted.
- `automation.manual`: `ruleId` (uuid), `key` (string), `actorId` (uuid):
  somebody ran the rule by hand.

### Everything else

An endpoint on `*` also gets the administrative events the audit log is made
from, such as `project.*`, `sprint.*`, `workflow.*`, `role.*` and
`csat.rated`. Their payloads are not listed here yet, and the same rule holds
for them: a field is added freely, and removed or renamed only with a new
version.
