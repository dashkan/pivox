// Aspire TypeScript AppHost
// For more information, see: https://aspire.dev

import {
  createBuilder,
  EndpointProperty,
  refExpr,
} from "./.aspire/modules/aspire.mjs";

const builder = await createBuilder();

const pgUsername = await builder.addParameter("postgres-username", {
  secret: true,
});
const pgPassword = await builder.addParameter("postgres-password", {
  secret: true,
});

const postgres = await builder
  .addPostgres("postgres")
  // Aspire's default image is plain `postgres`, which has no `vector` type.
  // Pin the same pgvector image the repo's test stack uses so
  // `CREATE EXTENSION vector` (000001_init.up.sql) and pgx's RegisterTypes
  // hook (cmd/pivox-cloud/main.go) both work.
  .withImage("pgvector/pgvector", { tag: "pg18" })
  .withUserName(pgUsername)
  .withPassword(pgPassword)
  // PG18 images store data in a major-version subdir and expect the mount at
  // `/var/lib/postgresql` (the parent), not `/var/lib/postgresql/data`. Aspire's
  // withDataBindMount auto-detects the path from the image tag, but the pgvector
  // tag `pg18` doesn't parse as "18", so it falls back to the PG17 path. Mount
  // explicitly to the PG18 path — matches docker-compose.test.yml.
  .withBindMount("./.data/pg", "/var/lib/postgresql");

const db = postgres.addDatabase("pivox", {
  databaseName: "pivox",
});

const pgEndpoint = postgres.getEndpoint("tcp");
const pgHost = await pgEndpoint.property(EndpointProperty.Host);
const pgPort = await pgEndpoint.property(EndpointProperty.Port);

// pgx (pgxpool.ParseConfig) wants a libpq URL, not Aspire's Npgsql keyword
// string. Build the postgres:// URL from the server endpoint + the parameters.
const pivoxDatabaseUrl = refExpr`postgres://${pgUsername}:${pgPassword}@${pgHost}:${pgPort}/pivox?sslmode=disable`;

// One-shot migration step. Shells out to the repo's golang-migrate CLI; the DB
// URL is a deferred expression (dynamic port) so it's injected as an env var
// and expanded by `sh -c` at runtime. workingDirectory ".." = repo root.
// `migrate up` is idempotent, so it's safe on every start.
const dbMigrate = await builder
  .addExecutable("db-migrate", "sh", "..", [
    "-c",
    'migrate -path internal/db/migrations -database "$PIVOX_DATABASE_URL" up',
  ])
  .withEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .waitFor(db);

// First-use-only seed. seed.sql is not idempotent (straight INSERTs), so guard
// it: only seed when the `organizations` table is empty. Runs after migrations.
const dbSeed = await builder
  .addExecutable("db-seed", "sh", "..", [
    "-c",
    [
      'count=$(psql -tAqc "SELECT count(*) FROM organizations" "$PIVOX_DATABASE_URL")',
      'if [ "$count" = "0" ]; then',
      '  psql -v ON_ERROR_STOP=1 "$PIVOX_DATABASE_URL" -f scripts/seed.sql',
      "else",
      '  echo "db already seeded ($count orgs), skipping"',
      "fi",
    ].join("\n"),
  ])
  .withEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .waitForCompletion(dbMigrate);

// --- otel-collector (CommunityToolkit) ---
// Receives OTLP and forwards to the Aspire dashboard — crucially it handles the
// dashboard's dynamic endpoint + rotating API key for us (the part the TS
// AppHost can't reach directly). forceNonSecureReceiver: plaintext receiver so
// the rustfs + envoy CONTAINERS can push spans without TLS/keys. A fixed
// container-network alias lets them target it at otel-collector:4317 (gRPC) /
// :4318 (HTTP). Declared early so resources that export to it can waitFor it.
const otelCollector = await builder
  .addOpenTelemetryCollector("otel-collector", {
    configureSettings: async (settings) => {
      await settings.forceNonSecureReceiver.set(true);
      // The toolkit defaults to ghcr's contrib image, which has no `latest`
      // tag (pull is denied). Point at the Docker Hub contrib image instead,
      // pinned to a concrete version for reproducibility (a re-pulled `latest`
      // can ship a config-schema change that fails collector startup — and
      // envoy waitFor()s the collector, so that would wedge the ingress).
      await settings.registry.set("docker.io");
      await settings.image.set("otel/opentelemetry-collector-contrib");
      await settings.collectorTag.set("0.154.0");
    },
  })
  // The toolkit ships no default pipeline ("no receiver/pipeline" without
  // this) — provide receivers + the forward-to-dashboard exporter.
  .withConfig("../configs/otel-collector.yaml")
  // The toolkit's injected ASPIRE_ENDPOINT is aspire.dev.localhost -> the
  // container's own loopback (connection refused). withOtlpExporter injects
  // OTEL_EXPORTER_OTLP_ENDPOINT = the container-reachable dashboard address;
  // the config uses that for the endpoint, ASPIRE_API_KEY for the header.
  .withOtlpExporter()
  .withContainerNetworkAlias("otel-collector");

