# Armature - all toolchains run in containers; nothing is installed on the host.

.DEFAULT_GOAL := help

# Pinned to 1.26 deliberately: under golang:1.27 on this host every test binary
# is killed about a second into its run ("signal: killed"), and some built
# binaries die a couple of seconds after startup. 1.25 and 1.26 are unaffected.
GO_IMAGE   ?= golang:1.26-alpine
# The integration suite clones and pushes through the git proxy with git
# itself, which the alpine image lacks and this one carries.
INTEGRATION_IMAGE ?= golang:1.26-bookworm
NODE_IMAGE ?= node:26-alpine
# 3.18 at the earliest: the SeaweedFS chart uses the fromToml template
# function, which older Helm does not define and fails the render on.
HELM_IMAGE ?= alpine/helm:3.19.0
# Busybox's sh, grep and sed, the smallest shell the scripts in deploy/ promise
# to run under, with git for the scratch repositories their tests make.
SCRIPTS_IMAGE    ?= alpine/git:v2.52.0
ACTIONLINT_IMAGE ?= rhysd/actionlint:1.7.12
UID        := $(shell id -u)
GID        := $(shell id -g)
ROOT       := $(shell pwd)

# Caches are bind-mounted into ./.cache (gitignored) so they are owned by the
# invoking user; named volumes would come up root-owned and break rootless runs.
# Credentials for the local stack. Override in the environment for anything
# that is not a throwaway development database.
POSTGRES_PASSWORD  ?= armature
APP_DB_PASSWORD    ?= armature_app
ADMIN_DB_PASSWORD  ?= armature_admin
REDIS_PASSWORD     ?= armature_redis
S3_ACCESS_KEY      ?= armature
S3_SECRET_KEY      ?= armature-dev-secret
# Seals authenticator secrets; the compose file's api uses the same one, so a
# row the suite leaves behind opens for the stack too.
SECRET_KEY         ?= YXJtYXR1cmUtZGV2LWtleS1ub3QtYS1zZWNyZXQtMDA=
# The compose stack's Gitea administrator, whom the git proxy's tests make
# their repositories as.
GITEA_USER         ?= demo
GITEA_PASSWORD     ?= demo password please

GO_CACHE_VOL   := $(ROOT)/.cache/gocache
GO_MOD_VOL     := $(ROOT)/.cache/gomod
NPM_CACHE_VOL  := $(ROOT)/.cache/npm
GO_CACHES      := $(GO_CACHE_VOL) $(GO_MOD_VOL)

# Made here, as the caller, before a container mounts them: a directory Docker
# creates for a mount is owned by root, and the container running as the
# caller can then neither write to it nor remove it.
$(GO_CACHES) $(NPM_CACHE_VOL):
	mkdir -p $@

# The OpenAPI document lives beside the code at api/, outside the backend
# module, so it is mounted separately and the test that checks it is told where.
# The files that carry the release are mounted the same way, for the test that
# refuses them disagreeing, and so are the README whose table of variables a
# test holds against the code and the Keycloak realm the seed has to match.
DOCKER_GO = docker run --rm -t \
	-u $(UID):$(GID) \
	-v $(ROOT)/backend:/src \
	-v $(ROOT)/api:/api \
	-v $(ROOT)/docs/design/themes:/themes:ro \
	-v $(ROOT)/VERSION:/release/VERSION:ro \
	-v $(ROOT)/web/package.json:/release/package.json:ro \
	-v $(ROOT)/web/package-lock.json:/release/package-lock.json:ro \
	-v $(ROOT)/$(CHART)/Chart.yaml:/release/Chart.yaml:ro \
	-v $(ROOT)/README.md:/docs/README.md:ro \
	-v $(ROOT)/deploy/keycloak/realm.json:/keycloak/realm.json:ro \
	-e ARMATURE_OPENAPI_FILE=/api/openapi.json \
	-e ARMATURE_README_FILE=/docs/README.md \
	-e ARMATURE_THEME_FILES=/themes \
	-e ARMATURE_RELEASE_FILES=/release \
	-e ARMATURE_REALM_FILE=/keycloak/realm.json \
	-v $(GO_CACHE_VOL):/gocache \
	-v $(GO_MOD_VOL):/gomod \
	-e GOCACHE=/gocache \
	-e GOMODCACHE=/gomod \
	-e GOFLAGS=-buildvcs=false \
	-w /src $(GO_IMAGE)

