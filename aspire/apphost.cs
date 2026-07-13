#:package Aspire.Hosting.Go@13.4.6-preview.1.26319.6
#:package Aspire.Hosting.JavaScript@13.4.6
#:package Aspire.Hosting.Kafka@13.4.6
#:package Aspire.Hosting.Keycloak@13.4.6-preview.1.26319.6
#:package Aspire.Hosting.PostgreSQL@13.4.6
#:package CommunityToolkit.Aspire.Hosting.OpenTelemetryCollector@13.4.1-beta.680
#:sdk Aspire.AppHost.Sdk@13.4.6

using System.Runtime.CompilerServices;
using System.Text.RegularExpressions;
using Aspire.Hosting.ApplicationModel;

var builder = DistributedApplication.CreateBuilder(args);

var pgUsername = builder.AddParameter("postgres-username", true);
var pgPassword = builder.AddParameter("postgres-password", true);

var postgres = builder
  // Pin the host port so you can connect from the host PC (psql / a GUI) at
  // localhost:5432 — the `pivox` and `keycloak` databases. Change this if a
  // local Postgres (or the docker-compose test stack) already owns 5432.
  .AddPostgres("postgres", port: 5432)
  // Custom image: pgvector + golang-migrate baked in (aspire/pg/Dockerfile) so
  // the DB init script runs real migrations. pgvector gives the `vector` type
  // for CREATE EXTENSION vector (000001_init.up.sql) + pgx RegisterTypes.
  .WithDockerfile("pg")
  .WithUserName(pgUsername)
  .WithPassword(pgPassword)
  // PG18 images store data in a major-version subdir and expect the mount at
  // `/var/lib/postgresql` (the parent), not `/var/lib/postgresql/data`. Aspire's
  // WithDataBindMount auto-detects the path from the image tag, but the pgvector
  // tag `pg18` doesn't parse as "18", so it falls back to the PG17 path. Mount
  // explicitly to the PG18 path — matches docker-compose.test.yml.
  .WithBindMount("./.data/pg", "/var/lib/postgresql")
  // First-start DB setup: the init script (mounted into the container's
  // docker-entrypoint-initdb.d) creates the pivox + keycloak databases, runs
  // real golang-migrate migrations against pivox, and seeds it. migrations +
  // scripts are bind-mounted (current without an image rebuild); `migrate`
  // itself is baked into the image (aspire/pg/Dockerfile). The whole scripts
  // dir is mounted (not just seed.sql) because seed.sql does
  // `\i scripts/seeds/*.sql` with paths relative to the working dir.
  .WithInitFiles("postgres-init")
  .WithBindMount("../internal/db/migrations", "/migrations", true)
  .WithBindMount("../scripts", "/scripts", true)
  // Forwarded to the first-init seed (postgres-init/00-init.sh): the dev
  // storage-gateway rows build /files/ URLs against the real public host
  // (PIVOX_HOSTNAME, e.g. pivox.app — bare host, no scheme) instead of a
  // hardcoded one, so a fresh checkout works on any dev's tunnel domain.
  // Empty => the seed defaults to localhost:8081.
  .WithEnvironment("PIVOX_HOSTNAME", Environment.GetEnvironmentVariable("PIVOX_HOSTNAME") ?? "")
  // Stable alias so other CONTAINERS (keycloak) can reach postgres over the
  // Aspire container network at `postgres:5432` — host.docker.internal only
  // works for host processes.
  .WithContainerNetworkAlias("postgres");

// Database resources. addDatabase models each DB (dashboard + health + the
// injected connection strings) AND creates it idempotently on first start
// (Aspire subscribes to the server's ResourceReadyEvent and runs
// CREATE DATABASE "<name>", ignoring 42P04 if it already exists):
//   - pivox is ALSO the POSTGRES_DB default, so it exists at initdb time for the
//     init script's golang-migrate + seed (addDatabase's post-startup CREATE is
//     then a harmless no-op for it).
//   - keycloak + sessions are created here — Keycloak builds its own schema on
//     boot and the BFF creates web_sessions on first use, so neither needs the
//     init script.
// River is deliberately NOT its own database: the LRO workers complete River
// jobs in the SAME transaction as the app-data mutation (river.JobCompleteTx +
// the org/space delete + one Commit), which requires River's tables in `pivox`
// (the `river` schema). Postgres has no cross-database transactions.
var pivoxDb = postgres.AddDatabase("pivox-db", "pivox");
// Resource name "keycloak-db", not "keycloak": the addKeycloak server resource
// already owns the name "keycloak" (resource names are unique, case-insensitive).
// databaseName pins the actual database to "keycloak".
var keycloakDb = postgres.AddDatabase("keycloak-db", "keycloak");

var sessionsDb = postgres.AddDatabase("sessions-db", "sessions");


