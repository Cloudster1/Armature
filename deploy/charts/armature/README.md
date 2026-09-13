# Armature

Installs the api, the worker, the SPA and, if asked, the PDF renderer. Postgres,
Redis and object storage are yours to provide; `values-demo.yaml` bundles them
for a look at the product.

## A trial

```sh
make helm-deps
helm install demo deploy/charts/armature -f deploy/charts/armature/values-demo.yaml
```

One database pod, a password in a file, no TLS and the demo organization
seeded. Not for real work.

Attachments go to the bundled SeaweedFS, whose S3 gateway the api is pointed
at. Its admin credentials and `secrets.s3AccessKey` / `secrets.s3SecretKey`
are set in two places that have to agree, so the chart refuses to render when
they do not: the api would sign uploads with a key the store had never been
told about, and every one would come back refused.

## A deployment

At a minimum you must give it a database host, a Redis host, an ingress host
and credentials:

```sh
helm install armature deploy/charts/armature \
  --set database.host=postgres.internal \
  --set externalRedis.host=redis.internal \
  --set ingress.host=armature.example.com \
  --set secrets.existingSecret=armature-secrets
```

### Three database roles

The application never connects as a privileged role: outside `development` the
api and the worker check, and refuse to start if the role they are given has
`rolsuper` or `rolbypassrls`.

| Value | Role | Used by |
|---|---|---|
| `database.ownerRole` | owns the schema, can `CREATE ROLE` | the roles and migrate Jobs |
| `database.appRole` | ordinary, subject to row level security | api, worker |
| `database.adminRole` | ordinary, exempt from RLS **by policy** | api, worker |

`adminRole` is exempt because the migrations write policies naming it, not
because it has `BYPASSRLS`. Granting it `BYPASSRLS` would stop the application
starting.

The role names must match what the migrations grant to. Every role reference in
a migration is wrapped in `IF EXISTS`, so a name that does not match is skipped
rather than refused: the grants and the RLS bypass go missing and nothing says
so. Change these only if you also change the migrations.

### The Secret

With `secrets.create: true` the chart builds one from the values. With
`secrets.existingSecret` it reads yours, which must carry these keys:

| Key | Contents |
|---|---|
| `db-primary-url` | DSN as the app role |
| `db-admin-url` | DSN as the admin role |
| `db-replica-urls` | comma separated DSNs, only if `database.replicaHosts` is set |
| `db-owner-url` | DSN as the owner, for the Jobs |
| `db-app-password`, `db-admin-password` | what the roles Job sets |
| `db-owner-password` | the same password as in `db-owner-url`, read by a bundled database |
| `redis-password` | only if `secrets.redisPassword` is set |
| `redis-url` | including a password if there is one |
| `s3-access-key`, `s3-secret-key` | only if `s3.enabled` |
| `pop3-password` | only if `mail.pop3.addr` is set |
| `assistant-key` | only if `assistant.url` is set |

## The bundled database and cache

`postgresql.enabled` and `redis.enabled` render two StatefulSets of this
chart's own, on `postgres:18-alpine` and `valkey/valkey:8-alpine`. One pod and
one volume each, no replica and no backup. They were Bitnami subcharts until
that catalog stopped publishing versioned tags and left the chart pulling
`bitnami/postgresql:latest`.

The database is told its superuser password through the Secret's
`db-owner-password`, which is the same value as in the `db-owner-url` the roles
and migration Jobs connect with, so there is no second place for it to drift.

Those two Jobs run **after** the release's resources exist, not before. Helm
runs `pre-install` hooks before it creates anything at all, which meant they
could not see their own Secret, let alone a bundled database. They are
`post-install` now, and still `pre-upgrade`, where both already exist.

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
