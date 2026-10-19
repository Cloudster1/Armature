# Integration capabilities

`GET /api/v1/meta` lists, under `capabilities`, the integration features this
build offers, as stable names with a version, such as `remote-links@1`. A
partner decides from these names whether a feature is there, not from the
version number of the build: Stator offers issue embeds only when Armature
lists `issue-embed@1`. `/meta` answers anyone, so a partner can ask before it
has an account; the names promise features, not data.

```json
{
  "product": "armature",
  "build": { "version": "1.3.0" },
  "apiVersion": "1",
  "capabilities": ["calendar@1", "issue-embed@1", "oauth.partner-tokens@1", "plan@1", "remote-link-relations@1", "remote-links@1", "reports@1", "theme-export@1", "webhooks.partner@1", "webhooks@1"]
}
```

The list is sorted. Paths below are under `/api/v1`, spelled as in
[`api/openapi.json`](../api/openapi.json). Every operation takes a session or
an API token unless the document marks it public.

## The capabilities

### `issue-embed@1`

An issue read by its key, and issues found by NQL or by the words typed so far,
with a session or a token.

- `GET /issues/{issueKey}`
- `GET /issues`
- `GET /issues/suggest`

### `remote-links@1`

The pages elsewhere that are about an issue: listed, put on by url, retitled by
putting the same url again, taken off, and announced as topics.

- `GET /issues/{issueKey}/remote-links`
- `POST /issues/{issueKey}/remote-links`
- `DELETE /issues/{issueKey}/remote-links/{remoteLinkID}`
- topics `issue.remote_link.added` and `issue.remote_link.removed`

### `remote-link-relations@1`

A remote link says whether it is a `page` or a `requirement`: `relation` on the
link, on the request that puts it and in both topics' payloads. One sent
without it is a page, and a resend that leaves it out keeps the one the link
has. A partner sends `relation` only to a build that lists this name.

- `GET /issues/{issueKey}/remote-links`
- `POST /issues/{issueKey}/remote-links`
- topics `issue.remote_link.added` and `issue.remote_link.removed`

### `webhooks@1`

Endpoints subscribed to topics, their deliveries, a redelivery, a test delivery
and a new secret, each delivery a signed envelope.

The envelope is a JSON object with `version`, `id`, `topic`, `occurredAt` and
`payload`, and `X-Armature-Webhook-Version` repeats its version; an endpoint
keeps the version it was registered for. The envelope, its versions and the
payloads are in [`docs/webhooks.md`](webhooks.md). It is signed with the
endpoint's secret as `sha256=` and the hex HMAC-SHA256 of the body in
`X-Armature-Signature-256`, and of `{timestamp}.{body}` in
`X-Armature-Signature-Timestamped-256`, the Unix seconds in
`X-Armature-Timestamp`.

- `GET /webhooks`
- `POST /webhooks`
- `PATCH /webhooks/{endpointID}`
- `DELETE /webhooks/{endpointID}`
- `GET /webhooks/{endpointID}/deliveries`
- `POST /webhooks/{endpointID}/deliveries/{deliveryID}/redeliver`
- `POST /webhooks/{endpointID}/test`
- `POST /webhooks/{endpointID}/rotate-secret`

### `oauth.partner-tokens@1`

A connected app gets a person's token with their consent, by authorization code
with PKCE, refreshes it, revokes it, and registers itself pending an
administrator. [Connected apps](oauth.md) is the whole contract.

- `POST /oauth/register`
- `POST /oauth/token`
- `POST /oauth/revoke`
- `GET /oauth/consent`
- `POST /oauth/consent`
- `GET /.well-known/oauth-authorization-server`, at the root rather than under `/api/v1`
- `GET /.well-known/oauth-protected-resource`, likewise

### `webhooks.partner@1`

A connected app an administrator allowed makes, changes, tests, rotates and
removes its own webhook endpoint; listed beside `oauth.partner-tokens@1` only.
[Connected apps](oauth.md#the-partners-own-webhook) says how.

- `GET /webhooks/partner`
- `POST /webhooks/partner`
- `PATCH /webhooks/partner`
- `DELETE /webhooks/partner`
- `POST /webhooks/partner/rotate-secret`
- `POST /webhooks/partner/test`

### `theme-export@1`

A theme exported as one file with its pictures and fonts, and such a file
imported as a theme of the caller's own.

- `GET /themes/{themeID}/export`
- `POST /themes/import`

### `reports@1`

The report kinds a project has, one report computed now, and a dashboard shared
by a link read without an account.

- `GET /projects/{projectKey}/report-kinds`
- `GET /projects/{projectKey}/reports/{kind}`
- `GET /shared/{token}`
- `GET /shared/{token}/widgets/{widgetID}`

### `plan@1`

A project laid out against a calendar, its people's hours against the work, and
the share of each person's week it has, read and set.

- `GET /projects/{projectKey}/plan`
- `GET /projects/{projectKey}/resources`
- `GET /projects/{projectKey}/allocations`
- `PUT /projects/{projectKey}/allocations/{userID}`

### `calendar@1`

Everything a project has dated in a month: issues, sprints, milestones,
versions, holidays and who is away.

- `GET /projects/{projectKey}/calendar`

## The rules

- **A name is promised for what it lists.** The operations keep their paths,
  their parameters and the fields they answer with; a field may be added, none
  is taken away or changes meaning. A topic keeps its name and its payload's
  fields the same way.
- **A breaking change raises the number.** Renaming or removing one of the
  operations or topics above, a field a partner reads, or a meaning, makes the
  capability `remote-links@2`. The old name is still reported beside the new
  one, and still kept, for at least one Dynamo release, so a partner has a
  release in which both work.
- **The list is shared.** The names and what each promises are kept in one
  list in the Dynamo repository, `docs/compatibility.md` of
  [praetorianer777/Dynamo](https://github.com/praetorianer777/Dynamo) (Dynamo#4,
  not written yet). A tool may only report names from it; this page follows it
  once it is there, and a new name is added there first.
- **The build is held to it.** The list lives in
  `backend/internal/httpapi/capabilities.go`. A unit test refuses a capability
  naming an operation the operations table does not have or a topic
  `events.Topics` does not list, so renaming or deleting a route fails before
  the build reports a feature it lost. Another refuses a name that is not
  `name@version` or is listed twice.

Armature calls no partner today, so it checks no partner's capabilities. A
feature that comes to rely on Stator or Exciter reads their `/meta` first and,
when a name it needs is missing, says so on its settings page and in the
feature, naming the partner and what to update.
