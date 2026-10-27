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
  "capabilities": ["calendar@1", "git-proxy@1", "issue-embed@1", "mcp@1", "oauth.partner-tokens@1", "oauth.token-exchange@1", "plan@1", "remote-link-relations@1", "remote-links@1", "reports@1", "theme-export@1", "webhooks.partner@1", "webhooks@1", "webhooks@2"]
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

### `webhooks@2`

An endpoint registered for envelope version 2 is posted each event as a
CloudEvents 1.0 event in structured mode, the version 1 payload as its data,
signed as version 1 is.

`POST /webhooks` and `PATCH /webhooks/{endpointID}` take `"version": 2`, as
does a connected app's own endpoint. The body is
`application/cloudevents+json; charset=utf-8`, with `specversion` `1.0`,
`source` the installation's address, `type` `dev.armature.` and the topic,
`dataversion` `1`, and `data` the version 1 payload;
[`docs/webhooks.md`](webhooks.md#version-2-a-cloudevent) has every field. The
headers and signatures are those of version 1, and `X-Armature-Webhook-Version`
says `2`.

- `POST /webhooks`
- `PATCH /webhooks/{endpointID}`
- `POST /webhooks/{endpointID}/test`

### `mcp@1`

A partner works with issues over the Model Context Protocol, version
2025-06-18, as the person whose token it holds: an API token, or a token of
`oauth.partner-tokens@1` within its scopes. The tools are the marked
operations of this API, discovered from `tools/list`, so only the endpoint is
promised; a tool that only reads says so with `readOnlyHint`.

- `POST /mcp`

A partner that writes code for an issue, as Rotor does, works in the
repository the issue's project connects, through four tools it may name. The
host is only ever called with the repository's own access token, and a write
is the person's: it needs `issue.write` on the issue, from the person's role
and from the app's grant.

| Tool | Arguments | Answers |
|---|---|---|
| `list_repositories` (reads) | `projectKey` | `repositories`: each one's `id`, `host`, `name` (owner/name), `url`, `defaultBranch` and `hasToken`, whether Armature can write to it |
| `get_issue_development` (reads) | `issueKey` | the issue's `branches`, `pullRequests`, `commits` and CI `runs`, and `branchName`, the name a branch made for it gets |
| `create_issue_branch` | `issueKey`, `repository`, `name` and `from` optional | `branch`: the branch the host made, off `from` or the default branch, named `name` or `branchName` |
| `open_pull_request` | `issueKey`, `repository`, `branch`, `title`, `body` and `draft` optional | `pullRequest`: its `number`, `url`, `state`, `sourceBranch` and `targetBranch`, recorded on the issue |

- `repository` is the owner/name `list_repositories` gives; `repositoryId`
  names it by id instead. Either way it is a repository of the issue's
  project, or it is not found.
- A pull request goes from a branch made for this issue, by
  `create_issue_branch` or by a push naming the issue, into the default
  branch. The default branch is never an issue's branch, which the database
  holds too, and a branch made for nothing or for another issue takes no pull
  request. `draft` opens it as a draft: GitHub's draft, GitLab's `Draft:`,
  Gitea's `WIP:`.
- A repository without an access token answers "This repository has no
  access token, so Armature cannot write to it." with code `no_token`, and the
  host refusing answers 502 `host_refused`. These calls are
  `POST /issues/{issueKey}/branches` and `POST /issues/{issueKey}/pull-requests`
  over HTTP.

### `git-proxy@1`

A partner clones a project's repository and pushes an issue's branch through
Armature, as the person whose token it holds, and the host's own token never
leaves Armature. Armature speaks git's smart HTTP and forwards each request to
the host's clone address with the repository's access token.

- `GET /git/{projectKey}/{owner}/{name}/info/refs?service=git-upload-pack` or
  `?service=git-receive-pack`
- `POST /git/{projectKey}/{owner}/{name}/git-upload-pack`
- `POST /git/{projectKey}/{owner}/{name}/git-receive-pack`

The clone address is `{issuer}/api/v1/git/{projectKey}/{owner}/{name}.git`,
where `owner/name` is the repository's name as `list_repositories` gives it:
`https://armature.example.com/api/v1/git/CP/acme/portal.git`. A GitLab owner
with subgroups is one path segment, its slashes written `%2F`.

- **The token.** An API token or a connected app's access token, sent as
  `Authorization: Bearer <token>` (`git -c http.extraHeader="Authorization:
  Bearer <token>" clone ...`) or as basic auth, the token being the password
  and the user name anything (`https://rotor:<token>@.../CP/acme/portal.git`).
  A browser's session cookie never clones or pushes. Without a credential the
  proxy answers 401 with `WWW-Authenticate: Basic`, so git asks its
  credential helper.
- **A clone or a fetch** needs `read` and the right to see the project. A
  project the person cannot see, or a repository it does not connect, is "not
  found".
- **A push** needs, besides, `git.write` from a connected app (a personal
  key may push unless it is read-only), and the person's `issue.write` in the
  project. Each command is then checked before anything reaches the host, and
  the whole push is refused when one is:
  - only `refs/heads/<branch>` of a branch made for an issue, by
    `create_issue_branch` or a push naming the issue, that the person may
    edit;
  - never the default branch, a tag, a deletion or a signed push;
  - never a force push: every commit a branch moves to must descend from
    where it was, through the commits the push carries. A push that only
    points a branch at a commit the host already has is refused for the
    same reason.
- **Refusals** are sentences git prints. One before the push, such as a
  missing scope, a reader or a missing repository, is a 401, 403, 404 or 409
  in `text/plain`, which git shows after `remote:`. A refused ref is reported
  as receive-pack does, `! [remote rejected] main -> main (main is the
  default branch, ...)`, with the reason on `remote:` as well.
- **Limits.** A push is at most 32 MiB and its objects at most 512 MiB
  unpacked. A token makes at most 30 pushes and starts at most 60 clones or
  fetches a minute; past either it hears 429. A clone or a push may stream for
  ten minutes.
- **The record.** Every push, forwarded or refused, is in the audit log as
  `git.push` against the repository: the person, the app, each ref with its
  old and new commit and its issue, why it was refused, and the host's status.
- **The host.** A repository without an access token answers 409, and the
  host refusing Armature's token, or not having the repository, answers 502;
  both say who fixes it. GitHub is reached as `x-access-token`, GitLab and
  Gitea as `oauth2`, each with the token as the password.

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

### `oauth.token-exchange@1`

A connected app trades a person's access token from the installation's identity
provider for a grant, with no consent page, so a partner connects the person at
their sign-in. `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
`subject_token` an access token of Armature's OIDC provider whose `azp` is the
app's `keycloakClientId` and whose `aud` holds Armature's own client, and the
app authenticating with its own credentials. The answer is an access and a
refresh token within the person's rights, never `webhooks.partner`.
[Connected apps](oauth.md#token-exchange-rfc-8693) says how.

- `POST /oauth/token`, which lists the grant in the server metadata

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
  `events.Topics` does not list, or a grant the token endpoint does not serve,
  so renaming or deleting a route fails before
  the build reports a feature it lost. Another refuses a name that is not
  `name@version` or is listed twice.

Armature calls no partner today, so it checks no partner's capabilities. A
feature that comes to rely on Stator or Exciter reads their `/meta` first and,
when a name it needs is missing, says so on its settings page and in the
feature, naming the partner and what to update.
