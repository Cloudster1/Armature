# Personal data: what is held, why, for how long, and how it leaves

This is the record of processing for a Armature installation, written for the
person who operates one and for the person whose data is in it. The
retention windows are configuration; the defaults are what a fresh
installation does.

## Purposes

- **Running a tracker.** Accounts, memberships, sessions and API tokens exist
  so people can sign in and be told apart; issues, comments, worklogs and
  attachments are the work itself and carry who did what.
- **Telling people.** Notifications and mail carry news of the work to the
  people on it; a person chooses, per reason, whether to be told.
- **Running a service desk.** A customer's address is how a request finds
  its way back to them; one-time codes let them in without a password.
- **Keeping the organization's record.** The audit log says who changed the
  organization's configuration, for the organization's own review.
- **Keeping the service working.** Access logs, metrics and traces exist to
  find faults. They carry request ids, user ids and route patterns, never
  addresses, names or bound query values.

## What is held

| Category | Where | Personal columns | Kept | Goes when |
|---|---|---|---|---|
| Account | `app_user` | address, name, password hash, picture, time zone, language | while the account exists | erased on request: the row becomes a tombstone ("Former user", a reserved address, no password, no picture) |
| Membership | `org_member`, `role_assignment`, `group_member`, `team_member` | who belongs where and holds what | while the membership exists | an administrator removes the member, or the person erases their account |
| Sessions | `user_session` | token hash, user agent, address, last seen | `ARMATURE_SESSION_TTL` (30 days) | signed out, erased, or swept `ARMATURE_RETAIN_SESSIONS` (1 day) past expiry |
| API tokens | `api_token`, `api_token_project` | name, token hash, last used, the projects it is confined to | until revoked or expired | revoked, erased, or swept `ARMATURE_RETAIN_API_TOKENS` (30 days) past expiry |
| Invitations | `org_invite` | address | until accepted or expired | swept `ARMATURE_RETAIN_INVITES` (30 days) past expiry; deleted by erasure |
| Portal codes | `portal_code` | address, code hash | 10 minutes | swept `ARMATURE_RETAIN_PORTAL_CODES` (1 day) past expiry; deleted by erasure |
| Sign-in handshakes | `oidc_login` | state, nonce | minutes | swept `ARMATURE_RETAIN_OIDC_LOGINS` (1 day) past expiry |
| Work | `issue`, `issue_comment`, `issue_worklog`, `issue_history`, `issue_watcher`, `attachment` | reporter, assignee, author, uploader, the text itself | while the issue exists | with the issue; after erasure the author reads "Former user" |
| Notifications | `notification`, `notification_preference` | recipient, a title naming the actor | `ARMATURE_RETAIN_NOTIFICATIONS` (180 days) | swept; deleted by erasure |
| Events | `outbox_event` | actor ids, issue summaries in payloads | until published, then `ARMATURE_RETAIN_OUTBOX` (30 days) | swept; the Redis stream is capped at 10 000 entries |
| Webhook deliveries | `webhook_delivery` | the event as posted | `ARMATURE_RETAIN_WEBHOOK_DELIVERIES` (30 days) | swept; with the endpoint |
| Automation runs | `automation_run` | what a rule did | `ARMATURE_RETAIN_AUTOMATION_RUNS` (90 days) | swept; with the rule |
| Inbound mail | `inbound_mail` | sender address, subject, outcome; never the body | `ARMATURE_RETAIN_INBOUND_MAIL` (90 days) | swept; sender pseudonymised by erasure |
| Outbound mail | nowhere | | not stored | a counter is the only record |
| Imports | `import_job` | file name, a report that may echo rows | `ARMATURE_RETAIN_IMPORT_JOBS` (90 days) | swept |
| Audit log | `audit_log` | actor id, action, target, address | `ARMATURE_RETAIN_AUDIT` (365 days) | swept per organization; exportable by its administrators |
| Commits and pull requests | `git_commit`, `pull_request` | author name and address as the repository gave them | while the repository is connected | disconnected; pseudonymised when the address belongs to an erased account |
| Pictures | the bucket, `user/<id>/avatar` | the picture | while chosen | removed on request or by erasure |
| Themes | `theme`, `theme_asset`, `user_theme`, the bucket under `theme/<theme>/<file>` | the maker, the files they uploaded, who chose what | while the theme exists | deleted by the maker or an administrator, and by the maker's erasure, files included |
| Ratings | `csat_rating` | a score and a free text | with the request | with the issue |