// pgx (pgxpool.ParseConfig) needs a libpq postgres:// URL, not Aspire's Npgsql
// keyword connection string. uriExpression() yields exactly that, correctly
// URL-encoded — so the connection strings are no longer hand-built (the old
// refExpr left the generated password's special chars unencoded: fine for the
// current value, a latent break the day a password contains @ / : ).
var pivoxDatabaseUrl = pivoxDb.Resource.UriExpression;
var sessionsDatabaseUrl = sessionsDb.Resource.UriExpression;


// --- otel-collector (CommunityToolkit) ---
// Receives OTLP and forwards to the Aspire dashboard — crucially it handles the
// dashboard's dynamic endpoint + rotating API key for us (the part the TS
// AppHost can't reach directly). forceNonSecureReceiver: plaintext receiver so
// the rustfs + envoy CONTAINERS can push spans without TLS/keys. A fixed
// container-network alias lets them target it at otel-collector:4317 (gRPC) /
// :4318 (HTTP). Declared early so resources that export to it can waitFor it.
var otelCollector = builder
  .AddOpenTelemetryCollector("otel-collector",
    (settings) =>
    {
      settings.ForceNonSecureReceiver = true;
      // The toolkit defaults to ghcr's contrib image, which has no `latest`
      // tag (pull is denied). Point at the Docker Hub contrib image instead,
      // pinned to a concrete version for reproducibility (a re-pulled `latest`
      // can ship a config-schema change that fails collector startup — and
      // envoy waitFor()s the collector, so that would wedge the ingress).
      settings.Registry = "docker.io";
      settings.Image = "otel/opentelemetry-collector-contrib";
      settings.CollectorTag = "0.154.0";
    })
  // The toolkit ships no default pipeline ("no receiver/pipeline" without
  // this) — provide receivers + the forward-to-dashboard exporter.
  .WithConfig("../configs/otel-collector.yaml")
  // The toolkit's injected ASPIRE_ENDPOINT is aspire.dev.localhost -> the
  // container's own loopback (connection refused). withOtlpExporter injects
  // OTEL_EXPORTER_OTLP_ENDPOINT = the container-reachable dashboard address;
  // the config uses that for the endpoint, ASPIRE_API_KEY for the header.
  .WithOtlpExporter()
  .WithContainerNetworkAlias("otel-collector");

// --- rustfs (S3 storage backend) ---
// Pinned to host :9000 with rustfsadmin/rustfsadmin to match the dev seed
// (scripts/seeds/10_storage_gateways.sql endpoint_uri http://localhost:9000).
// Note: the seeded buckets (pivox-dev, meridian-*, ...) are NOT auto-created;
// the storage agent errors on a missing bucket. Create them on first run.
var rustfs = builder
  .AddContainer("rustfs", "rustfs/rustfs:latest")
  .WithEnvironment("RUSTFS_ROOT_USER", "rustfsadmin")
  .WithEnvironment("RUSTFS_ROOT_PASSWORD", "rustfsadmin")
  .WithArgs(["server", "/data"])
  .WithBindMount("./.data/rustfs", "/data")
  .WithHttpEndpoint(9000, 9000, "s3")
  // Management/admin console.
  .WithHttpEndpoint(9001, 9001, "console")
  // RustFS observability (traces + metrics + logs). RustFS reads ONLY its own
  // RUSTFS_OBS_* env — NOT the standard OTEL_EXPORTER_OTLP_ENDPOINT — so
  // withOtlpExporter() was a no-op (rustfs exported nothing; "resource not
  // found" in the dashboard). Per crates/obs/src/config.rs, the root endpoint
  // is OTLP/HTTP (port 4318). Point it at the otel-collector's HTTP receiver,
  // which forwards to the dashboard (handling the rotating api key); the
  // collector's container alias makes otel-collector:4318 resolvable, and its
  // forceNonSecureReceiver accepts plaintext — so no key/TLS needed on this hop.
  .WithEnvironment("RUSTFS_OBS_ENDPOINT", "http://otel-collector:4318");

// RUSTFS_OBS_LOGGER_LEVEL gates rustfs's server-side spans: its S3/object-path
// spans in crates/ecstore are `#[tracing::instrument(level="debug")]`, and the
// OTLP trace layer is gated by the EnvFilter built from this level — so at the
// default `warn` rustfs exports metrics but NO server spans. Set
// ASPIRE_OTEL_RUSTFS_LOG_LEVEL=debug in direnv to un-gate full server-side
// tracing (agent HTTP GET -> rustfs ec/disk internals); very noisy, so it's
// opt-in and only applied when the env var is set. Never `debug` in prod.
var rustfsLogLevel = Environment.GetEnvironmentVariable("ASPIRE_OTEL_RUSTFS_LOG_LEVEL");
if (!string.IsNullOrWhiteSpace(rustfsLogLevel))
{
  rustfs = rustfs.WithEnvironment("RUSTFS_OBS_LOGGER_LEVEL", rustfsLogLevel);
}
rustfs.WaitFor(otelCollector);