# Helm keeps its cache under $HOME, which is not writable for the invoking
# user inside this image; /tmp is.
DOCKER_HELM = docker run --rm \
	-u $(UID):$(GID) \
	-v $(ROOT):/src \
	-e HELM_CACHE_HOME=/tmp -e HELM_CONFIG_HOME=/tmp -e HELM_DATA_HOME=/tmp \
	-w /src $(HELM_IMAGE)

# The shipped themes are written in Go, so the contrast test that judges them
# is handed that one file beside the web module.
DOCKER_NODE = docker run --rm -t \
	-u $(UID):$(GID) \
	-v $(ROOT)/web:/app \
	-v $(ROOT)/backend/internal/theme/examples.go:/theme-examples.go:ro \
	-e ARMATURE_THEME_EXAMPLES=/theme-examples.go \
	-v $(NPM_CACHE_VOL):/npmcache \
	-e npm_config_cache=/npmcache \
	-w /app $(NODE_IMAGE)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---------- backend ----------

.PHONY: go
go: | $(GO_CACHES) ## Run an arbitrary go command in the toolchain container: make go ARGS="version"
	$(DOCKER_GO) go $(ARGS)

.PHONY: tidy
tidy: | $(GO_CACHES) ## go mod tidy
	$(DOCKER_GO) go mod tidy

.PHONY: build
build: | $(GO_CACHES) ## Compile all backend binaries
	$(DOCKER_GO) go build ./...

.PHONY: test
test: test-scripts test-e2e-runner | $(GO_CACHES) ## Run backend unit tests and the script tests
	$(DOCKER_GO) go test ./...

# How the browser suite splits its scenarios across jobs, tested without a browser.
.PHONY: test-e2e-runner
test-e2e-runner: ## Test how the browser suite splits its scenarios
	docker run --rm -t -u $(UID):$(GID) -v $(ROOT)/e2e:/e2e:ro -w /e2e $(NODE_IMAGE) node --test /e2e/shard.test.mjs

.PHONY: test-scripts
test-scripts: ## Test the scripts in deploy/ and the push hook, and lint the GitHub workflows
	docker run --rm -t -u $(UID):$(GID) -e HOME=/tmp -v $(ROOT):/src:ro -w /src \
		--entrypoint sh $(SCRIPTS_IMAGE) -c 'for t in deploy/tests/*_test.sh; do sh "$$t" || exit 1; done'
	docker run --rm -t -u $(UID):$(GID) -v $(ROOT):/src:ro -w /src $(ACTIONLINT_IMAGE) -color

.PHONY: hooks
hooks: ## Have git refuse a push whose branch name or commit subjects the release script cannot read
	git config core.hooksPath .githooks