// --- rustfs (S3 storage backend) ---
// Pinned to host :9000 with rustfsadmin/rustfsadmin to match the dev seed
// (scripts/seeds/10_storage_gateways.sql endpoint_uri http://localhost:9000).
// Note: the seeded buckets (pivox-dev, meridian-*, ...) are NOT auto-created;
// the storage agent errors on a missing bucket. Create them on first run.
await builder
  .addContainer("rustfs", "rustfs/rustfs:latest")
  .withEnvironment("RUSTFS_ROOT_USER", "rustfsadmin")
  .withEnvironment("RUSTFS_ROOT_PASSWORD", "rustfsadmin")
  .withArgs(["server", "/data"])
  .withBindMount("./.data/rustfs", "/data")
  .withHttpEndpoint({ name: "s3", port: 9000, targetPort: 9000 })
  // Management/admin console.
  .withHttpEndpoint({ name: "console", port: 9001, targetPort: 9001 })
  // RustFS observability (traces + metrics + logs). RustFS reads ONLY its own
  // RUSTFS_OBS_* env — NOT the standard OTEL_EXPORTER_OTLP_ENDPOINT — so
  // withOtlpExporter() was a no-op (rustfs exported nothing; "resource not
  // found" in the dashboard). Per crates/obs/src/config.rs, the root endpoint
  // is OTLP/HTTP (port 4318). Point it at the otel-collector's HTTP receiver,
  // which forwards to the dashboard (handling the rotating api key); the
  // collector's container alias makes otel-collector:4318 resolvable, and its
  // forceNonSecureReceiver accepts plaintext — so no key/TLS needed on this hop.
  .withEnvironment("RUSTFS_OBS_ENDPOINT", "http://otel-collector:4318")
  // Dev-only: RustFS's S3/object-path spans in crates/ecstore are
  // `#[tracing::instrument(level="debug")]`, and the OTLP trace layer is gated
  // by the EnvFilter built from this level — at the default `warn` rustfs
  // exports no spans. `debug` un-gates full server-side distributed tracing
  // (agent HTTP GET -> rustfs ec/disk internals). Noisy by design; fine for the
  // local dev loop. Drop this (back to default `warn`) for any prod-like run.
  .withEnvironment("RUSTFS_OBS_LOGGER_LEVEL", "debug")
  // Start after the collector so otel-collector:4318 resolves on first export
  // (same explicit dependency envoy has) — no dropped spans in the cold-start window.
  .waitFor(otelCollector);

// --- keycloak (dev IDP) ---
// Pinned to host :8082 to match envoy's keycloak cluster. Data persisted.
// Served at ROOT (no KC_HTTP_RELATIVE_PATH): envoy proxies /realms/ and
// /resources/ to it, and the admin console is reached directly via the Aspire
// proxy — so keycloak never needs a base-path prefix. Serving at root also
// keeps the integration's built-in health probe (root OIDC discovery) green.
// KC_PROXY_HEADERS is load-bearing: envoy sets x-forwarded-proto=https, and
// keycloak only trusts it (+ Host) to build public https issuer/token URLs when
// this is set; otherwise discovery advertises non-https and the broker's
// requireSecureIssuer rejects it. No KC_HOSTNAME — derived from forwarded host.
await builder
  .addKeycloak("keycloak", { port: 8082 })
  .withEnvironment("KC_PROXY_HEADERS", "xforwarded")
  .withEnvironment("KC_HOSTNAME_STRICT", "false")
  .withEnvironment("KC_HTTP_ENABLED", "true")
  // Imports the `acme` realm (exported from the docker-compose keycloak) on
  // startup. The integration mounts this dir at /opt/keycloak/data/import and
  // runs --import-realm. Realms already present in the persisted data are
  // skipped, so this is a no-op once acme exists in ./.data/keycloak.
  .withRealmImport("../configs/keycloak")
  .withDataBindMount("./.data/keycloak");

// --- api (pivox-cloud) — host process, binds its own ports ---
// Override the service gRPC listener to all-interfaces. The default
// 127.0.0.1:50052 is loopback-only and unreachable from the envoy CONTAINER
// via host.docker.internal (which routes to the host gateway, not loopback).
await builder
  .addGoApp("api", "../cmd/pivox-cloud")
  .withReference(db)
  .withEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .withEnvironment("PIVOX_SERVICE_GRPC_PORT", ":50052")
  // Don't boot until migrations finished (the `vector` type must exist before
  // pgx's RegisterTypes runs). Waiting on the seed too gives a fully ready DB.
  .waitForCompletion(dbSeed);

