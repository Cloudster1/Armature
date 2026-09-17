# Armature - all toolchains run in containers; nothing is installed on the host.

# Pinned to 1.26 deliberately: under golang:1.27 on this host every test binary
# is killed about a second into its run ("signal: killed"), and some built
# binaries die a couple of seconds after startup. 1.25 and 1.26 are unaffected.
GO_IMAGE   ?= golang:1.26-alpine
NODE_IMAGE ?= node:26-alpine
# 3.18 at the earliest: the SeaweedFS chart uses the fromToml template
# function, which older Helm does not define and fails the render on.
HELM_IMAGE ?= alpine/helm:3.19.0
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

GO_CACHE_VOL   := $(ROOT)/.cache/gocache
GO_MOD_VOL     := $(ROOT)/.cache/gomod
NPM_CACHE_VOL  := $(ROOT)/.cache/npm

# The OpenAPI document lives beside the code at api/, outside the backend
# module, so it is mounted separately and the test that checks it is told where.
DOCKER_GO = docker run --rm -t \
	-u $(UID):$(GID) \
	-v $(ROOT)/backend:/src \
	-v $(ROOT)/api:/api \
	-e ARMATURE_OPENAPI_FILE=/api/openapi.json \
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

DOCKER_NODE = docker run --rm -t \
	-u $(UID):$(GID) \
	-v $(ROOT)/web:/app \
	-v $(NPM_CACHE_VOL):/npmcache \
	-e npm_config_cache=/npmcache \
	-w /app $(NODE_IMAGE)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---------- backend ----------

.PHONY: go
go: ## Run an arbitrary go command in the toolchain container: make go ARGS="version"
	$(DOCKER_GO) go $(ARGS)

.PHONY: tidy
tidy: ## go mod tidy
	$(DOCKER_GO) go mod tidy

.PHONY: build
build: ## Compile all backend binaries
	$(DOCKER_GO) go build ./...

.PHONY: test
test: ## Run backend unit tests
	$(DOCKER_GO) go test ./...

# The integration suite talks to the running compose stack: row level security,
# streaming replication and replay lag are properties of Postgres itself and
# cannot be usefully faked. Bring the stack up with `make up` first.
.PHONY: test-integration
test-integration: ## Run the integration suite against the running stack
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
		-e ARMATURE_S3_ENDPOINT=seaweedfs:8333 -e ARMATURE_S3_BUCKET=armature-test-attachments \
		-e ARMATURE_S3_ACCESS_KEY='$(S3_ACCESS_KEY)' -e ARMATURE_S3_SECRET_KEY='$(S3_SECRET_KEY)' \
		-e ARMATURE_SMTP_ADDR=mailpit:1025 -e ARMATURE_POP3_ADDR=mailpit:1110 -e ARMATURE_POP3_USER=armature -e ARMATURE_POP3_PASSWORD=armature \
		-w /src $(GO_IMAGE) go test -tags integration -count=1 $(TESTFLAGS) ./test/... ; \
		status=$$? ; \
		docker compose -f deploy/docker-compose.yml start worker >/dev/null 2>&1 || true ; \
		exit $$status

# The browser suite drives a real Chromium against the running stack. The image
# already carries both Chromium and puppeteer, so nothing is installed here
# either. The suite is mounted inside the image's own app directory because ES
# module resolution walks up from the importing file and ignores NODE_PATH.
# WORKERS=<n> sets how many browsers run scenarios side by side; ONLY=<words>
# narrows the run to the scenarios whose names contain them.
E2E_IMAGE ?= zenika/alpine-chrome:with-puppeteer

.PHONY: test-e2e
test-e2e: ## Run the browser end-to-end suite against the running stack
	@mkdir -p $(ROOT)/.cache/e2e
	docker run --rm -t \
		-u $(UID):$(GID) \
		--network armature_default \
		-v $(ROOT)/e2e:/usr/src/app/e2e:ro \
		-v $(ROOT)/.cache/e2e:/artifacts \
		-e HOME=/tmp \
		-e E2E_BASE_URL=http://web:5173 \
		-e E2E_GITEA_URL=http://gitea:3000 \
		-e E2E_ARTIFACTS=/artifacts \
		-e E2E_ONLY='$(ONLY)' \
		-e E2E_WORKERS='$(WORKERS)' \
		-w /usr/src/app \
		--entrypoint node $(E2E_IMAGE) /usr/src/app/e2e/suite.mjs