# The integration suite talks to the running compose stack: triggers,
# streaming replication and replay lag are properties of Postgres itself and
# cannot be usefully faked. Bring the stack up with `make up` first.
.PHONY: test-integration
test-integration: | $(GO_CACHES) ## Run the integration suite against the running stack
	@# The worker is stopped for the duration. The relay tests start their own
	@# relays and assert on exactly which events were published; a background
	@# worker draining the same outbox would race them.
	@docker compose -f deploy/docker-compose.yml stop worker >/dev/null 2>&1 || true
	@docker run --rm -t \
		-u $(UID):$(GID) \
		--network armature_default \
		-v $(ROOT)/backend:/src \
		-v $(GO_CACHE_VOL):/gocache \
		-v $(GO_MOD_VOL):/gomod \
		-e GOCACHE=/gocache -e GOMODCACHE=/gomod -e GOFLAGS=-buildvcs=false \
		-e ARMATURE_DB_PRIMARY_URL='postgres://armature_app:$(APP_DB_PASSWORD)@postgres-primary:5432/armature?sslmode=disable' \
		-e ARMATURE_DB_REPLICA_URLS='postgres://armature_app:$(APP_DB_PASSWORD)@postgres-replica:5432/armature?sslmode=disable' \
		-e ARMATURE_DB_ADMIN_URL='postgres://armature_admin:$(ADMIN_DB_PASSWORD)@postgres-primary:5432/armature?sslmode=disable' \
		-e ARMATURE_TEST_SUPERUSER_URL='postgres://armature:$(POSTGRES_PASSWORD)@postgres-primary:5432/armature?sslmode=disable' \
		-e ARMATURE_TEST_REPLICA_SUPERUSER_URL='postgres://armature:$(POSTGRES_PASSWORD)@postgres-replica:5432/armature?sslmode=disable' \
		-e ARMATURE_DB_MAX_REPLICA_LAG=1s \
		-e ARMATURE_OUTBOUND_ALLOW='127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16' \
		-e ARMATURE_REDIS_URL='redis://:$(REDIS_PASSWORD)@redis:6379/0' \
		-e ARMATURE_SECRET_KEY='$(SECRET_KEY)' \
		-e ARMATURE_S3_ENDPOINT=seaweedfs:8333 -e ARMATURE_S3_BUCKET=armature-test-attachments \
		-e ARMATURE_S3_ACCESS_KEY='$(S3_ACCESS_KEY)' -e ARMATURE_S3_SECRET_KEY='$(S3_SECRET_KEY)' \
		-e ARMATURE_SMTP_ADDR=mailpit:1025 -e ARMATURE_POP3_ADDR=mailpit:1110 -e ARMATURE_POP3_USER=armature -e ARMATURE_POP3_PASSWORD=armature \
		-e ARMATURE_TEST_TLS_SMTP_ADDR=mailpit-tls:1025 -e ARMATURE_TEST_TLS_POP3_ADDR=mailpit-tls:1110 \
		-e ARMATURE_TEST_GITEA_URL=http://gitea:3000 -e ARMATURE_TEST_GITEA_USER='$(GITEA_USER)' -e ARMATURE_TEST_GITEA_PASSWORD='$(GITEA_PASSWORD)' \
		-e CGO_ENABLED=0 \
		-w /src $(INTEGRATION_IMAGE) go test -tags integration -count=1 $(TESTFLAGS) ./test/... ; \
		status=$$? ; \
		docker compose -f deploy/docker-compose.yml start worker >/dev/null 2>&1 || true ; \
		exit $$status

# The browser suite drives a real Chromium against the running stack. The image
# already carries both Chromium and puppeteer, so nothing is installed here
# either. The suite is mounted inside the image's own app directory because ES
# module resolution walks up from the importing file and ignores NODE_PATH.
# WORKERS=<n> sets how many browsers run scenarios side by side; ONLY=<words>
# narrows the run to the scenarios whose names contain them, or to one file's
# (ONLY=board). SHUFFLE=1 runs the parallel pool in a random order and prints
# the seed, which SHUFFLE=<seed> repeats. SHARD=<i>/<n> runs one n-th of the
# suite, as each of CI's browser jobs does. E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD
# name the administrator the run acts as, the seed's Ada unless said otherwise;
# on an installation nobody has set up, the run signs them up through the
# sign-up page first. Each run writes .cache/e2e/timings.json and lists its slowest.
#
# The suite runs against the production build behind the production nginx,
# compose's web-e2e, which is built first; with web/ unchanged every layer is
# cached. It is recreated each time because nginx looks the api up only once.
# E2E_WEB=dev runs against the Vite dev server instead, with no build, for
# debugging a scenario with hot reload.
E2E_IMAGE ?= zenika/alpine-chrome:with-puppeteer
E2E_ADMIN_EMAIL    ?= ada@armature.test
E2E_ADMIN_PASSWORD ?= demo password please
E2E_WEB   ?= production
ifeq ($(E2E_WEB),dev)
E2E_BASE_URL := http://web:5173
E2E_WEB_UP   := @true
else
E2E_BASE_URL := http://web-e2e
E2E_WEB_UP   := docker compose -f deploy/docker-compose.yml --profile e2e up -d --no-deps --build --force-recreate --wait web-e2e
endif

