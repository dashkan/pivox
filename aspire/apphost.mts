// Aspire TypeScript AppHost
// For more information, see: https://aspire.dev

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  createBuilder,
  EndpointProperty,
  refExpr,
} from "./.aspire/modules/aspire.mjs";

function isTruthy(value: string | undefined): boolean {
  return value != null && (value.toLowerCase() === "true" || value === "1");
}

// Generate the effective envoy config and return its mount-relative path. The
// OTel tracing block in configs/envoy.aspire.yaml (between the BEGIN/END
// envoy-otel-tracing markers) is STRIPPED unless ASPIRE_OTEL_ENVOY_ENABLED is
// set. envoy's static YAML can't read env, so the apphost does it — letting you
// toggle envoy's (noisy) per-request ingress spans via direnv. Output goes to
// the gitignored .data dir and is regenerated on every `aspire start`.
function writeEffectiveEnvoyConfig(): string {
  const here = import.meta.dirname;
  let yaml = readFileSync(join(here, "../configs/envoy.aspire.yaml"), "utf8");
  if (!isTruthy(process.env.ASPIRE_OTEL_ENVOY_ENABLED)) {
    yaml = yaml.replace(
      /^[ \t]*# BEGIN envoy-otel-tracing\n[\s\S]*?# END envoy-otel-tracing[ \t]*\r?\n/m,
      "",
    );
  }
  mkdirSync(join(here, ".data"), { recursive: true });
  writeFileSync(join(here, ".data/envoy.effective.yaml"), yaml);
  return "./.data/envoy.effective.yaml";
}

const builder = await createBuilder();

const pgUsername = await builder.addParameter("postgres-username", {
  secret: true,
});
const pgPassword = await builder.addParameter("postgres-password", {
  secret: true,
});

const postgres = await builder
  // Pin the host port so you can connect from the host PC (psql / a GUI) at
  // localhost:5432 — the `pivox` and `keycloak` databases. Change this if a
  // local Postgres (or the docker-compose test stack) already owns 5432.
  .addPostgres("postgres", { port: 5432 })
  // Custom image: pgvector + golang-migrate baked in (aspire/pg/Dockerfile) so
  // the DB init script runs real migrations. pgvector gives the `vector` type
  // for CREATE EXTENSION vector (000001_init.up.sql) + pgx RegisterTypes.
  .withDockerfile("pg")
  .withUserName(pgUsername)
  .withPassword(pgPassword)
  // PG18 images store data in a major-version subdir and expect the mount at
  // `/var/lib/postgresql` (the parent), not `/var/lib/postgresql/data`. Aspire's
  // withDataBindMount auto-detects the path from the image tag, but the pgvector
  // tag `pg18` doesn't parse as "18", so it falls back to the PG17 path. Mount
  // explicitly to the PG18 path — matches docker-compose.test.yml.
  .withBindMount("./.data/pg", "/var/lib/postgresql")
  // First-start DB setup: the init script (mounted into the container's
  // docker-entrypoint-initdb.d) creates the pivox + keycloak databases, runs
  // real golang-migrate migrations against pivox, and seeds it. migrations +
  // scripts are bind-mounted (current without an image rebuild); `migrate`
  // itself is baked into the image (aspire/pg/Dockerfile). The whole scripts
  // dir is mounted (not just seed.sql) because seed.sql does
  // `\i scripts/seeds/*.sql` with paths relative to the working dir.
  .withInitFiles("postgres-init")
  .withBindMount("../internal/db/migrations", "/migrations", { isReadOnly: true })
  .withBindMount("../scripts", "/scripts", { isReadOnly: true })
  // Stable alias so other CONTAINERS (keycloak) can reach postgres over the
  // Aspire container network at `postgres:5432` — host.docker.internal only
  // works for host processes.
  .withContainerNetworkAlias("postgres");

const pgEndpoint = postgres.getEndpoint("tcp");
const pgHost = await pgEndpoint.property(EndpointProperty.Host);
const pgPort = await pgEndpoint.property(EndpointProperty.Port);

// pgx (pgxpool.ParseConfig) wants a libpq URL, not Aspire's Npgsql keyword
// string. Build the postgres:// URL from the server endpoint + the parameters.
// The pivox + keycloak databases, the schema migrations, and the seed are all
// created by the pg image's init script (aspire/pg/Dockerfile + initdb/00-init.sh)
// on first start, so consumers just waitFor(postgres) — no init executables.
const pivoxDatabaseUrl = refExpr`postgres://${pgUsername}:${pgPassword}@${pgHost}:${pgPort}/pivox?sslmode=disable`;

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
let rustfs = builder
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
  .withEnvironment("RUSTFS_OBS_ENDPOINT", "http://otel-collector:4318");