// --- kafka — Keycloak event stream for Pivox identity sync ---
// The keycloak-kafka SPI (baked into the KC image, aspire/keycloak/Dockerfile)
// produces user + admin events here; a Pivox consumer will sync identities/orgs
// from them. KC reaches the broker container-to-container via the network alias.
var kafka = builder
  // Host port pinned so the broker is reachable for validation at
  // localhost:9092 (mirrors postgres on 5432). KC reaches it container-to-
  // container via the network alias below.
  .AddKafka("kafka", 9092)
  // Persist broker data across restarts (KRaft log + topics/messages) — without
  // this a restart wipes all events.
  .WithDataVolume()
  // Kafka UI for validation, ingressed by envoy at /kafka. SERVER_SERVLET_CONTEXT_PATH
  // makes the (Spring Boot) UI serve under /kafka so its asset URLs resolve through
  // the proxy without a rewrite; envoy reaches it via the container alias.
  .WithKafkaUI((ui) =>
  {
    ui.WithContainerNetworkAlias("kafka-ui")
    .WithEnvironment("SERVER_SERVLET_CONTEXT_PATH", "/kafka");
  }
  )
  .WithContainerNetworkAlias("kafka");

var keycloak = builder
  .AddKeycloak("keycloak", 8082)
  // Custom image: stock Keycloak + the Pivox provider SPIs (kafka event listener
  // + select-organization authenticator), baked in via `kc build`. Versions
  // pinned in aspire/keycloak/Dockerfile (KC_VERSION / PIVOX_KC_SPI_VERSION).
  .WithDockerfile("keycloak")
  // Teach Keycloak's JVM to trust the Aspire developer certificate.
  //
  // Load-bearing for SSO: the acme identity provider's backchannel (token, jwks) is
  // an https call Keycloak makes to ITSELF over the dev cert. Without trust it dies
  // with "PKIX path building failed: unable to find valid certification path" — an
  // error that names nothing resembling a truststore.
  //
  // Aspire mounts the cert here and points SSL_CERT_DIR at it, but that is OpenSSL's
  // mechanism and Keycloak is Java: JSSE ignores SSL_CERT_DIR, so Aspire's default
  // container trust is a silent no-op. Keycloak's own KC_TRUSTSTORE_PATHS does not
  // work either — its FileTruststoreProvider (the truststore the identity broker's
  // HTTP client uses) initializes from the JVM cacerts BEFORE TruststoreBuilder reads
  // those paths, and it only ingests certs it classifies as CAs, which the dev cert —
  // a self-signed LEAF — is not. So the cert must be in the JVM truststore itself.
  //
  // cacerts is root-owned and read-only while Keycloak runs as uid 1000, so the image's
  // entrypoint wrapper (aspire/keycloak/entrypoint.sh) copies it, appends every PEM in
  // PIVOX_EXTRA_CA_DIR, and points the JVM at the copy. Copying rather than replacing
  // keeps the ~146 public roots, which is what keeps Google/GitHub brokering working.
  //
  // The literal path (rather than the trust-config callback's CertificateDirectoriesPath)
  // is deliberate: that property resolves to an OpenSSL-style COLON-SEPARATED list of
  // candidate trust dirs, not a single directory.
  .WithContainerCertificatePaths(customCertificatesDestination: "/usr/lib/ssl/aspire")
  .WithEnvironment("PIVOX_EXTRA_CA_DIR", "/usr/lib/ssl/aspire/certs")
  // Build-time PAT (read-only) to clone the private dashkan/pivox-keycloak-spi
  // repo during the image build. Sourced live from .envrc via a dedicated var
  // (PIVOX_KEYCLOAK_SPI_GITHUB_PAT, distinct from the shared GITHUB_PAT); empty
  // string when unset so `aspire:build` still typechecks — the KC image build
  // then fails the clone until it's set. Dev-only: lands in image build layers.
  .WithBuildArg("GITHUB_PAT", Environment.GetEnvironmentVariable("PIVOX_KEYCLOAK_SPI_GITHUB_PAT") ?? "")
  // Use Postgres (KC_DB) instead of the start-dev default H2; the db is created
  // by the pg init script and KC auto-migrates its schema into it on boot.
  // Imports the `acme` realm (exported from the docker-compose keycloak) on
  // startup. The integration mounts this dir at /opt/keycloak/data/import and
  // runs --import-realm. Realms already present in the persisted data are
  // skipped, so this is a no-op once acme exists in the keycloak database.
  .WithRealmImport("../configs/keycloak")
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
  .WithBindMount(
    "../web/packages/keycloak-theme/theme/pivox",
    "/opt/keycloak/themes/pivox",
    true
  )
  .WithEnabledFeatures(["cimd", "opentelemetry-logs", "opentelemetry-metrics", "token-exchange"])
  .WithEnvironment("KC_DB", "postgres")
  .WithEnvironment("KC_DB_URL", keycloakDb.Resource.JdbcConnectionString)
  .WithEnvironment("KC_DB_USERNAME", pgUsername)
  .WithEnvironment("KC_DB_PASSWORD", pgPassword)
  // Pin the Keycloak server image. Keeps the running server in lockstep with
  // the theme jar / account-ui library version we build against.
  .WithEnvironment("KC_PROXY_HEADERS", "xforwarded")
  .WithEnvironment("KC_HOSTNAME_STRICT", "false")
  .WithEnvironment("KC_HTTP_ENABLED", "true")
  // Enable CIMD (Client ID Metadata Documents) so MCP clients (VS Code) can auth
  // via a hosted client-metadata URL instead of DCR. start-dev picks it up at
  // boot; organizations (default-on in 26.x, drives the org picker) stays on.
  .WithEnvironment("KC_METRICS_ENABLED", "true")
  // KC (Quarkus) exports metrics + logs over OTLP/gRPC. Target the otel-collector,
  // NOT the dashboard directly. The raw dashboard endpoint is host-side
  // (https://aspire.dev.localhost:PORT) — inside the KC container aspire.dev.localhost
  // resolves to the container's OWN loopback, so the export fails; and it's TLS with
  // a dev cert Quarkus won't trust. The collector receives plaintext on :4317 (its
  // container-network alias) and forwards to the dashboard with the rotating api key
  // + TLS (traces/metrics/logs pipelines are wired in configs/otel-collector.yaml).
  // No api-key header needed on this hop — forceNonSecureReceiver accepts plaintext.
  .WithEnvironment("KC_TELEMETRY_ENDPOINT", "http://otel-collector:4317")
  .WithEnvironment("KC_TRACING_ENABLED", "true")
  .WithEnvironment("KC_TRACING_JDBC_ENABLED", "false")
  .WithEnvironment("KC_TELEMETRY_METRICS_ENABLED", "true")
  .WithEnvironment("KC_TELEMETRY_LOGS_ENABLED", "true")
  // No-cache the theme's static assets (login CSS + account-console JS/CSS/fonts)
  // so a `kc:build` shows up on a normal page refresh — no KC bounce, no hard
  // refresh. start-dev already disables theme/template caching (so .ftl +
  // theme.properties hot-reload), but it leaves static assets on a 30-day
  // cache; -1 makes KC send Cache-Control: no-cache instead. Dev-only apphost.
  .WithEnvironment("KC_SPI_THEME_STATIC_MAX_AGE", "-1")
  // keycloak-kafka SPI config (read from env by the baked-in provider). The
  // realm must ALSO list `kafka` as an event listener and enable Admin Events
  // with representation — configured in-realm and captured in the realm export.
  // Use the INTERNAL advertised listener (kafka:9093), NOT the host listener on
  // :9092 — that one advertises localhost:9092 (for host-side validation) which
  // is unreachable from inside the KC container. kafka:9093 is what kafka-ui
  // uses too. Topics auto-create on first produce in dev.
  .WithEnvironment("KAFKA_BOOTSTRAP_SERVERS", "kafka:9093")
  .WithEnvironment("KAFKA_CLIENT_ID", "keycloak")
  .WithEnvironment("KAFKA_TOPIC", "keycloak-events")
  .WithEnvironment("KAFKA_ADMIN_TOPIC", "keycloak-admin-events")
  .WithEnvironment(
    "KAFKA_EVENTS",
    "REGISTER,UPDATE_PROFILE,UPDATE_EMAIL,DELETE_ACCOUNT,LOGIN,LOGOUT"
  )
  // Trace the KC->Kafka producer. The SPI maps KAFKA_INTERCEPTOR_CLASS to the
  // producer's interceptor.classes. PivoxTracingProducerInterceptor opens the
  // PRODUCER span + injects W3C traceparent into the record headers, so the
  // worker's identity-sync consume span links back to the originating KC request
  // as one distributed trace. It binds KC's Quarkus-OTel GlobalOpenTelemetry (the
  // api ships on lib/main; the SPI bundles no duplicate copy).
  .WithEnvironment(
    "KAFKA_INTERCEPTOR_CLASS",
    "com.github.snuk87.keycloak.kafka.PivoxTracingProducerInterceptor"
  )
  // The span's reported peer. KC dials the broker over the container network
  // (kafka:9093), but Aspire only knows kafka's HOST endpoint (localhost:9092) —
  // and the dashboard resolves a span's peer by matching server.address:port
  // against a resource's registered endpoints. Reporting the host address is what
  // makes Kafka render as its own node instead of the spans hanging off Keycloak.
  // This is span METADATA only — the producer still connects to kafka:9093. In prod
  // both ends share one broker address and the override is unnecessary (the SPI
  // then falls back to parsing bootstrap.servers).
  .WithEnvironment("KAFKA_SPAN_SERVER_ADDRESS", "localhost")
  .WithEnvironment("KAFKA_SPAN_SERVER_PORT", "9092")
  // Secrets/IDs referenced by the realm-import JSON via ${...} placeholders (the
  // committed realm files carry no plaintext secrets). Forwarded 1:1 from .envrc
  // into the container so KC can resolve them on --import-realm. The IMPORT_
  // prefix avoids KC's own KC_*/KEYCLOAK_* config-option parsing. These names
  // must match the ${...} placeholders in the realm JSONs EXACTLY (all use the
  // _CLIENT_ID / _CLIENT_SECRET form).
  .WithEnvironment("IMPORT_KC_APP_URL", Environment.GetEnvironmentVariable("IMPORT_KC_APP_URL") ?? "")
  // Shared password for every dev user in the committed *-users-*.json baseline.
  // The exports carry only a ${IMPORT_KC_DEV_PASSWORD} placeholder (no hashes —
  // see configs/keycloak/sanitize-realms.sh); KC resolves it here and hashes it on
  // import. Unset => the placeholder resolves to empty and the dev logins fail,
  // which is the loud failure we want rather than a silently unusable realm.
  .WithEnvironment(
    "IMPORT_KC_DEV_PASSWORD",
    Environment.GetEnvironmentVariable("IMPORT_KC_DEV_PASSWORD") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_START_CLIENT_ID",
    Environment.GetEnvironmentVariable("IMPORT_KC_START_CLIENT_ID") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_START_CLIENT_SECRET",
    Environment.GetEnvironmentVariable("IMPORT_KC_START_CLIENT_SECRET") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_IDP_GITHUB_CLIENT_ID",
    Environment.GetEnvironmentVariable("IMPORT_KC_IDP_GITHUB_CLIENT_ID") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_IDP_GITHUB_CLIENT_SECRET",
    Environment.GetEnvironmentVariable("IMPORT_KC_IDP_GITHUB_CLIENT_SECRET") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_IDP_GOOGLE_CLIENT_ID",
    Environment.GetEnvironmentVariable("IMPORT_KC_IDP_GOOGLE_CLIENT_ID") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_IDP_GOOGLE_CLIENT_SECRET",
    Environment.GetEnvironmentVariable("IMPORT_KC_IDP_GOOGLE_CLIENT_SECRET") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_IDP_OIDC_ACME_CLIENT_ID",
    Environment.GetEnvironmentVariable("IMPORT_KC_IDP_OIDC_ACME_CLIENT_ID") ?? ""
  )
  .WithEnvironment(
    "IMPORT_KC_IDP_OIDC_ACME_CLIENT_SECRET",
    Environment.GetEnvironmentVariable("IMPORT_KC_IDP_OIDC_ACME_CLIENT_SECRET") ?? ""
  )
  // No withDataBindMount: KC state now lives in the `keycloak` Postgres
  // database (durable via the .data/pg mount), not a local H2 file.
  // waitFor(keycloakDb): the addDatabase resource is ready only after postgres
  // is healthy AND its CREATE DATABASE has run, so the `keycloak` DB exists
  // before KC boots and builds its schema. KC_DB_URL stays the hand-built
  // container JDBC (postgres:5432) — container-to-container, not the host URI.
  .WaitFor(keycloakDb)
  .WaitFor(kafka)
  // KC exports metrics + logs to the collector (KC_TELEMETRY_*_ENDPOINT above).
  .WaitFor(otelCollector);

