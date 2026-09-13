# Reporting a vulnerability

Please report privately, through GitHub's **Report a vulnerability** button on
the Security tab of this repository. That opens a draft advisory only the
maintainers can read. Do not open a public issue for a vulnerability, and do
not describe one in a pull request.

Tell us what you found, how to reproduce it, and what an attacker gets. A
proof of concept against your own local `make up` stack is the most useful
thing you can send.

You should hear back within a week. If a report is accepted, the fix, the
advisory and the release go out together, and you are credited unless you ask
not to be.

## What is in scope

Armature is pre-1.0 and has no released versions, so there is nothing to
backport to: fixes land on `main`.

The interesting surfaces, and what each is supposed to guarantee:

| Area | The claim to try to break |
|---|---|
| Tenancy | No request reaches another organization's rows. Every tenant table has forced row level security on `current_org_id()`, and the API connects as a role that is neither owner nor superuser |
| Session provenance | A session reaches only what its proof vouches for. A mailed code, an open desk door and single sign-on each reach one organization, and the database refuses the switch as well |
| Roles | A project is a wall. What a role does not reach is not found, rather than refused |
| The desk portal | A customer sees their own requests and never an internal note |
| Outbound requests | Webhooks, git hosts and OIDC issuers are resolved first and dialled as the address that was checked, so nothing inside the network answers by accident. `ARMATURE_OUTBOUND_ALLOW` is the only way in |
| Request forgery | A write carrying the session cookie must say it is JSON and, in production, come from one of this application's origins |
| NQL | A query is compiled to bound SQL from a catalog, never concatenated |
| Attachments | The object key never reaches the browser. Bytes are streamed by the API under the session's reach, and an uploaded HTML file is never rendered from this origin |

## What is not

- **The development stack.** `make up` publishes every port on `127.0.0.1`,
  puts its passwords in the compose file, and seeds accounts with a printed
  password. That is deliberate and is not a finding. A deployment changes all
  of it; the chart under `deploy/charts/armature` is where that is written
  down.
- **`/metrics`.** Both binaries serve it without authentication, on a listener
  of their own. It is meant for the cluster network and the chart never exposes
  it through the ingress. Reaching it from outside is a deployment mistake
  rather than a vulnerability here.
- **Denial of service by volume.** Rate limits exist on the doors that are
  guessed at (sign-in, invitations, portal codes). Exhausting a database pool
  by asking for a lot of work is expected of any self-hosted application.
- **Missing hardening headers on a page that carries nothing**, absent a way
  to reach data with it.
- **Anything requiring a global administrator.** That role can already
  configure automation, webhooks and the assistant. It is the trust boundary,
  not a target inside it.

## Handling data

`docs/privacy.md` is the record of processing: what is stored about a person,
why, and for how long. If you find something held longer than it says, or
returned to somebody who should not see it, that is a report we want.