// RUSTFS_OBS_LOGGER_LEVEL gates rustfs's server-side spans: its S3/object-path
// spans in crates/ecstore are `#[tracing::instrument(level="debug")]`, and the
// OTLP trace layer is gated by the EnvFilter built from this level — so at the
// default `warn` rustfs exports metrics but NO server spans. Set
// ASPIRE_OTEL_RUSTFS_LOG_LEVEL=debug in direnv to un-gate full server-side
// tracing (agent HTTP GET -> rustfs ec/disk internals); very noisy, so it's
// opt-in and only applied when the env var is set. Never `debug` in prod.
const rustfsLogLevel = process.env.ASPIRE_OTEL_RUSTFS_LOG_LEVEL;
if (rustfsLogLevel) {
  rustfs = rustfs.withEnvironment("RUSTFS_OBS_LOGGER_LEVEL", rustfsLogLevel);
}
// Start after the collector so otel-collector:4318 resolves on first export
// (same explicit dependency envoy has) — no dropped spans in the cold-start window.
await rustfs.waitFor(otelCollector);

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
//
// Keycloak persists in Postgres (not the dev H2 file) so realms/users/clients
// are durable + inspectable via SQL — much easier for adding accounts and for
// the Firebase->KC migration. KC creates its own SCHEMA on boot but NOT the
// database; the `keycloak` db is created by the pg image's init script.
//
// JDBC URL the keycloak CONTAINER uses to reach the postgres CONTAINER. Both
// are containers on the Aspire network, so connect via postgres's network alias
// + its INTERNAL port (5432) — NOT host.docker.internal (host processes only).
// Credentials go in KC_DB_* below.
const keycloakDbUrl = "jdbc:postgresql://postgres:5432/keycloak";

await builder
  .addKeycloak("keycloak", { port: 8082 })
  // Use Postgres (KC_DB) instead of the start-dev default H2; the db is created
  // by the pg init script and KC auto-migrates its schema into it on boot.
  .withEnvironment("KC_DB", "postgres")
  .withEnvironment("KC_DB_URL", keycloakDbUrl)
  .withEnvironment("KC_DB_USERNAME", pgUsername)
  .withEnvironment("KC_DB_PASSWORD", pgPassword)
  // Pin the Keycloak server image. Keeps the running server in lockstep with
  // the theme jar / account-ui library version we build against.
  .withEnvironment("KC_PROXY_HEADERS", "xforwarded")
  .withEnvironment("KC_HOSTNAME_STRICT", "false")
  .withEnvironment("KC_HTTP_ENABLED", "true")
  // No-cache the theme's static assets (login CSS + account-console JS/CSS/fonts)
  // so a `kc:build` shows up on a normal page refresh — no KC bounce, no hard
  // refresh. start-dev already disables theme/template caching (so .ftl +
  // theme.properties hot-reload), but it leaves static assets on a 30-day
  // cache; -1 makes KC send Cache-Control: no-cache instead. Dev-only apphost.
  .withEnvironment("KC_SPI_THEME_STATIC_MAX_AGE", "-1")
  // Imports the `acme` realm (exported from the docker-compose keycloak) on
  // startup. The integration mounts this dir at /opt/keycloak/data/import and
  // runs --import-realm. Realms already present in the persisted data are
  // skipped, so this is a no-op once acme exists in the keycloak database.
  .withRealmImport("../configs/keycloak")
  // Pivox-branded login + account themes (@pivox/keycloak-theme). Mounted
  // read-only into the container's themes dir so a realm can select
  // loginTheme/accountTheme "pivox". Dark mode honors Keycloak's native
  // realm-level "Dark mode" toggle (Realm Settings → Themes).
  //
  // Built by the web-build / web-build-watch executables below (web:build now
  // compiles the login CSS via @pivox/keycloak-theme AND the account-console
  // SPA into this theme dir). start-dev disables theme/template caching and the
  // KC_SPI_THEME_STATIC_MAX_AGE=-1 above no-caches the static assets, so edits
  // picked up by the watcher show on a page refresh — no KC restart.
  .withBindMount(
    "../web/packages/keycloak-theme/theme/pivox",
    "/opt/keycloak/themes/pivox",
    { isReadOnly: true },
  )
  // No withDataBindMount: KC state now lives in the `keycloak` Postgres
  // database (durable via the .data/pg mount), not a local H2 file.
  .waitFor(postgres);

