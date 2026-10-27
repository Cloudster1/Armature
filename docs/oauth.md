# Connected apps

A partner tool such as Stator, Exciter or Rotor calls Armature as a person. It
gets that person's token with their consent, in one click, instead of the person
making a personal access token and pasting it across. Armature is the OAuth 2.0
authorization server; the partner is a connected app.

This page is the contract partners build against (#280, #281). The route table and
[`api/openapi.json`](../api/openapi.json) are the source of truth for paths,
fields and types; this page says what they mean.

## At a glance

- The authorization code grant only, with PKCE `S256` required of every client.
  No implicit grant, no password grant, no client credentials, no `plain`.
- The issuer is `ARMATURE_APP_URL`. A partner reads every address from the
  metadata at `{issuer}/.well-known/oauth-authorization-server` and builds none
  itself.
- A scope is a permission name. A token never exceeds its person's live
  rights: the scopes narrow the person's permissions where a key is narrowed.
- One grant per person and app. It is one row in the person's token list,
  "Stator (connected app)", and ends there, at the revocation endpoint, or with
  the app.
- Access tokens live an hour. Refresh tokens rotate on every use and live 90
  days from their issue; presenting a rotated one ends the grant.
- An app is registered by an administrator, or registers itself and stays
  pending until an administrator allows it. Nothing a pending app asks for
  works before that.
- An app an administrator allowed may own one webhook endpoint, which it
  manages itself with a token holding `webhooks.partner`.
- The capabilities `oauth.partner-tokens@1`, `oauth.token-exchange@1` and
  `webhooks.partner@1` in `/api/v1/meta`.
- The OAuth endpoints answer OAuth's error shape, `{"error", "error_description"}`
  (RFC 6749 section 5.2, RFC 7591 section 3.2.2), and take OAuth's parameter
  names. Everything else answers Armature's envelope.

## Settings

| Setting | Meaning |
|---|---|
| `ARMATURE_APP_URL` | The issuer, and the base of every address in the metadata. |
| `ARMATURE_OAUTH_REGISTRATION` | `approve` (default): anybody may register an app, pending until an administrator allows it. `closed`: administrators register apps under Settings, Connected apps, and `/oauth/register` answers 403. |
| chart `oauth.registration` | The same, in the chart. |

nginx, the chart's nginx and its Ingress send the two `.well-known` documents
to the api. `/oauth/authorize` is a page of the web app.

## Scopes

| Scope | The consent page says | Lets the token |
|---|---|---|
| `read` | See the projects and issues you can see | Every read the person may make. Required with every other scope. |
| `issue.write` | File and edit issues, and link pages to them | Create and edit issues, their fields, labels, links, remote links, worklogs, attachments, branches and pull requests |
| `issue.transition` | Move issues through their workflow | Transitions, and moving a card on a board |
| `comment.write` | Comment on issues | Comments, internal notes and watchers |
| `git.write` | Push code to the branches of issues you may edit | Pushes through the [git proxy](integration.md#git-proxy1) to a branch made for an issue the person may edit; never the default branch, a tag, a deletion or a force push |
| `webhooks.partner` | Send Armature's events to {app}, from every project (administrators only) | The app's own webhook, below |

- A connected app may make the writes the table names and no other. Any other
  write, such as a profile, a theme, a saved filter or anything an
  administrator configures, is refused before any handler with 403
  `insufficient_scope`: "A connected app cannot change your own settings or
  Armature's. Do it in Armature." The MCP endpoint and Ask pass, and the calls
  they carry are judged again.
- A write whose scope the grant lacks is 403 `insufficient_scope`, with a
  sentence naming what to allow and `WWW-Authenticate: Bearer
  error="insufficient_scope", scope="issue.write"` (RFC 6750). A grant of
  `read` alone hears "Stator was allowed to read only. Connect it again and
  allow it to change issues."
- Within its scopes, the token is the person: a project they cannot see is not
  there, and a write their role does not allow is 403 `forbidden`, as for
  their own session. A person losing a right narrows their apps at once.
- `git.write` is no permission of its own: a push takes it from the app and
  `issue.write` from the person's role, whether or not the app also holds
  `issue.write`. Without it a push hears "Rotor was not allowed to push to
  issue branches. Connect it again and allow it to push to issue branches."
- Personal access tokens are unchanged: `POST /tokens` takes `read` or nothing.

## Apps

| Field | Meaning |
|---|---|
| `id` | The `client_id`, a uuid. |
| `name` | Shown on the consent page and in the token list. 1 to 60 characters. |
| `clientUri` | The partner's own address, shown under the name. Optional. |
| `redirectUris` | 1 to 5, compared byte for byte. `https`, or `http` for `localhost`, `127.0.0.1` and `[::1]`, or any `http` when `ARMATURE_ENV=development`. No fragment. |
| `scopes` | What the app may ever ask for; `read` among them. |
| `confidential` | True: it authenticates at the token endpoint with a secret. False: a public app, PKCE alone. |
| `status` | `active`, or `pending` until an administrator allows it. |
| `keycloakClientId` | The identity provider's client whose tokens may be exchanged for this app's grants. Empty: no exchange. |
| `webhook` | Its own webhook, `{id, url, topics, enabled}`, or null. |
| `grants` | How many people have connected it. |

A client secret is `armature_ocs_` and 43 base64url characters, stored as a
digest and shown once, on creation and on rotation.

### Registered by an administrator

`org.administer`, from a session. Under Settings, Connected apps.

| Operation | Answers |
|---|---|
| `GET /oauth/clients` | `{clients}`, by name, pending ones included |
| `POST /oauth/clients` | 201 `{client}` with `secret` when confidential; 422 on `name`, `clientUri`, `redirectUris`, `scopes` |
| `PATCH /oauth/clients/{clientID}` | `{client}`; what is left out keeps its value. Narrowing `scopes` narrows every grant of the app at once. |
| `POST /oauth/clients/{clientID}/approve` | `{client}`; a pending app becomes active, as allowing it on the consent page does |
| `POST /oauth/clients/{clientID}/rotate-secret` | `{client}` with the new `secret`; the old one stops at once. 409 for a public app. |
| `DELETE /oauth/clients/{clientID}` | 204; every grant of it ends, and its webhook goes with its log |

### The tools people switch to

The avatar menu's "Switch to" group links the suite's other tools, each to the
`clientUri` of the app that stands for it (#327). An active app with a
`clientUri` stands for Stator when its `keycloakClientId` is `stator` or its
name is "Stator", and for Exciter alike; when several stand for one tool, the
one changed last wins. Removing the app, or emptying its address, takes the
tool out of the menu.

`GET /installation/tools` answers `{tools}`, each `{product, name, url}`,
Stator before Exciter, to everyone signed in except customers. The other tool
decides what they may see there.

### Registered by the partner itself

`POST /api/v1/oauth/register` is RFC 7591 dynamic registration, public, braked
per caller address. It takes RFC 7591's names, and reads past metadata it does
not use, such as a logo:

```json
{
  "client_name": "Stator",
  "client_uri": "https://stator.example.com",
  "redirect_uris": ["https://stator.example.com/api/v1/armature/oauth/callback"],
  "scope": "read issue.write webhooks.partner",
  "token_endpoint_auth_method": "client_secret_basic",
  "grant_types": ["authorization_code", "refresh_token"],
  "response_types": ["code"]
}
```

201 answers the same fields with `client_id`, `client_id_issued_at`,
`client_secret_expires_at: 0` and, unless `token_endpoint_auth_method` is
`none`, `client_secret`. Left out, `scope` is `read`.

- The app is pending. The only thing it can do is send an administrator to the
  consent page, which allows it there with the scopes it registered. One nobody
  allowed within 24 hours is deleted.
- There is no `registration_access_token`: a partner cannot read or change its
  registration afterwards. It registers again, or an administrator edits it.
- 400 `invalid_redirect_uri` or `invalid_client_metadata` with a sentence, and
  403 `access_denied` where registration is `closed`: "This Armature registers
  connected apps only by hand. Ask an Armature administrator to add Stator
  under Settings, Connected apps."

## The consent page

The partner sends the browser to the `authorization_endpoint`:

```
GET {issuer}/oauth/authorize?response_type=code
  &client_id=0199e1c2-...
  &redirect_uri=https%3A%2F%2Fstator.example.com%2Fapi%2Fv1%2Farmature%2Foauth%2Fcallback
  &scope=read%20issue.write
  &state=<at least 128 bits, the partner's own>
  &code_challenge=<base64url SHA-256 of the verifier>
  &code_challenge_method=S256
  [&resource={issuer}/api/v1]
  [&prompt=consent]
```

The page asks the api, which decides everything:

| Operation | Needs | Answers |
|---|---|---|
| `GET /oauth/consent?{the same query}` | session | `{consent}`: the app's `name` and `clientUri`, `redirectHost`, `pending`, the person, the scopes in words, `mayAllow`, `mayCancel` and a `refusal` sentence; or `{redirectTo}` when nothing needs asking |
| `POST /oauth/consent` | session | `{query, decision: "allow" or "deny"}`, answers `{redirectTo}` |

The rules, in order:

1. **Not signed in.** 401, and the page sends the browser to the sign-in page,
   single sign-on included, and back. A session held for two-step sign-in sets
   it up first.
2. **An unknown app, or a return address it did not register.** 400
   `not_connected`: "This app is not connected to Armature. Ask an Armature
   administrator to connect it." The browser is never sent to the address
   (RFC 6749 section 4.1.2.1).
3. **A desk's customer** is told "A connected app acts for an Armature
   account. Sign in with yours." and may only cancel.
4. **Anything else wrong** is answered at the return address with `error`,
   `error_description`, `state` and `iss`: `unsupported_response_type`;
   `invalid_request` for a missing `state` or `code_challenge`, a method other
   than `S256` ("Armature requires PKCE with S256.") or a parameter sent
   twice; `invalid_scope` for a scope the app may not ask for, or none of them
   `read`; `invalid_target` for a `resource` other than `{issuer}/api/v1` or
   `{issuer}/api/v1/mcp`.
5. **A pending app.** Only an administrator may allow it; the page says
   "Stator at stator.example.com asks to be connected to Armature. Allowing it
   lets it ask anybody here to connect their account, with at most these
   rights." above the administrator's own consent. A member reads "Stator is
   not connected to Armature yet. An Armature administrator connects it
   first." and is not redirected.
6. **`webhooks.partner` asked of somebody who does not administer Armature.**
   "Sending Armature's events to Stator needs an Armature administrator. Sign
   in as one, or ask one to connect Stator." with only Cancel.
7. **Allowed already.** A person whose grant for the app covers the scopes
   asked is sent back with a new code at once, unless `prompt=consent` is sent
   or `webhooks.partner` is asked. Redeeming it replaces the grant's tokens.
8. **The page.** "Stator wants to act as Ada Lovelace in Armature:" and the
   scopes in words, the return host, Allow and Cancel. Cancel answers
   `error=access_denied`. Allow answers `code`, `state` and `iss={issuer}`
   (RFC 9207).

The audit log records `oauth.client.approved` and `oauth.grant.created`.

## Tokens

| Token | Prefix | Lives |
|---|---|---|
| authorization code | `armature_oac_` | 60 seconds, once. A second use ends every token issued from it (RFC 6749 section 10.5). |
| access token | `armature_oat_` | 1 hour. A bearer for every `/api/v1` operation a key may call, `/mcp` included. |
| refresh token | `armature_ort_` | 90 days from its issue, rotated on every use. |

Every secret is 32 random bytes in base64url after its prefix, stored as a
SHA-256 digest only.

- **One grant per person and app.** A new consent or exchange by the same
  person ends the grant's previous tokens and adds the scopes asked to the ones
  it holds, so signing in again or asking for less never drops
  `webhooks.partner`. A narrower `scope` on refresh is how a grant narrows; a
  grant that has ended keeps nothing.
- **Refresh rotates.** Each refresh answers a new access and refresh token and
  ends both previous ones. A rotated refresh token presented again ends the
  grant (RFC 9700 section 4.14.2): a partner refreshes one grant in one place
  at a time, under a lock of its own. There is no grace window.
- **The grant lives while it is used.** Each refresh gives another 90 days; a
  grant nobody refreshed for 90 days has ended.
- **What ends a grant**, as it ends a key: Disconnect in the token list,
  revocation, removing the app, the person being switched off, a password
  reset by mail, erasure, and a rotated refresh token presented again.
  Narrowing the app's scopes narrows the grant without ending it.
- **Who the token is.** `GET /auth/me` answers the person and `token`, whose
  `app` names the app and whose `scopes` are what it holds.
- **A dead token** answers 401 `unauthorized` with `WWW-Authenticate: Bearer
  error="invalid_token"`. A partner refreshes once; a refresh answered
  `invalid_grant` means the person is disconnected.

In `GET /tokens` a grant is a token with `app` (`clientId`, `name`,
`clientUri`), named after the app and " (connected app)", with `expiresAt` when
its refresh token runs out unless used. `DELETE /tokens/{tokenID}` disconnects
it, recorded as `oauth.grant.revoked`. The tokens page shows it as the app,
with its scopes in the consent page's words and Disconnect.

## The token endpoint

`POST {issuer}/api/v1/oauth/token`, `application/x-www-form-urlencoded`. A
confidential app authenticates with HTTP Basic, the id and secret form-encoded
first (RFC 6749 section 2.3.1), or with both in the body, never both ways; a
public app sends `client_id` alone. Every answer carries `Cache-Control:
no-store`.

```
grant_type=authorization_code&code=armature_oac_...&redirect_uri=<the same>&code_verifier=<43 to 128 characters>
grant_type=refresh_token&refresh_token=armature_ort_...[&scope=<a subset>]
```

A narrower `scope` on refresh narrows the grant for good. Both answer:

```json
{"access_token": "armature_oat_...", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "armature_ort_...", "scope": "read issue.write"}
```

| Status | `error` | When |
|---|---|---|
| 400 | `invalid_request` | A parameter missing or sent twice, a body that is not a form, or both ways of authenticating. |
| 401 | `invalid_client` | An unknown app, a wrong secret, a pending app, or a public app sending a secret; `WWW-Authenticate: Basic` when Basic was used. |
| 400 | `invalid_grant` | A code or refresh token unknown, used, expired or another app's, a verifier that does not match, a person switched off. "This connection has ended. Connect Armature again." |
| 400 | `unauthorized_client` | An identity provider token exchanged for an app with no Keycloak client named. |
| 400 | `unsupported_grant_type` | A grant the metadata does not list. |
| 400 | `invalid_scope` | A scope beyond what may be had, or none of them `read`. |
| 400 | `invalid_target` | A `resource` that is not Armature's API. |

Refused requests are braked per app, 429 with Armature's envelope; a partner's
successful refreshes do not count.

## Token exchange (RFC 8693)

`grant_type=urn:ietf:params:oauth:grant-type:token-exchange` at the token
endpoint, with the app's own authentication and
`subject_token_type=urn:ietf:params:oauth:token-type:access_token`.

**A narrower, shorter token**, for Rotor's workflow runs. The subject is an
access token of one of the app's grants:

```
subject_token=armature_oat_...&subject_token_type=urn:ietf:params:oauth:token-type:access_token&scope=read[&armature_expires_in=600]
```

The answer is an access token with at most the grant's scopes and at most the
hour, `issued_token_type` the access token type, and no refresh token. It
belongs to the grant: it is not a row in the token list, ends with the grant,
and does not narrow it. Revoking it ends only it.

**A signed-in person, with no click.** With a shared Keycloak realm, a partner
that signed the person in through Keycloak trades their Keycloak access token
for a grant:

```
subject_token=<the Keycloak access token>&subject_token_type=urn:ietf:params:oauth:token-type:access_token&scope=read issue.write
```

- Armature verifies it as a token of its own identity provider: the issuer and
  keys are the provider's, it has not expired, `azp` is the app's
  `keycloakClientId`, and `aud` holds Armature's own OIDC client id (an
  audience mapper on the partner's Keycloak client adds it).
- The person is found by the token's verified address, as a single sign-on
  finds them. Somebody with no account here, a customer or somebody switched
  off is `invalid_grant`: "Ada Lovelace has no Armature account yet. Sign in to
  Armature once, or ask an administrator to let them in."
- Where an administrator lets people in by their groups (`admitByGroup` on
  `PUT /api/v1/oidc-provider`, off by default), somebody with no standing whose
  token's groups claim names a provider group holding a role is let in here as
  a member first: the account is made or found by the verified address, they
  join the provider's groups, and the audit log records `member.admitted` with
  the group and `via: token_exchange`. So the person is connected at their
  first sign-in to the partner, without opening Armature. The partner's access
  token must then carry the groups claim Armature reads (`groups` unless
  configured otherwise): give its Keycloak client a group membership mapper.
  Anybody else is still `invalid_grant`, and no account is made for them.
- The answer is a full grant, refresh token included. The administrator who
  named the Keycloak client consented for everybody, so no page is shown, and
  `webhooks.partner` is never given this way. A grant that holds it from an
  administrator's consent keeps it.
- An app without a Keycloak client named is `unauthorized_client`; a partner
  tries the exchange and falls back to the button.
- Keycloak tokens are not bearers anywhere else: the API takes its own tokens
  only.

In the `dynamo` realm: give the partner's Keycloak client an audience mapper
that adds Armature's client id to `aud`, and set the app's Keycloak client
under Settings, Connected apps. Keycloak's own token exchange is not switched
on; the exchange happens at Armature.

## Revocation (RFC 7009)

`POST {issuer}/api/v1/oauth/revoke`, a form, with the app's authentication as
at the token endpoint: `token=...[&token_type_hint=...]`. A grant's access or
refresh token ends the whole grant, so a partner's Disconnect is one call; an
exchanged token ends only itself. 200 with no body for every token, known,
unknown, ended or another app's (left alone); 401 `invalid_client` for bad
authentication.

## Discovery

`GET {issuer}/.well-known/oauth-authorization-server` (RFC 8414), public,
cacheable for five minutes, lists the issuer, the four endpoints
(`registration_endpoint` left out where registration is `closed`), the scopes,
`code` as the only response type and `query` as its mode, the three grant
types, `S256`, the three authentication methods for the token and revocation
endpoints, `authorization_response_iss_parameter_supported: true` and the
OpenAPI document as `service_documentation`.

`GET {issuer}/.well-known/oauth-protected-resource` (RFC 9728), for MCP
clients, names `{issuer}/api/v1` as the resource, the issuer as its
authorization server, the scopes and `header` as the bearer method. A 401 from
`/api/v1/mcp` carries `WWW-Authenticate: Bearer
resource_metadata="{issuer}/.well-known/oauth-protected-resource"`.

## The capability

`oauth.partner-tokens@1`: a connected app gets a person's token with their
consent, by authorization code with PKCE, refreshes it, revokes it, and
registers itself pending an administrator. It promises `POST /oauth/register`,
`POST /oauth/token`, `POST /oauth/revoke`, `GET /oauth/consent`,
`POST /oauth/consent` and both metadata documents; `/oauth/authorize` is the
page in front of the consent operations.

Stator offers its Connect button only when `/meta` lists it, and the manual
token form otherwise.

`oauth.token-exchange@1`: a connected app trades a person's Keycloak access
token for a grant, as [Token exchange](#token-exchange-rfc-8693) says, with no
consent page and never `webhooks.partner`. It promises `POST /oauth/token` with
the token exchange grant. A partner exchanges at the person's sign-in only when
`/meta` lists it, and offers the Connect button otherwise.

## The partner's own webhook

A connected app an administrator allowed sets up the one webhook it needs
itself, so no secret is copied by hand (#281). Each operation below needs an
access token of the app (not a session, not a key) whose grant holds
`webhooks.partner`, of a person who administers Armature when the call is made.
Otherwise 403 `insufficient_scope` ("Stator was not allowed to manage its
webhook. An Armature administrator connects it again.") or 403 `forbidden`
("Ada Lovelace is no longer an Armature administrator. An administrator
connects Stator again to manage its webhook."). Each acts on the calling app's
own endpoint only, so another endpoint is never reachable and needs no id.

| Operation | Answers |
|---|---|
| `GET /webhooks/partner` | `{webhook}`, or `{webhook: null}` when the app has none |
| `POST /webhooks/partner` | 201 `{webhook}` with `secret`; 409 `partner_webhook_exists`; 422 on `url`, `topics` |
| `PATCH /webhooks/partner` | `{webhook}`; `url`, `topics`, `version`, `enabled`, any left out keep their value; 404 when there is none |
| `POST /webhooks/partner/rotate-secret` | `{webhook}` with the new `secret`; the old one stops at once |
| `POST /webhooks/partner/test` | `{delivery}`, a ping posted now |
| `DELETE /webhooks/partner` | 204, also when there was none |

```json
{
  "url": "https://stator.example.com/api/v1/armature/webhook",
  "topics": ["issue.updated", "issue.transitioned", "issue.deleted", "issue.remote_link.added", "issue.remote_link.removed"],
  "version": 1
}
```

- **The endpoint is an ordinary one**: the same envelope, headers,
  signatures, retries, pauses and log as every endpoint
  ([webhooks](webhooks.md)), so a receiver written for `webhooks@1` works
  unchanged. Its topics come back sorted, each once.
- **Topics are named.** `*` is refused ("Name the topics Stator takes; a
  connected app cannot take every event."), and so is any `automation.` topic;
  the rest are what `GET /automation/catalog` lists. `version` is optional and
  defaults to the current envelope, 1; `2` is the CloudEvents envelope of
  `webhooks@2`, and the catalog's `versions` lists what may be named.
- **Its name** is the app's and " (connected app)", following a rename of the
  app. One that clashes with a webhook an administrator named so is 409
  `conflict`: "A webhook called Stator (connected app) is already here. Rename
  it in Settings, Webhooks, and connect again."
- **In the administrator's list** an endpoint carries `partner`
  (`clientId`, `name`), null for one an administrator made, and Settings,
  Webhooks marks it with the app's name. An administrator may turn it off and
  on, test it, redeliver, read its log and remove it. Changing its address,
  topics or version, or rotating its secret, from there is 409
  `partner_owned`: "Stator manages this webhook. Change it in Stator, or remove
  it here." A partner that finds its endpoint gone (null on `GET`, 404 on
  `PATCH`) makes a new one.
- **It is the app's**, not the administrator's: it stays when the
  administrator who connected the app leaves, and goes with the app. The
  database keeps one per app.
- **The audit log** records `webhook.created`, `webhook.updated`,
  `webhook.secret_rotated` and `webhook.deleted` as for any endpoint, with the
  app in the data and the administrator whose token made the call as the
  actor.

### The outbound guard

On `POST` and on a `PATCH` that changes `url`, Armature resolves the host and
judges its addresses as the dialer would, before it stores anything; nothing
is sent. An address inside Armature's own network is 422 `outbound_refused`:

```json
{
  "error": {
    "code": "outbound_refused",
    "message": "Armature may not reach stator.internal, which is inside its own network. Add stator.internal to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW) and try again.",
    "fields": { "url": "Add stator.internal to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW)." },
    "requestId": "..."
  }
}
```

The value named is the address's host, lower case and without the port: the
name when the address has one, the address itself when it is an IP literal.
A host that does not resolve is 422 `invalid` on `url`: "stator.example.com
does not resolve from Armature. Check the address, or Armature's DNS." The
same check holds for `POST /webhooks` and a `PATCH /webhooks/{endpointID}` that
changes the address, so the administrator's page learns it too.

## The capability for webhooks

`webhooks.partner@1`: a connected app an administrator allowed makes, changes,
tests, rotates and removes its own webhook endpoint, by the six operations
above. It is listed beside `oauth.partner-tokens@1` only. Stator offers the
administrator's one-step connect when `/meta` lists both, and the manual
webhook form otherwise.

## Decided, where the proposal asked

The proposal on #280 left questions open. This build answers them with the
proposal's own defaults, so changing one is a change to this page first:

1. Token, revocation and registration stay under `/api/v1/oauth`, where the
   capability test can name them.
2. Self-registration is on by default, braked and pending an administrator;
   `ARMATURE_OAUTH_REGISTRATION=closed` turns it off.
3. No grace window for refresh: a reused refresh token ends the grant.
4. Linking a page takes `issue.write`; there is no separate remote link scope.
5. Personal access tokens take `read` or nothing, as before.
6. One grant per person and app; a new consent or exchange adds to its scopes
   (#298), and a narrower refresh narrows them.
7. 90 days rolling for a grant and an hour for an access token, with no setting.
8. An administrator naming an app's Keycloak client consents for everybody.
9. The value to allow is named in the outbound guard's sentence and its `url`
   field, not in a field of its own.
10. Managing the partner webhook needs a grant of somebody who administers
    Armature at the time of the call. The webhook itself is the app's and
    stays when that administrator leaves; another administrator connects the
    app again to manage it.

## Not in this contract

- Marking writes made through a connected app with the app's name. The grant
  knows its app, so the audit log and the activity stream can name it; it wants
  an issue of its own.
- Token introspection (RFC 7662): every token is opaque and checked by Armature
  only.
