#!/bin/sh
# Installs the demo chart into a throwaway kind cluster and checks what a render
# cannot: hook Jobs, ready workloads, a sign-in, an upgrade with no failed request.
set -eu

ROOT="${ROOT:-/work}"
CLUSTER="${CLUSTER:-armature-chart-install}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.34.0}"
RELEASE=armature
NS=armature-trial
LOCAL_PORT="${LOCAL_PORT:-18080}"
URL="http://127.0.0.1:${LOCAL_PORT}"
HELM_TIMEOUT=15m
# The api crash-loops until the hooks have made its database, so kubelet's
# back-off of up to five minutes is inside this.
ROLLOUT_TIMEOUT=360s
ANSWER_TRIES=30
DUMP_LINES=40
WATCH_EVERY=0.5
WATCH_TIMEOUT=3
# The second seed may take as long as the first, bucket wait included.
SEED_TRIES=90
SEED_EVERY=2
BUILD_VERSION="$(cat "$ROOT/VERSION")+kind"
BUILD_COMMIT="${ARMATURE_BUILD_COMMIT:-unknown}"
WORK="$(mktemp -d)"
KUBECONFIG="$WORK/kubeconfig"
export KUBECONFIG
FORWARD_PID=""
WATCH_PID=""

say() { printf '\n== %s\n' "$*"; }
fail() { printf '   FAIL  %s\n' "$*"; exit 1; }
ok() { printf '   ok    %s\n' "$*"; }

dump() {
  say "What the cluster says"
  kubectl get pods,jobs,svc -n "$NS" -o wide 2>&1 || true
  for pod in $(kubectl get pods -n "$NS" -o name 2>/dev/null); do
    ready="$(kubectl get "$pod" -n "$NS" -o jsonpath='{.status.phase} {.status.containerStatuses[*].ready}' 2>/dev/null || true)"
    case "$ready" in
      "Succeeded"*) continue ;;
      "Running"*false*) ;;
      "Running"*) continue ;;
    esac
    echo "--- $pod ($ready)"
    kubectl describe "$pod" -n "$NS" 2>&1 | tail -n "$DUMP_LINES" || true
    kubectl logs "$pod" -n "$NS" --all-containers --tail="$DUMP_LINES" 2>&1 || true
  done
}

cleanup() {
  code=$?
  [ "$code" -ne 0 ] && dump
  [ -n "$WATCH_PID" ] && kill "$WATCH_PID" 2>/dev/null
  [ -n "$FORWARD_PID" ] && kill "$FORWARD_PID" 2>/dev/null
  if [ "${KEEP_CLUSTER:-}" = 1 ]; then
    echo "cluster $CLUSTER kept; kind export kubeconfig --name $CLUSTER"
  else
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK"
  exit "$code"
}
trap cleanup EXIT

# kube-proxy needs the mark target for service addresses; without it every pod
# times out on the API server and the first Job fails after a long wait.
if [ -r /proc/modules ] && ! grep -qE '^xt_(mark|MARK) ' /proc/modules; then
  echo "note: the xt_mark kernel module is not loaded; if pods cannot reach services, run: sudo modprobe xt_mark xt_MARK"
fi

say "A cluster"
kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" --kubeconfig "$KUBECONFIG" --wait 180s
ok "kind cluster $CLUSTER is up"

say "The images, built from this checkout and loaded into the cluster"
for target in api worker migrate seed; do
  docker build -q -f "$ROOT/deploy/Dockerfile.backend" --target "$target" -t "armature-test/$target:ci" \
    --build-arg VERSION="$BUILD_VERSION" --build-arg COMMIT="$BUILD_COMMIT" "$ROOT" >/dev/null
done
docker build -q -f "$ROOT/deploy/Dockerfile.web" --target production -t armature-test/web:ci "$ROOT" >/dev/null
kind load docker-image --name "$CLUSTER" armature-test/api:ci armature-test/worker:ci armature-test/migrate:ci armature-test/seed:ci armature-test/web:ci
ok "api, worker, migrate, seed and web are on the node"

