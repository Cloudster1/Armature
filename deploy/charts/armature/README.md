# Armature

Installs the api, the worker, the SPA and, if asked, the PDF renderer. Postgres,
Redis and object storage are yours to provide; `values-demo.yaml` bundles them
for a look at the product.

## A trial

```sh
make helm-deps
helm install demo deploy/charts/armature -f deploy/charts/armature/values-demo.yaml
```

One database pod, no TLS and the demo organization seeded. Not for real work.

Attachments go to the bundled SeaweedFS, whose S3 gateway the api is pointed
at. Its admin credentials and `secrets.s3AccessKey` / `secrets.s3SecretKey`
are set in two places that have to agree, so the chart refuses to render when
they do not: the api would sign uploads with a key the store had never been
told about, and every one would come back refused.

## A deployment

At a minimum you must give it a database, a Redis host and an ingress host.
With a database of your own, its owner's password goes in a basic-auth Secret
first; see [Passwords](#passwords):

```sh
kubectl create secret generic armature-db-owner --type=kubernetes.io/basic-auth \
  --from-literal=username=postgres --from-literal=password=...
helm install armature deploy/charts/armature \
  --set database.host=postgres.internal \
  --set externalRedis.host=redis.internal \
  --set ingress.host=armature.example.com
```

With a CloudNativePG cluster that the chart creates, nothing has to exist
beforehand but the operator:

```sh
helm install armature deploy/charts/armature \
  --set cnpg.enabled=true \
  --set externalRedis.host=redis.internal \
  --set ingress.host=armature.example.com
```

### Three database roles

The application never connects as a privileged role: outside `development` the
api and the worker check, and refuse to start if the role they are given has
`rolsuper` or `rolbypassrls`.

| Value | Role | Used by |
|---|---|---|
| `database.ownerRole` | owns the schema, can `CREATE ROLE` outside cnpg; empty means `armature_owner` with cnpg, `postgres` otherwise | the roles and migrate Jobs |
| `database.appRole` | ordinary, subject to row level security | api, worker |
| `database.adminRole` | ordinary, exempt from RLS **by policy** | api, worker |

`adminRole` is exempt because the migrations write policies naming it, not
because it has `BYPASSRLS`. Granting it `BYPASSRLS` would stop the application
starting.

The role names must match what the migrations grant to. Every role reference in
a migration is wrapped in `IF EXISTS`, so a name that does not match is skipped
rather than refused: the grants and the RLS bypass go missing and nothing says
so. Change these only if you also change the migrations.

### Passwords

Every database and Redis password is a `kubernetes.io/basic-auth` Secret of its
own, with the keys `username` and `password`, because that is the shape CNPG
reads. The workloads build their connection URLs from them in the pod spec
(`$(DB_APP_PASSWORD)`), so no password is ever rendered by Helm.

| Secret | Password of | Generated |
|---|---|---|
| `<fullname>-db-app` | `database.appRole` | always |
| `<fullname>-db-admin` | `database.adminRole` | always |
| `<fullname>-db-owner` | `database.ownerRole` | with `cnpg.enabled` or `postgresql.enabled` |
| `<fullname>-redis` | Redis | with `redis.enabled` |

With `secrets.generate` on, the default, the secrets Job runs before anything
else on every install, upgrade and Argo CD sync. It creates each missing Secret
with a random password and leaves every existing one alone. The Secrets are not
owned by Helm or Argo CD, so uninstalling or pruning keeps them, next to the
database volumes that hold the same passwords. Delete them by hand along with
those volumes.

A password the chart cannot hand to its server is never generated: the owner
of an external database, and an external Redis with `externalRedis.auth`. Those
Secrets must exist, and the Job stops with a message naming the one that is
missing. `secrets.names` points any of the four at a Secret of your own. The
password is put into a URL unescaped, so keep it to letters and digits.

The app and admin passwords are set by the roles Job on a database of your own
and by the operator under cnpg, so rotating one is editing its Secret.

### The other credentials

With `secrets.create: true` the chart renders a Secret from the values. With
`secrets.existingSecret` it reads yours, which must carry these keys:

| Key | Contents |
|---|---|
| `s3-access-key`, `s3-secret-key` | only if `s3.enabled` |
| `pop3-password` | only if `mail.pop3.addr` is set |
| `assistant-key` | only if `assistant.url` is set |

## CloudNativePG

`cnpg.enabled` renders a `postgresql.cnpg.io/v1` `Cluster` named
`<fullname>-postgres`. The operator has to be installed already; the chart does
not bring it. The workloads connect to `<fullname>-postgres-rw`, and to
`-ro` for reads when there is more than one instance and `cnpg.readReplicas`
is on.

`cnpg.spec` is the Cluster's spec, passed through as written, so storage,
backups, affinity, monitoring and anything else in the CNPG documentation are
set there. The chart sets only what has to agree with the rest of it:

- `bootstrap.initdb`'s `database`, `owner` and `secret`, from `database.name`,
  `database.ownerRole` and the owner Secret. Any other `initdb` field is kept.
  A `recovery` or `pg_basebackup` bootstrap is left alone entirely.
- The app and admin roles under `managed.roles`, with `pg_read_all_stats` and
  their Secrets as `passwordSecret`. Roles of your own are appended after them.
- `track_commit_timestamp`, on. Every other parameter is passed as a string,
  which is the only type the operator accepts.

The owner is `armature_owner` unless `database.ownerRole` names another. It
cannot be `postgres`, which CNPG keeps for its own superuser, and the chart
refuses to render if it is. CNPG creates the owner once, when the cluster is
first initialised, so the name cannot be changed afterwards.

With `enableSuperuserAccess: false`, the default, the owner cannot create
roles. The operator does it from `managed.roles`, and the roles Job waits for
them to appear and then only grants.

## Argo CD

The chart works unchanged as an Argo CD Application. Every hook carries both
`helm.sh/hook` and `argocd.argoproj.io/hook` annotations; Argo CD ignores the
Helm ones on anything that has its own, and Helm ignores Argo's. Under Argo CD
a sync runs:

| Phase and wave | What |
|---|---|
| PreSync -40 to -30 | the secrets Job and its ServiceAccount and Role |
| Sync -20 | ServiceAccount, env ConfigMap, the credentials Secret |
| Sync -10 | the CNPG Cluster or bundled database, and the bundled Redis; Argo CD waits for them to be healthy |
| Sync -5, -4 | the roles Job, then the migrate Job |
| Sync 0 | the workloads |
| PostSync | the seed Job, if enabled |

The roles and migrate Jobs are Sync hooks, not PostSync. PostSync only runs
once everything is healthy, and the api cannot become ready until those two
have run, so the sync would never get there.

The passwords are generated inside the cluster rather than in the templates.
Argo CD renders a chart without access to the cluster, so `lookup` finds
nothing, and a random value in a template would change on every sync and
overwrite the passwords the database was initialised with.

## The bundled database and cache

`postgresql.enabled` and `redis.enabled` render two StatefulSets of this
chart's own, on `postgres:18-alpine` and `valkey/valkey:8-alpine`. One pod and
one volume each, no replica and no backup. They were Bitnami subcharts until
that catalog stopped publishing versioned tags and left the chart pulling
`bitnami/postgresql:latest`.

The database is told its superuser password through the generated
`<fullname>-db-owner` Secret, the same one the roles and migration Jobs connect
with, so there is no second place for it to drift. The bundled Redis always
requires a password, from `<fullname>-redis`.

Under Helm the roles and migrate Jobs run **after** the release's resources
exist, not before. Helm runs `pre-install` hooks before it creates anything at
all, which meant they could not see a bundled database. They are
`post-install`, and still `pre-upgrade`, where it already exists. The roles Job
waits for the database to accept connections, which on a new CNPG cluster
takes a minute or two.

One consequence: `helm install --wait` waits for every Deployment to be ready
before it runs post-install hooks, and the api cannot be ready until the roles
Job has made the role it connects as. Install without `--wait` and let the api
settle, which takes a minute of restarts on a first install.

## What is optional

Attachments (`s3`), mail (`mail.smtpAddr`), replies by mail (`mail.pop3`), PDF
export (`render`), the assistant (`assistant`) and tracing
(`telemetry.otel`) are all off unless configured, and the product runs without
them - an attachment upload or a PDF download is refused with a message naming
the setting that would turn it on.

Redis is not optional. Both binaries ping it at startup and exit if it is not
there.

## The worker runs once

`worker.replicas` may only be 1, and the chart refuses anything else. There is
no leader election; the Redis consumers all identify themselves as the literal
`worker`; and the hourly digest remembers what it has sent in an in-process
map, so a second replica mails every digest twice.

## Verifying a change

```sh
make helm-lint    # both values files
make helm-check   # rendered manifests still match tests/golden
```

`make helm-template` rewrites the golden files when a change to them is
intended.