// --- worker (pivox-worker) — host process, River-backed periodic jobs ---
// Mirrors the `make air-worker` leg of the dev target. No envoy-facing port;
// it only needs the DB. River runs its own (idempotent) migrations on start.
await builder
  .addGoApp("worker", "../cmd/pivox-worker")
  .withReference(db)
  .withEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .waitForCompletion(dbSeed);

// --- agent (pivox-agent) — on-prem storage agent; host process ---
// Mirrors `make run-agent`. The `storage` subcommand is positional. Dev
// config is pinned here (moving config into the AppHost over direnv):
//   - PIVOX_TOKEN: the seeded `local` gateway's registration token
//     (scripts/seeds/11_local_corp.sql).
//   - PIVOX_PORT: envoy's storage_agent cluster targets :8083 (agent
//     default is 443).
// PIVOX_CLOUD_HOST / PIVOX_PLAINTEXT still come from direnv (the agent
// connects to the cloud AgentService over that). Serves /files/.
// waitForCompletion(dbSeed) so its gateway/token row exists first.
await builder
  .addGoApp("agent", "../cmd/pivox-agent")
  .withArgs(["storage"])
  .withEnvironment("PIVOX_TOKEN", "dev-token-local")
  .withEnvironment("PIVOX_PORT", "8083")
  .waitForCompletion(dbSeed);

// --- web libraries build (mirrors the `web-build` prereq of `make dev`) ---
// The start app imports from @pivox/* workspace packages at vite config-LOAD
// time, so their dist/ must exist before the dev server boots. One-shot; the
// watcher below handles incremental rebuilds after. Assumes deps are already
// installed (same as the Makefile) — run `pnpm -C web install` if not.
const webBuild = await builder.addExecutable("web-build", "pnpm", "../web", [
  "run",
  "web:build",
]);

// --- web libraries watcher (the `packages` leg of `make dev`) ---
await builder
  .addExecutable("web-build-watch", "pnpm", "../web", ["run", "web:build:watch"])
  .waitForCompletion(webBuild);

// --- start (TanStack Start / Vite dev server) — the `start` leg of dev ---
// Pinned to :3000 to match envoy's web_app cluster. The dev server binds
// 127.0.0.1:3000 (its default) and the envoy container reaches it via
// host.docker.internal, which resolves to the host loopback on Docker Desktop.
// pnpm is the workspace package manager; install is off (web-build needs deps).
await builder
  .addViteApp("start", "../web/apps/start", { runScriptName: "dev" })
  .withPnpm({ install: false })
  // isProxied:false → the dev server binds :3000 directly (no DCP proxy). A
  // non-container resource can't be proxied when port === targetPort, and we
  // want vite on the real :3000 so envoy's web_app cluster reaches it.
  .withHttpEndpoint({ name: "http", port: 3000, targetPort: 3000, isProxied: false })
  .waitForCompletion(webBuild);

// --- envoy (L7 ingress) ---
// Uses the Aspire-specific config (clusters -> host.docker.internal). Mounts
// the gitignored proto descriptor for grpc_json_transcoder. Pinned to host
// :8081 so ngrok can reach it. Its OTel tracer exports to the collector above
// (otel-collector:4317). TODO: verify/bump the envoy image tag.
const envoy = await builder
  .addContainer("envoy", "envoyproxy/envoy:v1.31-latest")
  .withBindMount("../configs/envoy.aspire.yaml", "/etc/envoy/envoy.yaml", {
    isReadOnly: true,
  })
  .withBindMount("../configs/pivox.pb", "/etc/envoy/pivox.pb", {
    isReadOnly: true,
  })
  .withArgs(["-c", "/etc/envoy/envoy.yaml"])
  .withHttpEndpoint({ name: "ingress", port: 8081, targetPort: 8081 })
  // Start after the collector so otel-collector:4317 resolves on first export.
  .waitFor(otelCollector);

// --- ngrok (public tunnel -> pivox.ngrok.app) ---
// Tunnels to envoy via host.docker.internal:8081. NGROK_AUTHTOKEN comes from
// your direnv .envrc — the apphost process inherits it, but containers don't,
// so forward it explicitly. Stop `make proxy-ngrok` first: ngrok allows one
// agent session per reserved domain.
await builder
  .addContainer("ngrok", "ngrok/ngrok:latest")
  .withEnvironment("NGROK_AUTHTOKEN", process.env.NGROK_AUTHTOKEN ?? "")
  .withArgs(["http", "host.docker.internal:8081", "--domain", "pivox.ngrok.app"])
  .waitFor(envoy);

await builder.build().run();