.PHONY: test-e2e
test-e2e: ## Run the browser end-to-end suite against the running stack
	@mkdir -p $(ROOT)/.cache/e2e
	$(E2E_WEB_UP)
	docker run --rm -t \
		-u $(UID):$(GID) \
		--network armature_default \
		-v $(ROOT)/e2e:/usr/src/app/e2e:ro \
		-v $(ROOT)/.cache/e2e:/artifacts \
		-e HOME=/tmp \
		-e E2E_BASE_URL=$(E2E_BASE_URL) \
		-e E2E_WEB=$(E2E_WEB) \
		-e E2E_GITEA_URL=http://gitea:3000 \
		-e E2E_ARTIFACTS=/artifacts \
		-e E2E_ONLY='$(ONLY)' \
		-e E2E_WORKERS='$(WORKERS)' \
		-e E2E_SHUFFLE='$(SHUFFLE)' \
		-e E2E_SHARD='$(SHARD)' \
		-e E2E_ADMIN_EMAIL='$(E2E_ADMIN_EMAIL)' \
		-e E2E_ADMIN_PASSWORD='$(E2E_ADMIN_PASSWORD)' \
		-w /usr/src/app \
		--entrypoint node $(E2E_IMAGE) /usr/src/app/e2e/suite.mjs

# A split run's jobs each leave their timings; this adds them up for CI's
# summary and fails when a shard is missing or a scenario ran twice or not at all.
E2E_SHARDS_DIR ?= .cache/e2e-shards
.PHONY: e2e-report
e2e-report: ## Add up a split browser run's timings: make e2e-report E2E_SHARDS_DIR=<dir>
	@docker run --rm -u $(UID):$(GID) -v $(ROOT)/e2e:/e2e:ro -v $(abspath $(E2E_SHARDS_DIR)):/shards -w /e2e $(NODE_IMAGE) node /e2e/report.mjs /shards

.PHONY: test-all
test-all: test test-integration test-e2e lint | $(NPM_CACHE_VOL) ## Everything: unit, lint, integration and browser
	$(DOCKER_NODE) npm run test

# ESLint with the rules Stator and Exciter share; the accessibility checks and
# the style ratchet run inside npm run test.
.PHONY: lint
lint: | $(NPM_CACHE_VOL) ## Lint the web app with the rules Stator and Exciter share
	$(DOCKER_NODE) npm run lint

# Biome formats web/ and e2e/ as Stator and Exciter do, from its own image.
BIOME_IMAGE ?= ghcr.io/biomejs/biome:2.5.8
DOCKER_BIOME = docker run --rm -u $(UID):$(GID) -v $(ROOT):/code -w /code $(BIOME_IMAGE)

.PHONY: check-format
check-format: ## Fail when a file in web/ or e2e/ is not formatted as biome.json says
	@$(DOCKER_BIOME) format web e2e \
		|| { echo "Some files in web/ or e2e/ are not formatted. Run make format, review the changes and commit them."; exit 1; }

.PHONY: format
format: ## Format web/ and e2e/ in place as biome.json says
	$(DOCKER_BIOME) format --write web e2e

