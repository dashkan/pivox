# syntax=docker/dockerfile:1
# Dev Postgres image: pgvector + golang-migrate baked in, so the DB init
# script (docker-entrypoint-initdb.d) can run REAL migrations instead of raw
# *.up.sql. migrate is pinned to match tools/go.mod.
ARG PG_TAG=pg18
FROM pgvector/pgvector:${PG_TAG}

ARG MIGRATE_VERSION=4.19.1
# TARGETARCH is provided by buildkit (arm64 | amd64) — we only support those.
ARG TARGETARCH
ADD https://github.com/golang-migrate/migrate/releases/download/v${MIGRATE_VERSION}/migrate.linux-${TARGETARCH}.deb /tmp/migrate.deb
RUN dpkg -i /tmp/migrate.deb && rm /tmp/migrate.deb && migrate -version