.PHONY: test-all
test-all: test test-integration test-e2e ## Everything: unit, integration and browser
	$(DOCKER_NODE) npm run test

.PHONY: vet
vet: ## go vet
	$(DOCKER_GO) go vet ./...

.PHONY: fmt
fmt: ## gofmt the backend
	$(DOCKER_GO) gofmt -l -w .

# The OpenAPI document is derived from the server's own tables and types; the
# checked in copy at api/openapi.json is what a test compares against and what
# the TypeScript client is generated from.
.PHONY: openapi
openapi: ## Regenerate api/openapi.json and the TypeScript client types from it
	$(DOCKER_GO) go run ./cmd/openapi /api/openapi.json
	docker run --rm -t -u $(UID):$(GID) \
		-v $(ROOT)/web:/app -v $(ROOT)/api:/api \
		-v $(NPM_CACHE_VOL):/npmcache -e npm_config_cache=/npmcache \
		-w /app $(NODE_IMAGE) npx openapi-typescript /api/openapi.json -o src/api/schema.d.ts

.PHONY: shell
shell: ## Interactive shell in the Go toolchain container
	docker run --rm -it -u $(UID):$(GID) \
		-v $(ROOT)/backend:/src -v $(GO_CACHE_VOL):/gocache -v $(GO_MOD_VOL):/gomod \
		-e GOCACHE=/gocache -e GOMODCACHE=/gomod -e GOFLAGS=-buildvcs=false \
		-w /src $(GO_IMAGE) sh

## ---------- frontend ----------

.PHONY: npm
npm: ## Run an arbitrary npm command: make npm ARGS="install"
	$(DOCKER_NODE) npm $(ARGS)

.PHONY: web-install
web-install: ## Install frontend dependencies
	$(DOCKER_NODE) npm install

.PHONY: web-build
web-build: ## Production build of the SPA
	$(DOCKER_NODE) npm run build

## ---------- stack ----------

.PHONY: up
up: ## Start the full stack (postgres primary + replica, redis, gitea, api, worker, web)
	docker compose -f deploy/docker-compose.yml up -d --build

.PHONY: down
down: ## Stop the stack
	docker compose -f deploy/docker-compose.yml down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose -f deploy/docker-compose.yml down -v

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
seed: ## Load the demo organization into the running stack
	docker compose -f deploy/docker-compose.yml run --rm --build seed

.PHONY: psql
psql: ## Open psql against the primary
	docker compose -f deploy/docker-compose.yml exec postgres-primary psql -U armature -d armature

.PHONY: screenshots
screenshots: ## Refresh the pictures in docs/ from the seeded demo, through the browser runner
	@mkdir -p $(ROOT)/.cache/e2e/shots
	docker run --rm -t \
		-u $(UID):$(GID) \
		--network armature_default \
		-v $(ROOT)/e2e:/usr/src/app/e2e:ro \
		-v $(ROOT)/.cache/e2e:/artifacts \
		-e HOME=/tmp \
		-e E2E_BASE_URL=http://web:5173 \
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
		--set mail.smtpAddr=smtp.example.com:587 --set mail.inbox=desk@example.com \
		--set database.replicaHosts={replica.example.com} --set externalRedis.auth=true > $(GOLDEN)/everything.yaml
	$(DOCKER_HELM) template armature $(CHART) $(CNPG_ARGS) > $(GOLDEN)/cnpg.yaml

.PHONY: helm-check
helm-check: ## Fail if the rendered chart differs from the committed golden files
	@$(MAKE) --no-print-directory helm-template
	@git diff --quiet --exit-code -- $(GOLDEN) \
		&& echo "  chart renders as committed" \
		|| { echo "  the rendered chart differs from $(GOLDEN); read the diff and commit it if it is right"; git --no-pager diff --stat -- $(GOLDEN); exit 1; }
