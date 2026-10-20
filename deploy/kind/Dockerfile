# What the chart install test runs in, so a machine needs only Docker: kind to
# make a cluster out of a container, the Docker client and BuildKit, kubectl and
# helm. The kind node image is pinned in chart-install-test.sh.
FROM golang:1.26-alpine AS kind
ENV GOFLAGS=-buildvcs=false GOTOOLCHAIN=local CGO_ENABLED=0
RUN go install sigs.k8s.io/kind@v0.30.0

FROM alpine/helm:3.19.0 AS helm
FROM alpine/kubectl:1.34.1 AS kubectl

FROM alpine:3.22
RUN apk add --no-cache curl docker-cli docker-cli-buildx jq
COPY --from=kind /go/bin/kind /usr/local/bin/kind
COPY --from=helm /usr/bin/helm /usr/local/bin/helm
COPY --from=kubectl /usr/local/bin/kubectl /usr/local/bin/kubectl
