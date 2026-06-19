# syntax=docker/dockerfile:1

FROM node:20-bookworm-slim AS frontend

WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.23.0 --activate

COPY frontend/package.json frontend/pnpm-lock.yaml ./frontend/
RUN pnpm --dir frontend install --frozen-lockfile

COPY frontend ./frontend
RUN mkdir -p internal/web && pnpm --dir frontend build

FROM golang:1.23-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /src/internal/web/dist ./internal/web/dist

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
RUN if [ -n "$TARGETARCH" ]; then export GOARCH="$TARGETARCH"; fi; \
    CGO_ENABLED=0 GOOS="$TARGETOS" go build -trimpath -ldflags "-s -w -X main.BuildID=$VERSION" -o /out/logthing ./cmd/server

FROM debian:bookworm-slim

RUN useradd --uid 10001 --gid users --home-dir /nonexistent --shell /usr/sbin/nologin --no-create-home logthing \
    && mkdir -p /data/messages \
    && chown -R logthing:users /data

COPY --from=builder /out/logthing /usr/local/bin/logthing

ENV LOGTHING_HTTP_ADDR=:8080 \
    LOGTHING_SYSLOG_UDP_ADDR=:5514 \
    LOGTHING_SYSLOG_TCP_ADDR=:5514 \
    LOGTHING_SYSLOG_FORMAT=automatic \
    LOGTHING_DATA_DIR=/data/messages

VOLUME ["/data"]
EXPOSE 8080
EXPOSE 5514/tcp
EXPOSE 5514/udp

USER logthing
ENTRYPOINT ["/usr/local/bin/logthing"]