// --- api (pivox-cloud) — host process, binds its own ports ---
// Override the service gRPC listener to all-interfaces. The default
// 127.0.0.1:50052 is loopback-only and unreachable from the envoy CONTAINER
// via host.docker.internal (which routes to the host gateway, not loopback).
var api = builder
  .AddGoApp("api", "../cmd/pivox-cloud")
  .WithEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  .WithEnvironment("PIVOX_SERVICE_GRPC_PORT", ":50052")
  // waitFor(pivoxDb): the DB resource is ready only after postgres is healthy,
  // which on first init is after the init script's migrate + seed — so the
  // schema + the `vector` type exist before pgx's RegisterTypes runs.
  .WaitFor(pivoxDb)
  // waitFor(keycloak): the OIDC verifier fetches the realm JWKS at startup. If
  // the API boots before Keycloak is serving, that fetch fails and (with no
  // on-demand refresh) every token 401s until the next background refresh. Wait
  // for KC to be healthy so the startup JWKS load succeeds.
  .WaitFor(keycloak);

// --- worker (pivox-worker) — host process, River-backed periodic jobs ---
// Mirrors the `make air-worker` leg of the dev target. No envoy-facing port;
// it only needs the DB. River runs its own (idempotent) migrations on start.
builder
  .AddGoApp("worker", "../cmd/pivox-worker")
  .WithEnvironment("PIVOX_DATABASE_URL", pivoxDatabaseUrl)
  // The purge_web_sessions periodic job GCs expired rows from the BFF-owned
  // `web_sessions` table, which lives in the separate `sessions` DB — so the
  // worker needs that connection in addition to the app DB.
  .WithEnvironment("PIVOX_SESSIONS_DATABASE_URL", sessionsDatabaseUrl)
  // identity-sync consumer reads the keycloak-events topic to provision
  // `identities` rows (the identity-sync hook / KC event-sync). The
  // worker is a host process, so it reaches the kafka container on the host
  // listener (:9092), not the internal kafka:9093 advertised to containers.
  .WithEnvironment("PIVOX_KAFKA_BROKERS", "localhost:9092")
  .WaitFor(pivoxDb)
  .WaitFor(sessionsDb)
  .WaitFor(kafka);