An import adds one column to issues, comments and worklogs: the name the
tracker they came from called the row, so running the same file twice corrects
what it made. It may also make an account for somebody the file names who has
none here: no password, not active, a member so the work has somebody to point
at, and only ever at an address nobody already holds.

An administrator may also make an account outright, typing its first
password, and may reset or switch off an account whose only organization is
theirs. A reset or a switch-off ends every session and token the person holds.
The person is asked to change a password an administrator typed, from their
profile, which ends every other session of theirs.

Every retention window is a duration read from the environment
(`ARMATURE_RETAIN_*`, Go duration syntax such as `720h`); zero keeps a kind
forever, which nothing here recommends. The worker sweeps on start and every
24 hours, inside each organization for the organization's tables and as the
administrator for the tables that belong to nobody in particular.

## Where it lives and where it goes

- **Postgres**, a primary and a streaming replica. The replica is a copy of
  the primary and follows every deletion within seconds.
- **The bucket** (any S3 compatible store) holds attachments and pictures. A deleted
  attachment leaves a tombstone the worker uses to remove the object.
- **Redis** holds the event stream, capped, and the read-your-writes marks,
  which expire.
- **Logs** go to standard output as JSON: request id, trace id, method,
  route, status, duration, user id, organization slug. Where they go from
  there, and for how long, is the operator's policy.
- **Traces and metrics** are on only when asked (`make observe`), held in
  memory by Jaeger and Prometheus with no volume, and carry no addresses,
  names or bound values.
- **Leaving the system:** mail through the configured SMTP relay; webhooks to
  the endpoints an organization's administrator named; the identity
  provider, when configured, learns that somebody signed in; a model
  provider, when configured, sees the question a person asked and what the
  tools returned for them.

## Rights, and how each is served

- **Access and portability:** `GET /api/v1/auth/me/export` hands the person
  one JSON file: profile, memberships, sessions, tokens, issues reported and
  assigned, comments, worklogs, watches, notifications, saved filters and the
  audit actions they performed. On the profile page it is "Download my
  data"; a customer finds it under their name in the portal.
- **Rectification:** the profile page changes the name, the zone, the
  language and the picture. The address is what signs a person in and does
  not change here.
- **Erasure:** `DELETE /api/v1/auth/me`, from a browser session only, so a
  leaked token cannot erase its owner. Sessions, tokens, notifications,
  filters and memberships go; the account becomes a tombstone; commits and
  inbound mail carrying the address are pseudonymised. What the person wrote
  stays, attributed to "Former user": the record belongs to the team. The
  last owner of an organization is refused until ownership is handed on.
- **Who may ask for the whole person:** export and erasure reach every
  organization the person belongs to, so they take a session opened with the
  person's password. A session opened by a mailed code, an open door or single
  sign-on vouches for one organization and may ask only when the person
  belongs to no other.
- **An organization letting a member go:** `DELETE /api/v1/members/{id}` by
  an organization administrator removes the membership and what hung off it
  here; the account stays the person's for their other organizations.
- **Objection to being told:** notification preferences, per reason, in the
  inbox and by mail.

## Known limits

- There are no backups in this repository. An installation that adds them
  needs a policy for purging erased people from them.
- Webhook secrets and identity-provider client secrets are at rest in clear,
  as the schema comments say why.
- Mail arriving at a desk is trusted as far as the receiving server's own
  checks go: a message whose `Authentication-Results` says the sender is
  forged is refused, and one from a colleague's address is filed where only
  the desk sees it. Without such a header nobody checked, and the address is
  taken at face value, as mail always is.
- Access logs are the operator's to keep or discard; the application does
  not rotate or delete them.
- Third parties' names and addresses arrive through connected repositories
  without a consent path; disconnecting the repository removes them.
