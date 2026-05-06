# Validation prompt — test framework audit

Paste the section below into a fresh Claude Code session
(preferably with a code-reviewer or general-purpose subagent).
The author of these changes has low trust in their own work and
wants an independent read.

---

## Context

The repo's test framework was just rebuilt. Specifically:

1. **Deleted** every test file that used `MockQuerier` (30+
   files) along with `internal/testutil/mocks/` and
   `internal/testutil/mocksetup/`. The deletions are catalogued
   in the most recent commits.
2. **Replaced** per-test testcontainers (Postgres + rustfs) with
   a single `docker-compose.test.yml` stack shared across every
   `go test ./...` invocation. See `internal/testutil/db.go` and
   `internal/testutil/s3.go`.
3. **Per-test isolation** for Postgres is via the
   `CREATE DATABASE ... TEMPLATE` clone pattern; for rustfs via
   per-test buckets.
4. **Cross-process template coordination** uses a Postgres
   advisory lock + a marker table (`__pivox_test_template_ready`)
   to handle concurrent `go test ./...` packages without racing
   on template creation.
5. **Mockery v3** is wired (`.mockery.yml`, `make mocks`) for
   external boundaries only — Firebase Auth (`authn.Service`).
   Internal interfaces (`db.Querier`, etc.) are NOT mocked.

The author has demonstrated repeated failures during this work:

- **Lazy diagnosis from external symptoms** instead of reading
  actual code. Examples: filed an "AWS SDK timeouts" issue
  without reading `SetupTestS3` (the real cause was per-test
  containers, fixable in 30 seconds); guessed at proto field
  names without reading the .proto; invented `pgconnTag any`
  to dodge an import.
- **Made-up types and structural shims** to avoid imports
  (forbidden per `internal/AGENTS.md` "Never dodge an import").
- **Filing issues for things that should be fixed in the same
  PR** (auto-slug dead code: filed #77, then fixed it the next
  message).
- **Believing self-summary instead of running tests** — claims
  of "green" without verification.

Your job is to find latent issues this author missed. Default to
"there's something wrong here" rather than "looks fine."

## What to verify

**Run the actual checks. Don't trust the diff descriptions.**

### 1. Test framework correctness

- [ ] `make test-up` brings up Postgres + rustfs healthy. Note
  what happens if it's already running (idempotent?).
- [ ] `go clean -testcache && go test -tags=dev ./...` runs in
  under ~10s wall time. If not, find the slow package and read
  the test setup. The previous regression was per-test
  containers; check for a similar pattern (per-test heavy
  fixture creation, cold caches, sleeps).
- [ ] All packages green. If any fail, run them individually
  with `-v` and audit the actual error.
- [ ] `docker-compose.test.yml` services use ports 55432 / 59000.
  Confirm they don't collide with the dev pivox stack on
  whatever ports it uses.

### 2. Per-test isolation

- [ ] Run a service-layer test twice in a row
  (`go test -tags=dev -run TestE2E_OrgSoftDeleteRevive ./internal/service/organizations/`).
  Both runs must pass — if state leaks across runs, the
  template-clone isolation is broken.
- [ ] Run two packages in parallel several times
  (`for i in 1 2 3 4 5; do go test -tags=dev -count=1 ./internal/service/organizations/ ./internal/service/spaces/; done`).
  No flakes from cross-process template races.
- [ ] Inspect `internal/testutil/db.go`'s `initTemplateDB` — the
  advisory lock + marker pattern. Is the unlock properly
  deferred even on early-return paths? Could a half-built
  template (process killed mid-migration) be considered "ready"
  by a later process?

### 3. Verify nothing's mocked at the wrong layer

- [ ] `grep -rl "MockQuerier\|testutil/mocks" internal/` — should
  return nothing. If it returns anything, the deletion missed
  files.
- [ ] `grep -rln "type Mock" internal/` — find any inline mocks
  outside `internal/testutil/authnmock/` (the only allowed
  generated location). External boundary mocks should go
  through `.mockery.yml` + `make mocks`, not hand-rolled.
- [ ] `make mocks` should regenerate without producing a diff.
  If it does produce a diff, the committed mock is stale or the
  config is wrong.

### 4. Read the testutil/db.go template logic critically

- [ ] `validIdent` is supposed to guard against SQL injection
  in the few places identifier names are interpolated. Check
  it's actually called everywhere it needs to be. Look for any
  `fmt.Sprintf` that takes a database name from input — the
  helper must validate before formatting.
- [ ] `dropDatabase` runs `pg_terminate_backend(...)` to kill
  stragglers. Is there a window where a still-active
  connection from a parallel process could be terminated by
  someone else? (Likely fine because per-test DBs have unique
  names; verify.)
- [ ] The "ready marker" table is created at the END of
  `initTemplateDB`. If migrations succeed but the marker
  creation fails, what happens on next run? (Should re-init
  because no marker = "not ready"; verify.)

### 5. Verify the SetupTestS3 happy + failure modes

- [ ] If rustfs isn't running, what does the first SetupTestS3
  call do? The error message should mention `make test-up`.
- [ ] Each test's bucket name is derived from `t.Name()` and
  truncated to 63 chars. What happens if two tests have names
  that collide after truncation? (Vanishingly rare in practice,
  but check the helper.)
- [ ] Cleanup drains objects then drops the bucket. What if a
  test fails mid-write? Does cleanup still succeed, or could
  it leave the bucket behind?

### 6. Sniff for the same diseases the author has

The author keeps doing these. Check.

- [ ] Any new file in `internal/testutil/` using `any` /
  `interface{}` / `mockTestingT`-style structural shims to dodge
  importing the real type. Forbidden per `internal/AGENTS.md`
  "Never dodge an import."
- [ ] Any test that asserts on call shape only (e.g.
  `mock.AssertCalled("MethodName", args)`) without verifying
  the post-condition. The mockery-generated mock allows
  `EXPECT().X().Return(nil).Once()` — fine — but if a test
  asserts only that a method was called and never checks the
  observable effect, it's probably mock theater.
- [ ] Any test that pre-supposes runtime state without setup
  (e.g. assuming a row exists without a Create call). These
  break when run in isolation.
- [ ] Any test that creates a fresh container/server inside the
  test body when a shared one is available. Same disease as the
  rustfs issue.

### 7. Validate the docker-compose

- [ ] Pull `docker-compose.test.yml` and read it line by line.
  Verify the healthchecks actually fire (rustfs's `/health`
  returns 200, not 403; PG18 mount convention).
- [ ] `docker compose -p pivox-test -f docker-compose.test.yml ps`
  should show two containers. Verify the project name is
  consistently `pivox-test` (not `pivox`, which would collide
  with the dev compose).

### 8. Build mode and dev tag

The repo currently uses `-tags=dev` to swap dev variants of
crypto/appkey/server/storageagent. The author was discussing
removing the tag entirely. Read CLAUDE.md "The dev build tag"
section. Decide if the current state is internally consistent
or if there's a halfway-removed mess.

## Output expected

Report what you found. Surface anything wrong directly, with
file paths and line numbers. Don't soften. The author has
explicitly low trust in their own work and wants real signal.

If you find:
- Latent bugs: report them with the exact reproducer.
- Test theater (assertions that don't catch real bugs): name
  the test + why it doesn't earn its keep.
- Inconsistent behavior under concurrent runs: describe the
  race.
- Anti-patterns (made-up types, lazy diagnosis tells): cite the
  file:line.

If you find nothing wrong, say so explicitly. "Looks fine" is
acceptable output if it's true.