// --- agent (pivox-agent) — on-prem storage agent; host process ---
// Mirrors `make run-agent`. The `storage` subcommand is positional. Dev
// config is pinned here (moving config into the AppHost over direnv):
//   - PIVOX_TOKEN: the seeded `local` gateway's registration token
//     (scripts/seeds/11_local_corp.sql).
//   - PIVOX_PORT: envoy's storage_agent cluster targets :8083 (agent
//     default is 443).
// PIVOX_CLOUD_HOST / PIVOX_PLAINTEXT still come from direnv (the agent
// connects to the cloud AgentService over that). Serves /files/.
//
// waitFor(api), NOT the database: the agent is an ON-PREM component that talks to
// the Cloud Controller and has no knowledge of Postgres — modelling it as a
// dependent of the DB puts a component's private storage in another component's
// dependency graph. It waited on pivox-db as a stand-in for "the seed has run"
// (PIVOX_TOKEN is the `local` gateway's registration token from
// scripts/seeds/11_local_corp.sql), but api already waits for pivox-db, so waiting
// on api covers the seed transitively AND expresses the real edge: the agent's
// first action is to open a control stream to the API.
// PIVOX_AGENT_STATE_DIR / PIVOX_AGENT_CACHE_DIR come from direnv, not from here.
// The agent's defaults (/var/lib/pivox/*) are correct for a real Linux install and
// wrong for a host process running as your own user — it cannot create them and
// degrades to in-memory-only ("no crash resilience"), losing sessions, denied
// patterns and endpoints on every restart. Point them somewhere writable in .envrc.
// Keep them SIBLINGS, not nested: cache cleanup must never be able to delete state
// (see the --state-dir flag doc in cmd/pivox-agent/storage.go).
builder
  .AddGoApp("agent", "../cmd/pivox-agent")
  .WithArgs(["storage"])
  .WithEnvironment("PIVOX_TOKEN", "dev-token-local")
  .WithEnvironment("PIVOX_PORT", "8083")
  .WaitFor(api);

