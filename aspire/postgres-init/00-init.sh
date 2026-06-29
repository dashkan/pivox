#!/bin/sh
# Runs ONCE, on a fresh data dir, during first Postgres init. At this point the
# server listens on the unix socket only (not TCP), so everything below connects
# over the socket as the superuser (trust auth during init).
#
# Owns the pivox dev DB setup so the apphost doesn't need init executables:
#   1. ensure the pivox database exists (POSTGRES_DB already creates it; guard)
#   2. apply real golang-migrate migrations to pivox (binary baked into the image)
#   3. seed pivox
# Only pivox is handled here — it's the one DB that needs real migrations + a
# seed, which Aspire's addDatabase can't do. keycloak + sessions are created by
# addDatabase (idempotent CREATE DATABASE on startup): Keycloak builds its own
# schema on boot and the BFF creates `web_sessions` on first use, so neither
# needs anything from this script.
#
# migrations + scripts are bind-mounted by the apphost at /migrations and /scripts.
set -eu

# Ensure pivox exists. Postgres creates POSTGRES_DB (pivox) before running init
# scripts, so an unconditional CREATE would collide — and CREATE DATABASE has no
# IF NOT EXISTS. Guard on pg_database. (keycloak + sessions are created by Aspire
# addDatabase, not here.)
ensure_db() {
	if [ "$(psql -tAqc "SELECT 1 FROM pg_database WHERE datname = '$1'" --username "$POSTGRES_USER")" = "1" ]; then
		echo "database $1 already exists, skipping create"
	else
		psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" -c "CREATE DATABASE $1"
	fi
}
ensure_db pivox

# golang-migrate over the socket (no TCP yet). The postgres lib/pq driver reads
# host=<socket dir>; empty host in the URL + the host param keeps it off TCP.
migrate -path /migrations \
	-database "postgres://${POSTGRES_USER}@/pivox?host=/var/run/postgresql&sslmode=disable" \
	up

# seed.sql does `\i scripts/seeds/*.sql` (paths relative to the working dir), so
# run it from / where the mounted scripts dir lives at /scripts.
cd /
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname pivox -f /scripts/seed.sql