.PHONY: vet
vet: | $(GO_CACHES) ## go vet
	$(DOCKER_GO) go vet ./...

.PHONY: fmt
fmt: | $(GO_CACHES) ## gofmt the backend
	$(DOCKER_GO) gofmt -l -w .

# The OpenAPI document is derived from the server's own tables and types; the
# checked in copy at api/openapi.json is what a test compares against and what
# the TypeScript client is generated from.
.PHONY: openapi
openapi: | $(GO_CACHES) $(NPM_CACHE_VOL) ## Regenerate api/openapi.json and the TypeScript client types from it
	$(DOCKER_GO) go run ./cmd/openapi /api/openapi.json
	docker run --rm -t -u $(UID):$(GID) \
		-v $(ROOT)/web:/app -v $(ROOT)/api:/api \
		-v $(NPM_CACHE_VOL):/npmcache -e npm_config_cache=/npmcache \
		-w /app $(NODE_IMAGE) npx openapi-typescript /api/openapi.json -o src/api/schema.d.ts

.PHONY: shell
shell: | $(GO_CACHES) ## Interactive shell in the Go toolchain container
	docker run --rm -it -u $(UID):$(GID) \
		-v $(ROOT)/backend:/src -v $(GO_CACHE_VOL):/gocache -v $(GO_MOD_VOL):/gomod \
		-e GOCACHE=/gocache -e GOMODCACHE=/gomod -e GOFLAGS=-buildvcs=false \
		-w /src $(GO_IMAGE) sh

# Dynamo holds the suite's realm; each tool's copy has to be the same file, keys
# order aside. Until Dynamo has one there is nothing to compare, which passes.
DYNAMO_REALM_URL ?= https://raw.githubusercontent.com/praetorianer777/Dynamo/main/deploy/keycloak/realm.json

# govulncheck reports only the vulnerable code the binaries can reach, which
# keeps it quiet enough to fail on; npm audit looks at what the web app ships.
GOVULNCHECK_VERSION ?= v1.8.0
NPM_AUDIT_LEVEL     ?= high

.PHONY: audit-deps audit-go audit-npm
audit-deps: audit-go audit-npm ## Look for known vulnerabilities in the Go and npm dependencies (make -k runs both)

audit-go: | $(GO_CACHES) ## Look for known vulnerabilities the backend can reach
	$(DOCKER_GO) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

audit-npm: | $(NPM_CACHE_VOL) ## Look for known vulnerabilities in what the web app ships
	$(DOCKER_NODE) npm audit --omit=dev --audit-level=$(NPM_AUDIT_LEVEL)

.PHONY: realm-check
realm-check: ## Fail if deploy/keycloak/realm.json differs from Dynamo's canonical realm
	@docker run --rm -u $(UID):$(GID) -v $(ROOT)/deploy/keycloak:/realm:ro $(NODE_IMAGE) \
		node /realm/realm-check.mjs '$(DYNAMO_REALM_URL)' /realm/realm.json

## ---------- release ----------

# VERSION is read here only as the argument; the file of the same name is what
# the release writes. See deploy/release.sh for what is refused and why.
.PHONY: release
release: ## Open the release pull request from an up-to-date main: make release [VERSION=x.y.z] [DRY_RUN=1]
	@sh deploy/release.sh '$(VERSION)' $(if $(DRY_RUN),--dry-run)

.PHONY: release-tag
release-tag: ## Tag the merged release pull request and push the tag, which publishes it: make release-tag [DRY_RUN=1]
	@sh deploy/release.sh tag $(if $(DRY_RUN),--dry-run)

## ---------- frontend ----------

.PHONY: npm
npm: | $(NPM_CACHE_VOL) ## Run an arbitrary npm command: make npm ARGS="install"
	$(DOCKER_NODE) npm $(ARGS)