// --- web libraries build (mirrors the `web-build` prereq of `make dev`) ---
// The start app imports from @pivox/* workspace packages at vite config-LOAD
// time, so their dist/ must exist before the dev server boots. One-shot; the
// watcher below handles incremental rebuilds after. Assumes deps are already
// installed (same as the Makefile) — run `pnpm -C web install` if not.
var webBuild = builder.AddExecutable("web-build", "pnpm", "../web", [
  "run",
  "web:build",
]);

// --- web libraries watcher (the `packages` leg of `make dev`) ---
builder
  .AddExecutable("web-build-watch", "pnpm", "../web", [
    "run",
    "web:build:watch",
  ])
  .WaitForCompletion(webBuild);


// --- start (TanStack Start / Vite dev server) — the `start` leg of dev ---
// Pinned to :3000 to match envoy's web_app cluster. The dev server binds
// 127.0.0.1:3000 (its default) and the envoy container reaches it via
// host.docker.internal, which resolves to the host loopback on Docker Desktop.
// pnpm is the workspace package manager; install is off (web-build needs deps).
builder
  .AddViteApp("start", "../web/apps/start", "dev")
  .WithPnpm(false)
  // isProxied:false → the dev server binds :3000 directly (no DCP proxy). A
  // non-container resource can't be proxied when port === targetPort, and we
  // want vite on the real :3000 so envoy's web_app cluster reaches it.
  .WithHttpEndpoint(3000, 3000, "http", isProxied: false)
  // The BFF stores OIDC sessions server-side in Postgres (web_sessions),
  // reading one per request. That table lives in the BFF-owned `sessions` DB —
  // NOT the app `pivox` DB — so the BFF only needs the sessions connection. The
  // start app is a host process and could inherit this from direnv, but the URL
  // is built here from the Postgres endpoint (not in .envrc), so forward it
  // explicitly.
  .WithEnvironment("PIVOX_SESSIONS_DATABASE_URL", sessionsDatabaseUrl)
  // Browser OpenTelemetry: the app exports spans to this same-origin path,
  // which envoy routes to the otel-collector (-> dashboard). Relative so it
  // resolves against whatever origin serves the app (the tunnel host).
  .WithEnvironment("VITE_OTEL_TRACES_URL", "/v1/traces")
  // Server-side (SSR) OpenTelemetry. --import loads the Node OTel SDK before the
  // Nitro/TanStack server (no server-entry hook in 1.168). It exports to the
  // envoy /v1/traces route on the host (plaintext) instead of the Aspire-injected
  // dashboard OTLP endpoint, because that endpoint is TLS with a dev cert Node
  // won't trust (Go trusts it via the system store; Node's TLS/grpc-js doesn't).
  // envoy forwards to the collector, which handles the dashboard key + TLS.
  .WithEnvironment("NODE_OPTIONS", "--import ./instrumentation.node.mjs")
  .WithEnvironment(
    "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
    "http://localhost:8081/v1/traces"
  )
  // The BFF reads the sessions DB per request; ensure it exists before boot.
  .WaitFor(sessionsDb)
  .WaitForCompletion(webBuild);


