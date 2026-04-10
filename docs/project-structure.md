# Структура проекта

Документ фиксирует стартовый каркас репозитория для `t-helper`, собранный по каноническим документам:

- `docs/architecture.md`
- `docs/interfaces.md`
- `docs/api.md`
- `docs/data-model.md`
- `docs/roadmap.md`
- `docs/implementation-specs/stage-01-foundation.md`

## Принцип построения

Каркас привязан к модульной декомпозиции продукта и этапу `Foundation`, но не фиксирует преждевременно детали конкретного frontend/runtime toolchain сверх уже описанных бинарников, модулей и storage adapters.

## Дерево каталогов

```text
cmd/
  thelper/
  thelper-ctl/

internal/
  app/
    api/
    cli/
  contracts/
    api/
    payloads/
  core/
  domain/
    audit/
    auth/
    config/
    environments/
    ignorerules/
    jobs/
    modules/
    projects/
    repositories/
    rootpaths/
    scans/
    security/
    workspaces/
  modules/
    auth/
    config-manager/
    global-scanner/
    module-runtime/
    project-scanner/
    repository-manager/
    security-validator/
  platform/
    config/
    http/
    idempotency/
    jobs/
    locking/
    logging/
    runtime/
    storage/
      badger/
      migrations/
      postgresql/

web/
ui/

deploy/
  docker/
  nginx/

configs/
scripts/
test/
  contract/
  fixtures/
  integration/
build/
```

## Назначение каталогов

### `cmd/`

- `cmd/thelper` - entrypoint backend runtime/service process.
- `cmd/thelper-ctl` - entrypoint административного CLI.

### `internal/app/`

- `internal/app/api` - сборка HTTP API, module registry, dependency wiring.
- `internal/app/cli` - wiring CLI-команд `-reconfigure`, `-reload`, `-restart` и следующих команд roadmap.

### `internal/core/`

- общие orchestration primitives верхнего уровня;
- bootstrap runtime, не привязанный к отдельному модулю;
- место для shared application services foundation-этапа.

### `internal/domain/`

Доменные границы разложены по основным сущностям и capability areas из `docs/data-model.md` и `docs/requirements.md`:

- `rootpaths` - `root_paths`;
- `projects` - `projects`, `project_scan_settings`, `project_security_scan_settings`;
- `repositories` - `repositories`;
- `scans` - orchestration global/project scan use cases;
- `security` - findings, rule sets, policy integration contracts;
- `config` - `config_entries` и validation model;
- `modules` - `module_states` и lifecycle contracts;
- `jobs` - `jobs`, `job_locks`, job orchestration;
- `auth` - users, groups, roles, role bindings, SCIM boundaries;
- `audit` - audit log model;
- `environments` - `environments`;
- `workspaces` - `workspaces`;
- `ignorerules` - `ignore_rules`.

### `internal/modules/`

Каталоги повторяют логические runtime-модули из `docs/architecture.md`:

- `global-scanner`
- `project-scanner`
- `security-validator`
- `repository-manager`
- `config-manager`
- `auth`
- `module-runtime`

Это позволит держать lifecycle, health и restartability модулей изолированно уже с первого этапа.

### `internal/platform/`

Инфраструктурный слой для технических механизмов, явно требуемых документацией:

- `config` - import, masking, reloadability, validation helpers;
- `http` - HTTP server, middleware, error envelope, idempotency hooks;
- `idempotency` - `Idempotency-Key` handling для job-producing endpoints;
- `jobs` - job queue abstractions и execution plumbing;
- `locking` - `job_locks` и serialization policy;
- `logging` - базовое логирование;
- `runtime` - module registry, lifecycle coordinator;
- `storage/badger` и `storage/postgresql` - обязательные адаптеры Foundation;
- `storage/migrations` - SQL migrations framework и migration assets.

### `internal/contracts/`

- `internal/contracts/api` - DTO, request/response schemas и `api_error`.
- `internal/contracts/payloads` - versioned payload/result contracts из `docs/payload-schemas.md`.

### `web/` и `ui/`

- `web` - `Web UI` assets/app.
- `ui` - локальный `GUI`.

Пока каталоги пустые намеренно: документация фиксирует наличие клиентов и общий backend API, но не фиксирует стек реализации.

### `deploy/`

- `deploy/nginx` - reverse proxy и static delivery.
- `deploy/docker` - контейнеризация и локальные compose/deployment assets.

### `configs/`

- шаблоны конфигурации окружений;
- non-secret config fragments;
- дополнительные примеры поверх корневого `config.example.json`.

### `scripts/`

- developer scripts;
- helper automation для scaffolding, validation, local run и CI bootstrap.

### `test/`

- `test/contract` - API contract tests.
- `test/integration` - integration tests foundation/storage/runtime.
- `test/fixtures` - scan/storage/API fixtures.

### `build/`

- packaging assets;
- build metadata;
- release-oriented templates.

## Принятые допущения

- Каркас собран под roadmap-этап `Foundation`, но уже оставляет стабильные места для этапов 2-8.
- Директории `web/` и `ui/` созданы без выбора конкретного стека, потому что документация явно оставляет этот вопрос открытым.
- Названия доменных каталогов приведены к читаемой форме, близкой к сущностям из `docs/data-model.md`, чтобы не смешивать storage table names и package boundaries.

## Следующий шаг после каркаса

Практически оправданный следующий инкремент - заполнить `cmd/`, `internal/app/`, `internal/platform/runtime/`, `internal/platform/storage/` и `internal/contracts/` минимальным foundation-scaffolding: bootstrap, module registry, config import/reload, job model и API skeleton.