.PHONY: web-install
web-install: | $(NPM_CACHE_VOL) ## Install frontend dependencies
	$(DOCKER_NODE) npm install

.PHONY: web-build
web-build: | $(NPM_CACHE_VOL) ## Production build of the SPA
	$(DOCKER_NODE) npm run build

## ---------- stack ----------

.PHONY: up
up: ## Start the full stack (postgres primary + replica, redis, gitea, api, worker, web)
	docker compose -f deploy/docker-compose.yml up -d --build

.PHONY: down
down: ## Stop the stack
	docker compose -f deploy/docker-compose.yml --profile e2e down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose -f deploy/docker-compose.yml --profile e2e down -v

.PHONY: logs
logs: ## Tail stack logs
	docker compose -f deploy/docker-compose.yml logs -f

.PHONY: observe
observe: ## Start Jaeger and Prometheus beside the stack, with the api and worker sending traces to Jaeger
	ARMATURE_OTEL_ENDPOINT=http://jaeger:4318/v1/traces docker compose -f deploy/docker-compose.yml --profile observe up -d
	@echo "Jaeger: http://localhost:$${JAEGER_PORT:-16686}   Prometheus: http://localhost:$${PROMETHEUS_PORT:-9090}"

.PHONY: observe-down
observe-down: ## Stop Jaeger and Prometheus and stop sending traces
	docker compose -f deploy/docker-compose.yml --profile observe stop jaeger prometheus
	docker compose -f deploy/docker-compose.yml --profile observe rm -f jaeger prometheus
	docker compose -f deploy/docker-compose.yml up -d api worker

.PHONY: stack-stats
stack-stats: ## What each container costs right now, and how much Redis holds
	@docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}' $$(docker compose -f deploy/docker-compose.yml ps -q)
	@docker compose -f deploy/docker-compose.yml exec -T redis redis-cli info memory | grep -E '^used_memory_(rss_)?human'

.PHONY: seed
seed: ## Fill the running stack's installation with demo people and projects
	docker compose -f deploy/docker-compose.yml run --rm --build seed

.PHONY: demo-snapshot
demo-snapshot: ## Take the suite demo's GitHub issues again, on the host with gh signed in
	@sh deploy/demo-snapshot.sh

# ImageMagick runs from an image, as every other tool does, and as root only
# for the package install; the files are handed back to the caller after.
BRANDING_IMAGE ?= alpine:3.22

.PHONY: branding
branding: ## Make web/public's logo and icons from docs/armature-logo.jpg
	docker run --rm -v $(ROOT):/code $(BRANDING_IMAGE) sh -ec \
		'apk add --no-cache imagemagick imagemagick-jpeg imagemagick-webp >/dev/null && /code/deploy/branding.sh && chown -R $(UID):$(GID) /code/web/public'

.PHONY: psql
psql: ## Open psql against the primary
	docker compose -f deploy/docker-compose.yml exec postgres-primary psql -U armature -d armature