// --- envoy (L7 ingress) ---
// Uses the Aspire-specific config (clusters -> host.docker.internal). Mounts
// the gitignored proto descriptor for grpc_json_transcoder. Its OTel tracer
// exports to the collector above (otel-collector:4317).
// apphost-generated config: tracing block stripped unless ASPIRE_OTEL_ENVOY_ENABLED.
//
// TWO listeners, same routes (see configs/envoy.aspire.yaml):
//   :8081 http  — the Cloudflare Tunnel's origin. Machine-facing.
//   :8443 https — the LOCAL origin, TLS-terminated with the Aspire dev cert.
// The :8443 listener is what lets the whole stack run with no public host, no DNS
// and no tunnel, while every "we're behind an https ingress" assumption baked into
// the app stays true.
var envoyConfigMount = WriteEffectiveEnvoyConfig();

#pragma warning disable ASPIRECERTIFICATES001 // HTTPS certificate APIs are experimental
var envoy = builder
  .AddContainer("envoy", "envoyproxy/envoy:v1.31-latest")
  // Mount the Aspire developer certificate into the container. Aspire names the
  // files after the cert thumbprint and hands us their paths as ReferenceExpressions
  // (resolved at container start, NOT strings we can bake into a file), and envoy's
  // static config cannot read env vars — so the paths arrive as env and the start
  // command below symlinks them to the fixed paths the config names.
  .WithHttpsCertificateConfiguration(ctx =>
  {
    ctx.EnvironmentVariables["PIVOX_TLS_CERT"] = ctx.CertificatePath;
    ctx.EnvironmentVariables["PIVOX_TLS_KEY"] = ctx.KeyPath;
    return Task.CompletedTask;
  })
  .WithBindMount(envoyConfigMount, "/etc/envoy/envoy.yaml", true)
  .WithBindMount("../configs/pivox.pb", "/etc/envoy/pivox.pb", true)
  // Scalar API reference: envoy serves the OpenAPI v3 spec + the Scalar HTML
  // page inline (direct_response) at /api-docs. In the container those file
  // bodies are read from absolute /etc/envoy/openapi/* paths — see the
  // /api-docs routes in configs/envoy.aspire.yaml.
  .WithBindMount("../api/openapi/v3", "/etc/envoy/openapi", true)
  // Symlink the thumbprint-named cert/key to the stable paths the envoy config
  // names, then exec envoy. `set -e` so a missing cert fails the container loudly
  // instead of starting an ingress whose TLS listener silently never binds.
  // (The image's entrypoint execs its args verbatim when they don't start with `-`.)
  .WithArgs([
    "sh", "-c",
    "set -e; ln -sf \"$PIVOX_TLS_CERT\" /tmp/tls.crt; ln -sf \"$PIVOX_TLS_KEY\" /tmp/tls.key; "
      + "exec envoy -c /etc/envoy/envoy.yaml",
  ])
  .WithHttpEndpoint(port: 8081, targetPort: 8081, name: "http")
  // The local origin. 8443 must match Keycloak's INTERNAL https port — see the
  // ingress_8443 comment in configs/envoy.aspire.yaml; renumbering it silently
  // breaks the SSO issuer match.
  .WithHttpsEndpoint(port: 8443, targetPort: 8443, name: "https")
  .WaitFor(otelCollector);
