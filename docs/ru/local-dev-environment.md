# Local Developer Environment

## Purpose

This document defines the local containerized environment for building,
running and testing `t-helper` during implementation stages.

The environment must support:

- product containers for `thelper`, `thelper-worker`, `thelper-ctl`, `web` and
  reverse proxy/runtime delivery where applicable;
- dependency containers required for storage, repository, auth, SCIM and toolchain
  testing;
- automated test execution in isolated containers;
- manual exploratory testing through the same product API and UI;
- Linux OS-family compatibility checks for Ubuntu, Red Hat family and Amazon
  Linux;
- macOS compatibility checks through a native macOS runner, because macOS
  container images are not supported by Docker.

The local environment is for development and test only. It is not a production
deployment contract.

## Network Model

All local environment services must run in a dedicated Docker network:

```text
t-helper-dev
```

Rules:

- product, dependency and test-runner containers join only `t-helper-dev` unless
  a test explicitly needs host access;
- service discovery uses Docker DNS names, not host ports;
- host port publishing is disabled by default except for manual profiles;
- manual profiles may publish `thelper` API, `web` and `nginx` ports to
  `127.0.0.1` only;
- security scan tests must run in a network-restricted profile with no outbound
  SaaS access;
- containers must not use host Docker socket mounts.

Recommended network name in compose:

```yaml
networks:
  t-helper-dev:
    name: t-helper-dev
```

## Compose Projects and Profiles

The canonical local compose project name is:

```text
t-helper-dev
```

Recommended files:

```text
docker-compose.dev.yml
docker-compose.test.yml
docker-compose.os-matrix.yml
```

Recommended profiles:

| Profile | Purpose |
| --- | --- |
| `product` | Build and run product containers. |
| `storage` | Start storage dependencies. |
| `repo` | Start local Git/provider test services. |
| `security-tools` | Provide Terraform/TFLint/Trivy toolchain fixtures. |
| `auth` | Start auth/SCIM test doubles. |
| `e2e` | Start frontend/browser test dependencies. |
| `manual` | Publish selected ports to `127.0.0.1` for manual testing. |
| `offline` | Run tests with no external network assumptions. |
| `os-matrix` | Run Linux OS-family compatibility test containers. |

Stage 04 provides the current executable backend baseline and test runner.
Compose commands may now be used directly for the `postgres` and `test-runner`
services, including config/module/runtime, jobs/status and scanner/registry
checks; later product containers are still introduced by their owning stages.

## Product Containers

The environment must build and run product images from the current working tree.

Required product services:

| Service | Component | Notes |
| --- | --- | --- |
| `thelper` | `cmd/thelper` | API/runtime process. |
| `thelper-worker` | `cmd/thelper-worker` | Background job worker process. |
| `thelper-ctl` | `cmd/thelper-ctl` | One-shot administrative CLI container. |
| `web` | shared React frontend | Stage 08 and later. |
| `nginx` | reverse proxy/static delivery | Stage 08/server setup and later. |

Product services must use the same container network and configuration import
contract as the documented runtime:

- `thelper-ctl -reconfigure` imports `config.json` and `.t-helper.ignore`;
- `thelper` reads runtime configuration from storage;
- workers read jobs from storage and do not receive raw secrets in payloads;
- `web` and GUI-equivalent browser testing use only documented `/api` endpoints.

Recommended internal service addresses:

```text
thelper:8080
web:5173
nginx:8088
postgres:5432
mysql:3306
mssql:1433
```

Host bindings, when enabled by `manual`, must use loopback only:

```text
127.0.0.1:8080 -> thelper:8080
127.0.0.1:8088 -> nginx:8088
```

## Dependency Containers

Required for MVP development:

