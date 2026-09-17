{{/* Names and labels. */}}

{{- define "armature.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "armature.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "armature.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "armature.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: armature
{{- end -}}

{{- define "armature.selectorLabels" -}}
app.kubernetes.io/name: {{ include "armature.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "armature.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "armature.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "armature.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- printf "%s-secrets" (include "armature.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "armature.image" -}}
{{- $img := index .root.Values.images .name -}}
{{- $tag := default .root.Chart.AppVersion $img.tag -}}
{{- printf "%s:%s" $img.repository $tag -}}
{{- end -}}

{{/*
Hosts. The bundled database and the CNPG cluster name their own services, so
the chart has to answer "where is the database" differently depending on
whether it brought one.
*/}}

{{- define "armature.cnpgName" -}}
{{- printf "%s-postgres" (include "armature.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "armature.dbHost" -}}
{{- if and .Values.cnpg.enabled .Values.postgresql.enabled -}}
{{- fail "cnpg.enabled and postgresql.enabled are both true. Turn one of them off: the workloads can only be pointed at one database." -}}
{{- end -}}
{{- if .Values.cnpg.enabled -}}
{{- printf "%s-rw" (include "armature.cnpgName" .) -}}
{{- else if .Values.postgresql.enabled -}}
{{- printf "%s-postgresql" .Release.Name -}}
{{- else -}}
{{- required "database.host is required unless cnpg.enabled or the bundled postgresql is enabled" .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/* Comma separated, because a template can only return a string. */}}
{{- define "armature.replicaHosts" -}}
{{- $hosts := .Values.database.replicaHosts | default list -}}
{{- if and .Values.cnpg.enabled .Values.cnpg.readReplicas (gt (int (.Values.cnpg.spec.instances | default 1)) 1) -}}
{{- $hosts = append $hosts (printf "%s-ro" (include "armature.cnpgName" .)) -}}
{{- end -}}
{{- join "," $hosts -}}
{{- end -}}

{{- define "armature.ownerRole" -}}
{{- $role := .Values.database.ownerRole -}}
{{- if and .Values.cnpg.enabled (eq $role "postgres") -}}
{{- fail "database.ownerRole is postgres, which CNPG keeps for its own superuser. Leave database.ownerRole empty to get armature_owner, or set another name." -}}
{{- end -}}
{{- default (ternary "armature_owner" "postgres" .Values.cnpg.enabled) $role -}}
{{- end -}}

{{- define "armature.redisHost" -}}
{{- if .Values.redis.enabled -}}
{{- printf "%s-redis-master" .Release.Name -}}
{{- else -}}
{{- required "externalRedis.host is required unless the bundled redis is enabled" .Values.externalRedis.host -}}
{{- end -}}
{{- end -}}

{{- define "armature.redisAuth" -}}
{{- if or .Values.redis.enabled .Values.externalRedis.auth -}}true{{- end -}}
{{- end -}}

{{/*
The basic-auth Secrets holding one password each. CNPG reads them in exactly
this shape, and the bundled database and the Jobs read the same ones.
*/}}
{{- define "armature.credentialSecret" -}}
{{- $names := .root.Values.secrets.names -}}
{{- $suffix := dict "dbOwner" "db-owner" "dbApp" "db-app" "dbAdmin" "db-admin" "redis" "redis" -}}
{{- default (printf "%s-%s" (include "armature.fullname" .root) (get $suffix .name)) (get $names .name) -}}
{{- end -}}

{{/*
A password from its Secret, as an env var the URLs below expand with $(NAME).
Kubernetes only expands variables declared earlier in the same list.
*/}}
{{- define "armature.passwordEnv" -}}
- name: {{ .var }}
  valueFrom:
    secretKeyRef:
      name: {{ include "armature.credentialSecret" (dict "root" .root "name" .name) }}
      key: password
{{- end -}}

{{/* Generated passwords are alphanumeric, so they need no escaping here. */}}
{{- define "armature.dbURL" -}}
{{- $v := .root.Values.database -}}
{{- printf "postgres://%s:$(%s)@%s:%d/%s?sslmode=%s" .role .var .host (int $v.port) $v.name $v.sslmode -}}
{{- end -}}

{{- define "armature.redisEnv" -}}
{{- $host := include "armature.redisHost" . -}}
{{- $port := int .Values.externalRedis.port -}}
{{- $db := int .Values.externalRedis.db -}}
{{- if include "armature.redisAuth" . -}}
{{ include "armature.passwordEnv" (dict "root" . "name" "redis" "var" "REDIS_PASSWORD") }}
- name: ARMATURE_REDIS_URL
  value: {{ printf "redis://:$(REDIS_PASSWORD)@%s:%d/%d" $host $port $db | quote }}
{{- else -}}
- name: ARMATURE_REDIS_URL
  value: {{ printf "redis://%s:%d/%d" $host $port $db | quote }}
{{- end -}}
{{- end -}}

{{/*
Annotations that order a hook the same way under Helm and under Argo CD. Argo
ignores helm.sh/hook on anything that carries its own hook annotation, and
Helm ignores Argo's, so each tool reads only the half meant for it.
*/}}
{{- define "armature.hookAnnotations" -}}
{{- if .helm -}}
helm.sh/hook: {{ .helm }}
helm.sh/hook-weight: {{ .weight | quote }}
helm.sh/hook-delete-policy: before-hook-creation
{{- end }}
{{- if .argo }}
argocd.argoproj.io/hook: {{ .argo }}
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
{{- end }}
argocd.argoproj.io/sync-wave: {{ .weight | quote }}
{{- end -}}

{{- define "armature.s3Endpoint" -}}
{{- if .Values.seaweedfs.enabled -}}
{{- include "armature.checkBundledS3Keys" . -}}
{{- printf "%s-seaweedfs-s3:%d" .Release.Name (int .Values.seaweedfs.s3.port) -}}
{{- else -}}
{{- .Values.s3.endpoint -}}
{{- end -}}
{{- end -}}

{{/*
The bundled store is told its credentials under its own keys, and the api is
told them under ours. Nothing joins the two, so a change to one alone would
leave uploads failing on a signature nobody had touched.
*/}}
{{- define "armature.checkBundledS3Keys" -}}
{{- $admin := (((.Values.seaweedfs.s3).credentials).admin) | default dict -}}
{{- if and $admin.accessKey (not .Values.secrets.existingSecret) -}}
{{- if or (ne $admin.accessKey .Values.secrets.s3AccessKey) (ne $admin.secretKey .Values.secrets.s3SecretKey) -}}
{{- fail "secrets.s3AccessKey and secrets.s3SecretKey must match seaweedfs.s3.credentials.admin: the api signs its uploads with the first pair and the bundled store only knows the second, so every upload would be refused as unsigned." -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "armature.appURL" -}}
{{- $scheme := ternary "https" "http" .Values.ingress.tls.enabled -}}
{{- printf "%s://%s" $scheme .Values.ingress.host -}}
{{- end -}}

{{/*
The environment every workload shares. Built once so that a setting cannot
reach the api and not the worker: they load the same configuration and refuse
the same conditions, so a difference between them is always a bug.

Credentials are not here. They come from the Secret, by reference, in
armature.secretEnv below.
*/}}
{{- define "armature.env" -}}
ARMATURE_ENV: {{ .Values.env | quote }}
ARMATURE_LOG_LEVEL: {{ .Values.logLevel | quote }}
ARMATURE_APP_URL: {{ include "armature.appURL" . | quote }}
ARMATURE_HTTP_ADDR: {{ printf ":%d" (int .Values.api.port) | quote }}
ARMATURE_METRICS_ADDR: {{ printf ":%d" (int .Values.telemetry.metricsPort) | quote }}
{{- with .Values.telemetry.otel.endpoint }}
ARMATURE_OTEL_ENDPOINT: {{ . | quote }}
ARMATURE_OTEL_SAMPLE_RATIO: {{ $.Values.telemetry.otel.sampleRatio | quote }}
{{- end }}

# Database pool. The URLs themselves carry credentials and live in the Secret.
ARMATURE_DB_MAX_CONNS: {{ .Values.database.pool.maxConns | quote }}
ARMATURE_DB_MIN_CONNS: {{ .Values.database.pool.minConns | quote }}
ARMATURE_DB_CONN_MAX_LIFETIME: {{ .Values.database.pool.maxConnLifetime | quote }}
ARMATURE_DB_HEALTH_INTERVAL: {{ .Values.database.pool.healthInterval | quote }}
ARMATURE_DB_MAX_REPLICA_LAG: {{ .Values.database.pool.maxReplicaLag | quote }}
ARMATURE_DB_REPLICA_LAG_SAMPLES: {{ .Values.database.pool.replicaLagSamples | quote }}

# Sessions and hashing. secureCookies must be true unless env is development,
# on the worker as much as the api: they share one configuration loader.
ARMATURE_SESSION_TTL: {{ .Values.auth.sessionTTL | quote }}
ARMATURE_SESSION_COOKIE: {{ .Values.auth.sessionCookie | quote }}
ARMATURE_SECURE_COOKIES: {{ .Values.auth.secureCookies | quote }}
ARMATURE_ARGON_MEMORY_KIB: {{ .Values.auth.argon.memoryKiB | quote }}
ARMATURE_ARGON_ITERATIONS: {{ .Values.auth.argon.iterations | quote }}
ARMATURE_ARGON_THREADS: {{ .Values.auth.argon.threads | quote }}
ARMATURE_READ_YOUR_WRITES_TTL: {{ .Values.auth.readYourWritesTTL | quote }}
ARMATURE_PORTAL_CODE_COOLDOWN: {{ .Values.auth.portalCodeCooldown | quote }}

{{- if .Values.s3.enabled }}
ARMATURE_S3_ENDPOINT: {{ include "armature.s3Endpoint" . | quote }}
ARMATURE_S3_BUCKET: {{ .Values.s3.bucket | quote }}
ARMATURE_S3_REGION: {{ .Values.s3.region | quote }}
ARMATURE_S3_USE_SSL: {{ .Values.s3.useSSL | quote }}
{{- end }}

{{- with .Values.mail.smtpAddr }}
ARMATURE_SMTP_ADDR: {{ . | quote }}
ARMATURE_MAIL_FROM: {{ $.Values.mail.from | quote }}
{{- end }}
{{- with .Values.mail.inbox }}
ARMATURE_MAIL_INBOX: {{ . | quote }}
{{- end }}
{{- with .Values.mail.pop3.addr }}
ARMATURE_POP3_ADDR: {{ . | quote }}
ARMATURE_POP3_USER: {{ $.Values.mail.pop3.user | quote }}
ARMATURE_POP3_TLS: {{ $.Values.mail.pop3.tls | quote }}
ARMATURE_POP3_INTERVAL: {{ $.Values.mail.pop3.interval | quote }}
{{- end }}

{{- if .Values.render.enabled }}
ARMATURE_RENDER_URL: {{ printf "http://%s-render:8090" (include "armature.fullname" .) | quote }}
ARMATURE_RENDER_TIMEOUT: {{ .Values.render.timeout | quote }}
{{- end }}

{{- with .Values.assistant.url }}
ARMATURE_ASSISTANT_URL: {{ . | quote }}
ARMATURE_ASSISTANT_MODEL: {{ $.Values.assistant.model | quote }}
ARMATURE_ASSISTANT_TIMEOUT: {{ $.Values.assistant.timeout | quote }}
{{- end }}

{{- with .Values.network.trustedProxies }}
ARMATURE_TRUSTED_PROXIES: {{ join "," . | quote }}
{{- end }}
{{- with .Values.network.corsOrigins }}
ARMATURE_CORS_ORIGINS: {{ join "," . | quote }}
{{- end }}
{{- with .Values.network.outboundAllow }}
ARMATURE_OUTBOUND_ALLOW: {{ join "," . | quote }}
{{- end }}
ARMATURE_OIDC_REDIRECT_URL: {{ default (printf "%s/api/v1/auth/oidc/callback" (include "armature.appURL" .)) .Values.auth.oidc.redirectURL | quote }}
{{- with .Values.auth.oidc.backchannel }}
ARMATURE_OIDC_BACKCHANNEL: {{ . | quote }}
{{- end }}

{{/*
Spelled out rather than derived from the key names. A loop with snakecase
would quietly produce ARMATURE_RETAIN_APITOKENS the day somebody renames a
value, and nothing would notice: an unknown variable is ignored, and the
default silently applies.
*/}}
ARMATURE_RETAIN_SESSIONS: {{ .Values.retention.sessions | quote }}
ARMATURE_RETAIN_PORTAL_CODES: {{ .Values.retention.portalCodes | quote }}
ARMATURE_RETAIN_INVITES: {{ .Values.retention.invites | quote }}
ARMATURE_RETAIN_API_TOKENS: {{ .Values.retention.apiTokens | quote }}
ARMATURE_RETAIN_OIDC_LOGINS: {{ .Values.retention.oidcLogins | quote }}
ARMATURE_RETAIN_NOTIFICATIONS: {{ .Values.retention.notifications | quote }}
ARMATURE_RETAIN_OUTBOX: {{ .Values.retention.outbox | quote }}
ARMATURE_RETAIN_WEBHOOK_DELIVERIES: {{ .Values.retention.webhookDeliveries | quote }}
ARMATURE_RETAIN_AUTOMATION_RUNS: {{ .Values.retention.automationRuns | quote }}
ARMATURE_RETAIN_INBOUND_MAIL: {{ .Values.retention.inboundMail | quote }}
ARMATURE_RETAIN_IMPORT_JOBS: {{ .Values.retention.importJobs | quote }}
ARMATURE_RETAIN_AUDIT: {{ .Values.retention.audit | quote }}
{{- end -}}

{{/* The credentials, by reference. Never rendered into a pod spec. */}}
{{- define "armature.secretEnv" -}}
{{- $secret := include "armature.secretName" . -}}
{{- $v := .Values.database -}}
{{- $host := include "armature.dbHost" . -}}
{{ include "armature.passwordEnv" (dict "root" . "name" "dbApp" "var" "DB_APP_PASSWORD") }}
{{ include "armature.passwordEnv" (dict "root" . "name" "dbAdmin" "var" "DB_ADMIN_PASSWORD") }}
- name: ARMATURE_DB_PRIMARY_URL
  value: {{ include "armature.dbURL" (dict "root" . "role" $v.appRole "var" "DB_APP_PASSWORD" "host" $host) | quote }}
- name: ARMATURE_DB_ADMIN_URL
  value: {{ include "armature.dbURL" (dict "root" . "role" $v.adminRole "var" "DB_ADMIN_PASSWORD" "host" $host) | quote }}
{{- with include "armature.replicaHosts" . }}
- name: ARMATURE_DB_REPLICA_URLS
  {{- $urls := list }}
  {{- range splitList "," . }}
  {{- $urls = append $urls (include "armature.dbURL" (dict "root" $ "role" $v.appRole "var" "DB_APP_PASSWORD" "host" .)) }}
  {{- end }}
  value: {{ join "," $urls | quote }}
{{- end }}
{{ include "armature.redisEnv" . }}
{{- if .Values.s3.enabled }}
- name: ARMATURE_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: s3-access-key
- name: ARMATURE_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: s3-secret-key
{{- end }}
{{- if .Values.mail.pop3.addr }}
- name: ARMATURE_POP3_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: pop3-password
{{- end }}
{{- if .Values.assistant.url }}
- name: ARMATURE_ASSISTANT_KEY
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: assistant-key
{{- end }}
{{- end -}}