#pragma warning restore ASPIRECERTIFICATES001

// --- public tunnel ---
// The public HTTPS origin (PIVOX_PUBLIC_HOST) is fronted by a
// Cloudflare Tunnel that forwards to envoy on localhost:8081. cloudflared runs
// as a host service (`brew services start cloudflared`, config in
// ~/.cloudflared/config.yml) — NOT an Aspire resource — so it's independent of
// the stack lifecycle and shared across dev machines via each dev's own zone.
// Nothing to declare here.

// --- diag — network diagnostics sidecar (latest Ubuntu LTS + net tools) ---
// A throwaway container for poking the dev stack from *inside* the Aspire
// container network. Built from aspire/diag/Dockerfile (ubuntu:latest + ping/
// dig/ss/nc/curl/traceroute/mtr/tcpdump/psql/kcat/...). It gets a WithReference
// to every OTHER resource below, so their connection strings + service-
// discovery endpoints are injected as env:
//   docker exec -it <diag-container> bash
//   env | sort | grep -Ei 'ConnectionStrings|services__'   # probe targets
// No WaitFor: it starts immediately so you can exec in and watch the rest come
// up. Stays alive via `sleep infinity` (see the Dockerfile).
var diag = builder
  .AddDockerfile("diag", "diag")
  .WithContainerNetworkAlias("diag");

// Reference every OTHER resource. Iterating the model (rather than hand-listing
// them) keeps this correct as resources are added/removed. Parameters are
// skipped — they're secrets, not network targets. The capability interface a
// resource implements picks the ref kind AND disambiguates the WithReference
// overload: connection-string resources (postgres + its DBs, kafka) inject
// ConnectionStrings__<name>; endpoint-bearing resources (keycloak, rustfs,
// envoy, otel-collector, kafka-ui, the host apps, ...) inject
// services__<name>__<endpoint>__N. Resources that implement neither (e.g. a
// pure one-shot executable with no endpoints) contribute nothing and are the
// only ones left unreferenced.
foreach (var resource in builder.Resources.ToList())
{
  if (resource == diag.Resource || resource is ParameterResource)
  {
    continue;
  }
  if (resource is IResourceWithConnectionString cs)
  {
    // postgres + its DBs, kafka → ConnectionStrings__<name> (+ property env).
    diag.WithReference(builder.CreateResourceBuilder(cs));
  }
  else if (resource is IResourceWithServiceDiscovery sd)
  {
    // keycloak (KeycloakResource is service-discovery-aware) → all endpoints
    // as services__<name>__<endpoint>__N.
    diag.WithReference(builder.CreateResourceBuilder(sd));
  }
  else if (resource is IResourceWithEndpoints ep)
  {
    // Plain containers (rustfs, envoy, otel-collector, kafka-ui) + the vite app
    // (start) expose endpoints but aren't service-discovery resources, so
    // reference each endpoint individually → services__<name>__<endpoint>__N.
    foreach (var endpoint in ep.GetEndpoints())
    {
      diag.WithReference(endpoint);
    }
  }
}

builder.Build().Run();

// Generate the effective envoy config and return its mount-relative path. The
// OTel tracing block in configs/envoy.aspire.yaml (between the BEGIN/END
// envoy-otel-tracing markers) is STRIPPED unless ASPIRE_OTEL_ENVOY_ENABLED is
// set. envoy's static YAML can't read env, so the apphost does it — letting you
// toggle envoy's (noisy) per-request ingress spans via direnv. Output goes to
// the gitignored .data dir and is regenerated on every `aspire run`.
static string WriteEffectiveEnvoyConfig()
{
  var here = AppHostDir();
  var yaml = File.ReadAllText(Path.Combine(here, "../configs/envoy.aspire.yaml"));
  if (!IsTruthy(Environment.GetEnvironmentVariable("ASPIRE_OTEL_ENVOY_ENABLED")))
  {
    yaml = Regex.Replace(
        yaml,
        @"^[ \t]*# BEGIN envoy-otel-tracing\n[\s\S]*?# END envoy-otel-tracing[ \t]*\r?\n",
        "",
        RegexOptions.Multiline);
  }
  Directory.CreateDirectory(Path.Combine(here, ".data"));
  File.WriteAllText(Path.Combine(here, ".data/envoy.effective.yaml"), yaml);
  return "./.data/envoy.effective.yaml";
}

static bool IsTruthy(string? value) =>
    value != null && (value.ToLowerInvariant() == "true" || value == "1");

// import.meta.dirname equivalent: the directory of THIS source file, captured at
// compile time via [CallerFilePath] so path resolution is independent of the
// current working directory (matches the TS AppHost's behavior).
static string AppHostDir([CallerFilePath] string path = "") =>
    Path.GetDirectoryName(path)!;