# The checkout is mounted read-only and the subcharts are not committed, so
# they are fetched into a copy.
cp -R "$ROOT/deploy/charts/armature" "$WORK/chart"
helm dependency update "$WORK/chart" >/dev/null
VALUES="-f $WORK/chart/values-demo.yaml -f $ROOT/deploy/kind/values.yaml"

say "Install"
kubectl create namespace "$NS" >/dev/null
# Not --wait: Helm would wait for the api before the post-install hooks, and
# the api is not ready until those hooks have made its database.
# shellcheck disable=SC2086
helm install "$RELEASE" "$WORK/chart" -n "$NS" $VALUES --timeout "$HELM_TIMEOUT" >/dev/null
ok "helm ran the hook Jobs"

for job in secrets-generator roles migrate seed; do
  [ "$(kubectl get "job/$RELEASE-$job" -n "$NS" -o jsonpath='{.status.succeeded}' 2>/dev/null)" = 1 ] \
    || fail "the $job Job did not succeed"
done
ok "the secrets, roles, migrate and seed Jobs all succeeded"

for workload in api worker web; do
  kubectl rollout status "deploy/$RELEASE-$workload" -n "$NS" --timeout="$ROLLOUT_TIMEOUT" >/dev/null || fail "$workload is not ready"
done
ok "api, worker and web are ready"

say "Through the web server"
kubectl port-forward -n "$NS" "svc/$RELEASE-web" "$LOCAL_PORT:80" >/dev/null 2>&1 &
FORWARD_PID=$!
for _ in $(seq 1 "$ANSWER_TRIES"); do
  curl -fs "$URL/api/v1/meta" >/dev/null 2>&1 && break
  sleep 1
done
meta="$(curl -fs "$URL/api/v1/meta")" || fail "/api/v1/meta through the web server did not answer"
[ "$(printf '%s' "$meta" | jq -r '.product + " " + .build.version')" = "armature $BUILD_VERSION" ] \
  || fail "/api/v1/meta says $meta, want armature $BUILD_VERSION"
ok "the web server reaches the api, which reports the build it was stamped with"

JAR="$WORK/cookies"
code="$(curl -s -o /dev/null -w '%{http_code}' -c "$JAR" -H 'Content-Type: application/json' \
  -d '{"email":"ada@armature.test","password":"demo password please"}' "$URL/api/v1/auth/login")"
[ "$code" = 200 ] || fail "the seeded owner could not sign in: $code"
me="$(curl -fs -b "$JAR" "$URL/api/v1/auth/me" | jq -r '.principal.user.email + " " + .principal.role')"
[ "$me" = "ada@armature.test owner" ] || fail "signed in as [$me], want [ada@armature.test owner]"
commit="$(curl -fs -b "$JAR" "$URL/api/v1/meta" | jq -r '.build.commit')"
[ "$commit" = "$BUILD_COMMIT" ] || fail "signed in, /api/v1/meta names commit $commit, want $BUILD_COMMIT"
ok "the owner the seed made signs in and is told the commit"

say "The seed again"
# Argo CD runs the seed on every sync, and the Job retries a failed run. A
# copy of the hook Job, as kubectl create job --from copies only CronJobs.
kubectl get "job/$RELEASE-seed" -n "$NS" -o json | jq --arg name "$RELEASE-seed-again" '{
  apiVersion, kind, metadata: { name: $name },
  spec: (.spec | del(.selector) | del(.template.metadata.labels["controller-uid", "batch.kubernetes.io/controller-uid", "job-name", "batch.kubernetes.io/job-name"]))
}' | kubectl create -n "$NS" -f - >/dev/null
state=""
for _ in $(seq 1 "$SEED_TRIES"); do
  state="$(kubectl get "job/$RELEASE-seed-again" -n "$NS" -o jsonpath='{.status.conditions[?(@.status=="True")].type}')"
  case "$state" in *Complete* | *Failed*) break ;; esac
  sleep "$SEED_EVERY"
done
case "$state" in
  *Complete*) ok "a second seed tops the installation up" ;;
  *) kubectl logs "job/$RELEASE-seed-again" -n "$NS" --tail="$DUMP_LINES" 2>&1 || true
     fail "the seed did not run again on the installation it made ($state)" ;;