| Service | Image family | Required stage | Purpose |
| --- | --- | --- | --- |
| `postgres` | PostgreSQL 16 or 17 | Stage 01 | PostgreSQL storage tests. |
| `git-server` | local bare Git service or lightweight SSH/HTTP Git server | Stage 05 | Generic Git clone/pull/sync tests. |
| `repo-provider-mock` | local HTTP mock | Stage 05 | GitLab/GitHub provider API fixtures without SaaS calls. |
| `scim-mock` | local HTTP mock | Stage 07/13 | SCIM contract and sync fixtures. |
| `mail-mock` | local SMTP/web mock | Stage 07 | Password reset/manual auth flows. |
| `browser-runner` | Playwright-capable image | Stage 08 | Web UI e2e tests. |

Platform/stage-gated dependencies:

| Service | Required stage | Purpose |
| --- | --- | --- |
| `mysql` | Stage 10 | MySQL storage adapter tests. |
| `mssql` | Stage 10 | MSSQL storage adapter tests. |
| `webhook-provider-mock` | Stage 14 | Repository webhook payload/signature tests. |

SQLite uses a container volume or bind-mounted test directory and does not need
a database service.

## Toolchain Containers

External scanner compatibility must be tested with pinned toolchain images or
tool bundles:

| Tool | Stage | Purpose |
| --- | --- | --- |
| `terraform` | Stage 06A/06B | `terraform validate` profile fixtures. |
| `tflint` | Stage 06A/06B | TFLint profile fixtures. |
| `trivy` | Stage 06A/06B | Mandatory MVP security scanner fixtures. |
| `gitleaks` | Stage 11 | Extension scanner fixtures. |
| `checkov` | Stage 11 | Extension scanner fixtures. |
| `opa` | Stage 11 | Policy engine fixtures. |
| `conftest` | Stage 11 | Policy engine fixtures. |

Toolchain test containers must support:

- version discovery;
- certified profile validation;
- unsupported version negative tests;
- missing binary negative tests;
- malformed output fixtures;
- redaction tests for secret-like values.

Raw tool output is fixture input only. It must not become the primary persisted
job, scan or finding contract.

## Automated Tests

Automated tests are grouped by blast radius.

### Fast Local Tests

Run on every local developer test invocation:

- Go unit tests;
- package-level domain/service tests;
- SQLite storage contract tests;
- config validation tests;
- payload schema validation tests;
- secret masking tests;
- frontend unit tests after Stage 08.

Expected command:

```text
make test
```

`make test` includes `gofmt` check, `go vet ./...` and `go test ./...`.

### Container Integration Tests

Run through `docker-compose.test.yml` in `t-helper-dev`:

- PostgreSQL storage contract suite - implemented in Stage 01;
- migration tests from empty databases;
- API contract tests against `thelper`;
- worker claim, lease, heartbeat and retry tests;
- `job_locks` conflict tests;
- global scanner fixtures;
- repository manager fixtures using `git-server` and `repo-provider-mock`;
- auth/RBAC/SCIM contract tests;
- security tool profile tests through toolchain containers;
- network-restricted security scan tests.

Integration tests must start product containers, not only libraries. At minimum,
the stack under test contains:

```text
postgres
thelper
thelper-worker
thelper-ctl
test-runner
```

Stage-specific tests add their dependency services.

### End-to-End Tests

Run after Stage 08:

- `web`/`nginx` startup;
- login/session flows;
- projects, repositories, jobs, modules and findings screens;
- clone workflow form validation;
- long-running job status display;
- access-denied states;
- shared route tree parity for Web UI and GUI-equivalent browser tests.

E2E tests must use documented backend APIs only.

### OS-Matrix Tests

Linux OS-family compatibility must run in containers:

| Runner service | Base image family | Purpose |
| --- | --- | --- |
| `test-ubuntu` | Ubuntu LTS | Build/test on Debian/Ubuntu family. |
| `test-redhat` | Red Hat UBI or compatible RHEL-family image | Build/test on Red Hat family. |
| `test-amazonlinux` | Amazon Linux 2023 | Build/test on Amazon Linux family. |

Each Linux OS runner must execute the same contract:

- install only documented build dependencies;
- build `thelper`, `thelper-worker` and `thelper-ctl`;
- run Go unit tests;
- run SQLite storage tests;
- run CLI smoke tests;
- run product binary smoke tests against `GET /api/health`;
- optionally run PostgreSQL integration tests against the shared `postgres`
  service.