// --- api (pivox-cloud) — host process, binds its own ports ---
// Override the service gRPC listener to all-interfaces. The default
// 127.0.0.1:50052 is loopback-only and unreachable from the envoy CONTAINER
// via host.docker.internal (which routes to the host gateway, not loopback).
await builder
  .addGoApp("api", "../cmd/pivox-cloud")
  .withEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .withEnvironment("PIVOX_SERVICE_GRPC_PORT", ":50052")
  // postgres is healthy only after the init script finishes (migrations + seed
  // run during first-init before it accepts TCP), so the `vector` type exists
  // before pgx's RegisterTypes runs.
  .waitFor(postgres);

// --- worker (pivox-worker) — host process, River-backed periodic jobs ---
// Mirrors the `make air-worker` leg of the dev target. No envoy-facing port;
// it only needs the DB. River runs its own (idempotent) migrations on start.
await builder
  .addGoApp("worker", "../cmd/pivox-worker")
  .withEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .waitFor(postgres);

// --- agent (pivox-agent) — on-prem storage agent; host process ---
// Mirrors `make run-agent`. The `storage` subcommand is positional. Dev
// config is pinned here (moving config into the AppHost over direnv):
//   - PIVOX_TOKEN: the seeded `local` gateway's registration token
//     (scripts/seeds/11_local_corp.sql).
//   - PIVOX_PORT: envoy's storage_agent cluster targets :8083 (agent
//     default is 443).
// PIVOX_CLOUD_HOST / PIVOX_PLAINTEXT still come from direnv (the agent
// connects to the cloud AgentService over that). Serves /files/.
// waitFor(postgres) so the seeded gateway/token row exists first (seed runs in
// the pg init script, which completes before postgres is healthy).
await builder
  .addGoApp("agent", "../cmd/pivox-agent")
  .withArgs(["storage"])
  .withEnvironment("PIVOX_TOKEN", "dev-token-local")
  .withEnvironment("PIVOX_PORT", "8083")
  .waitFor(postgres);

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
  .addExecutable("web-build-watch", "pnpm", "../web", [
    "run",
    "web:build:watch",
  ])
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
  .withHttpEndpoint({
    name: "http",
    port: 3000,
    targetPort: 3000,
    isProxied: false,
  })
  // Browser OpenTelemetry: the app exports spans to this same-origin path,
  // which envoy routes to the otel-collector (-> dashboard). Relative so it
  // resolves against whatever origin serves the app (pivox.ngrok.app).
  .withEnvironment("VITE_OTEL_TRACES_URL", "/v1/traces")
  // Server-side (SSR) OpenTelemetry. --import loads the Node OTel SDK before the
  // Nitro/TanStack server (no server-entry hook in 1.168). It exports to the
  // envoy /v1/traces route on the host (plaintext) instead of the Aspire-injected
  // dashboard OTLP endpoint, because that endpoint is TLS with a dev cert Node
  // won't trust (Go trusts it via the system store; Node's TLS/grpc-js doesn't).
  // envoy forwards to the collector, which handles the dashboard key + TLS.
  .withEnvironment("NODE_OPTIONS", "--import ./instrumentation.node.mjs")
  .withEnvironment(
    "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
    "http://localhost:8081/v1/traces",
  )
  .waitForCompletion(webBuild);

// --- envoy (L7 ingress) ---
// Uses the Aspire-specific config (clusters -> host.docker.internal). Mounts
// the gitignored proto descriptor for grpc_json_transcoder. Pinned to host
// :8081 so ngrok can reach it. Its OTel tracer exports to the collector above
// (otel-collector:4317). TODO: verify/bump the envoy image tag.
// apphost-generated config: tracing block stripped unless ASPIRE_OTEL_ENVOY_ENABLED.
const envoyConfigMount = writeEffectiveEnvoyConfig();
const envoy = await builder
  .addContainer("envoy", "envoyproxy/envoy:v1.31-latest")
  .withBindMount(envoyConfigMount, "/etc/envoy/envoy.yaml", {
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
  .withArgs(["http", "host.docker.internal:8081", "--url", "pivox.ngrok.app"])
  .waitFor(envoy);

await builder.build().run();