esac

say "Upgrade"
before="$(kubectl get "job/$RELEASE-migrate" -n "$NS" -o jsonpath='{.metadata.uid}')"
( while :; do
    curl -s -o /dev/null -w '%{http_code}\n' --max-time "$WATCH_TIMEOUT" "$URL/api/v1/meta" >>"$WORK/health" 2>/dev/null || echo 000 >>"$WORK/health"
    sleep "$WATCH_EVERY"
  done ) &
WATCH_PID=$!
# shellcheck disable=SC2086
helm upgrade "$RELEASE" "$WORK/chart" -n "$NS" $VALUES --set api.replicas=2 --wait --timeout "$HELM_TIMEOUT" >/dev/null
kill "$WATCH_PID" 2>/dev/null
WATCH_PID=""
after="$(kubectl get "job/$RELEASE-migrate" -n "$NS" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
{ [ -n "$after" ] && [ "$before" != "$after" ]; } || fail "the migrate Job did not run again on upgrade"
ok "the migrate Job ran again"
[ "$(kubectl get "deploy/$RELEASE-api" -n "$NS" -o jsonpath='{.status.readyReplicas}')" = 2 ] || fail "the api did not reach two ready pods"
ok "the api is at two ready pods"
failed="$(grep -vc '^200$' "$WORK/health" || true)"
[ "$failed" = 0 ] || fail "$failed requests failed during the upgrade"
ok "no request failed while it upgraded ($(wc -l <"$WORK/health" | tr -d ' ') made)"

say "The bootstrap on the installation the seed set up"
# A real install's administrator and members, on an installation set up
# before, as an earlier install of the umbrella chart was (#324). The demo's
# open sign-up is closed with it.
# shellcheck disable=SC2086
helm upgrade "$RELEASE" "$WORK/chart" -n "$NS" $VALUES --set api.replicas=2 \
  --set jobs.seed.enabled=false --set jobs.bootstrap.enabled=true --set auth.signup=closed \
  --set bootstrap.admin.email=boot@armature.test \
  --set 'bootstrap.members[0].email=lead@armature.test' --set 'bootstrap.members[0].role=admin' \
  --wait --timeout "$HELM_TIMEOUT" >/dev/null
[ "$(kubectl get "job/$RELEASE-bootstrap" -n "$NS" -o jsonpath='{.status.succeeded}' 2>/dev/null)" = 1 ] \
  || { kubectl logs "job/$RELEASE-bootstrap" -n "$NS" --tail="$DUMP_LINES" 2>&1 || true; fail "the bootstrap Job did not succeed"; }
ok "the bootstrap Job succeeded"
[ "$(curl -fs "$URL/api/v1/auth/signup" | jq -r .open)" = false ] || fail "sign-up is still open after the bootstrap"
ok "nobody can sign up"
password="$(kubectl get secret "$RELEASE-bootstrap-admin" -n "$NS" -o jsonpath='{.data.password}' | base64 -d)"
BOOT_JAR="$WORK/boot-cookies"
code="$(curl -s -o /dev/null -w '%{http_code}' -c "$BOOT_JAR" -H 'Content-Type: application/json' \
  -d "$(jq -n --arg p "$password" '{email: "boot@armature.test", password: $p}')" "$URL/api/v1/auth/login")"
[ "$code" = 200 ] || fail "the administrator the bootstrap made could not sign in with its Secret's password: $code"
me="$(curl -fs -b "$BOOT_JAR" "$URL/api/v1/auth/me" | jq -r '.principal.user.email + " " + .principal.role')"
[ "$me" = "boot@armature.test admin" ] || fail "signed in as [$me], want [boot@armature.test admin]"
ok "its administrator signs in with the password from its Secret"
lead="$(curl -fs -b "$BOOT_JAR" "$URL/api/v1/users?q=lead%40armature.test" | jq -r '[.. | objects | select(.email? == "lead@armature.test")][0].role // empty')"
[ "$lead" = admin ] || fail "the member the bootstrap let in is [$lead], want admin"
ok "the member it let in is an administrator"

say "The chart installs, seeds twice, signs its owner in, upgrades and bootstraps"