macOS compatibility must run outside Docker on a native macOS host or CI runner:

- build Go binaries natively;
- run unit tests and SQLite tests;
- run CLI smoke tests;
- run Tauri-related checks after Stage 08;
- run browser/e2e tests against product containers when Docker Desktop is
  available on the macOS runner.

macOS must not be represented as a Linux container substitute.

## Manual Testing

Manual testing uses the same compose stack with the `manual` profile.

Required manual flows by stage:

- import config through `thelper-ctl -reconfigure`;
- verify `GET /api/health`;
- open Web UI through `nginx`;
- run global scan against mounted Terraform fixtures;
- inspect projects, jobs and module states;
- run project discovery fixtures for Git markers;
- run repository clone/pull/sync against local Git services;
- run project/security scans with local toolchain profiles;
- verify auth login/logout/password flows and RBAC denial states;
- verify that logs, job payloads and API responses do not expose secrets.

Manual fixtures should live under:

```text
test/fixtures/
  terraform/
  repositories/
  security-tools/
  auth/
```

Manual runtime data should live under ignored artifact paths:

```text
.artifacts/dev/
.artifacts/test/
```

## Configuration and Secrets

Local environment config files must use `secretref://env/...` for sensitive
values. Compose must inject local-only environment variables into the services
that resolve them.

Rules:

- raw secrets must not be written to `config.example.json`;
- raw secrets must not be persisted in `config_entries`;
- job payloads must carry IDs or secret references only where documented;
- API responses mask sensitive values;
- logs and test artifacts must not contain resolved secrets.

Recommended local variables:

```text
THELPER_POSTGRES_USER
THELPER_POSTGRES_PASSWORD
THELPER_POSTGRES_DSN
THELPER_MYSQL_DSN
THELPER_MSSQL_DSN
```

## Volumes and Artifacts

Recommended volumes:

| Volume/path | Purpose |
| --- | --- |
| `.artifacts/dev/sqlite` | SQLite runtime/test databases. |
| `.artifacts/dev/repos` | Cloned repositories for manual tests. |
| `.artifacts/dev/logs` | Product logs. |
| `.artifacts/test/results` | JUnit, coverage, Playwright and contract outputs. |
| `.artifacts/test/tool-output` | Redacted external tool fixture output. |

Artifact directories must be ignored by version control.

## Required Health Gates

A full local stack is ready for tests only when:

- `postgres` accepts connections, when the PostgreSQL profile is enabled;
- `thelper` returns `health_status.v1` from `GET /api/health`;
- `thelper-worker` is connected to the same storage profile;
- migrations have completed successfully;
- `thelper-ctl -reconfigure` has imported the test config;
- dependency mocks are reachable by Docker DNS name;
- manual profile host ports, if enabled, are bound to `127.0.0.1`.

For historical Stage 01-only validation, the required gates are reduced to:

- `postgres` accepts connections when PostgreSQL tests are enabled;
- Stage 01 migrations complete successfully;
- `GET /api/health` returns `health_status.v1`;
- storage contract tests pass for SQLite and PostgreSQL.

## Stage Ownership

Environment implementation is stage-owned like migrations:

- Stage 01 adds `postgres`, test runner and storage tests. Product images are
  expanded by the owning runtime/frontend stages.
- Stage 02 added config import, module registry and singleton runtime smoke tests.
- Stage 03 added worker/status integration tests.
- Stage 04 added scanner fixtures, scanner/registry API tests and global scan
  tests.
- Stage 05 adds Git/repository/provider mock services.
- Stage 06A/06B add toolchain profile and scanner/security tests.
- Stage 07 adds auth, RBAC, SCIM and audit test doubles.
- Stage 08 adds Web UI, browser runner and manual UI profile.
- Stage 10 adds MySQL/MSSQL services and storage contract profiles.
- Stage 14 adds webhook provider mocks.

Each stage must update this document when it introduces a new required service,
port, volume, profile or test gate.