.PHONY: screenshots
screenshots: ## Refresh the pictures in docs/ from the seeded demo, through the browser runner
	@mkdir -p $(ROOT)/.cache/e2e/shots
	$(E2E_WEB_UP)
	docker run --rm -t \
		-u $(UID):$(GID) \
		--network armature_default \
		-v $(ROOT)/e2e:/usr/src/app/e2e:ro \
		-v $(ROOT)/.cache/e2e:/artifacts \
		-e HOME=/tmp \
		-e E2E_BASE_URL=$(E2E_BASE_URL) \
		-e E2E_ARTIFACTS=/artifacts \
		-w /usr/src/app \
		--entrypoint node $(E2E_IMAGE) /usr/src/app/e2e/screenshots.mjs
	cp $(ROOT)/.cache/e2e/shots/*.png $(ROOT)/docs/

## ---------- chart ----------

CHART := deploy/charts/armature

.PHONY: helm-deps
helm-deps: ## Fetch the chart's subchart tarballs (needed once, and after Chart.yaml changes)
	$(DOCKER_HELM) dependency update $(CHART)

.PHONY: helm-lint
helm-lint: ## Lint the chart with both values files
	$(DOCKER_HELM) lint $(CHART) --values $(CHART)/values.yaml --set database.host=db --set externalRedis.host=redis
	$(DOCKER_HELM) lint $(CHART) --values $(CHART)/values.yaml $(CNPG_ARGS)
	$(DOCKER_HELM) lint $(CHART) --values $(CHART)/values-demo.yaml

# A golden render, the same bargain api/openapi.json strikes: the rendered
# manifests are committed, so a change to a template or a default has to show
# up in a diff somebody reads rather than arriving silently in a cluster.
GOLDEN := $(CHART)/tests/golden
GOLDEN_ARGS := --set database.host=db.example.com --set externalRedis.host=redis.example.com
CNPG_ARGS := --set cnpg.enabled=true --set externalRedis.host=redis.example.com

.PHONY: helm-template
helm-template: ## Render the chart into the golden files
	@mkdir -p $(GOLDEN)
	$(DOCKER_HELM) template armature $(CHART) $(GOLDEN_ARGS) > $(GOLDEN)/default.yaml
	$(DOCKER_HELM) template armature $(CHART) -f $(CHART)/values-demo.yaml > $(GOLDEN)/demo.yaml
	$(DOCKER_HELM) template armature $(CHART) $(GOLDEN_ARGS) --set s3.enabled=true --set s3.endpoint=s3.example.com \
		--set secrets.s3AccessKey=key --set secrets.s3SecretKey=secret --set render.enabled=true \
		--set mail.smtpAddr=smtp.example.com:587 --set mail.smtpUser=armature --set secrets.smtpPassword=secret \
		--set mail.inbox=desk@example.com --set mail.insecureTLS=true \
		--set database.replicaHosts={replica.example.com} --set externalRedis.auth=true \
		--set attachments.persistence.enabled=true \
		--set jobs.bootstrap.enabled=true --set bootstrap.admin.email=admin@example.com \
		--set bootstrap.members[0].email=lead@example.com --set bootstrap.members[0].role=admin > $(GOLDEN)/everything.yaml
	$(DOCKER_HELM) template armature $(CHART) $(CNPG_ARGS) > $(GOLDEN)/cnpg.yaml

# Too slow for every pull request (it builds the images and starts about a
# dozen pods); the Chart install workflow runs it when the chart's inputs change.
CHART_TOOLS_IMAGE ?= armature-chart-tools:local

.PHONY: chart-tools
chart-tools: ## Build the image with kind, helm and kubectl the chart install test runs in
	docker build -q -t $(CHART_TOOLS_IMAGE) $(ROOT)/deploy/kind >/dev/null

# The cluster name carries a hash of the checkout, so two clones do not share one.
.PHONY: test-chart-install
test-chart-install: chart-tools ## Install and upgrade the demo chart in a throwaway kind cluster: [KEEP_CLUSTER=1]
	docker run --rm --network host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(ROOT):/work:ro \
		-e ARMATURE_BUILD_COMMIT=$$(git rev-parse HEAD) \
		-e KEEP_CLUSTER -e CLUSTER=armature-chart-install-$$(printf '%s' $(ROOT) | cksum | cut -d' ' -f1) \
		$(CHART_TOOLS_IMAGE) sh /work/deploy/kind/chart-install-test.sh

.PHONY: helm-check
helm-check: ## Fail if the rendered chart differs from the committed golden files
	@$(MAKE) --no-print-directory helm-template
	@git diff --quiet --exit-code -- $(GOLDEN) \
		&& echo "  chart renders as committed" \
		|| { echo "  the rendered chart differs from $(GOLDEN); read the diff and commit it if it is right"; git --no-pager diff --stat -- $(GOLDEN); exit 1; }
