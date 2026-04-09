# Анализ документации и подготовка к внедрению

## Назначение

Этот документ сводит воедино результаты анализа текущего documentation-first набора для `t-helper` и фиксирует выводы, важные для планирования внедрения по roadmap.

## Общая оценка зрелости документации

Документация находится в хорошем состоянии для старта code scaffolding:

- определены продуктовые границы MVP и platform release;
- выделены канонические документы по требованиям, архитектуре, API, конфигурации, данным, RBAC и тестированию;
- roadmap связан с acceptance criteria;
- таблица трассировки связывает capabilities с API, моделью данных, permissions, roadmap и приёмкой;
- зафиксированы инварианты хранения, идемпотентности, сериализации jobs и локальности security-стека.

Практически все ключевые контракты для старта реализации уже заданы. Основной дефицит не в полноте требований, а в необходимости превратить их в последовательные пакеты внедрения с явными deliverables, зависимостями и Definition of Done.

## Канонические источники истины

- Продуктовые и нефункциональные требования: `docs/requirements.md`
- Архитектурные границы и модули: `docs/architecture.md`
- CLI/API/config surface: `docs/interfaces.md`
- HTTP contracts: `docs/api.md`
- Конфигурация и reloadability: `docs/configuration.md`
- Persistent model и storage invariants: `docs/data-model.md`
- Payload/result schemas: `docs/payload-schemas.md`
- Auth/SCIM/RBAC и authorization matrix: `docs/access-control.md`
- Трассировка capability -> artifact: `docs/traceability.md`
- Acceptance и test coverage: `docs/test-plan.md`
- Последовательность внедрения: `docs/roadmap.md`

## Сильные стороны текущего набора

### 1. Чёткая модульная декомпозиция

Роли модулей разделены корректно:

- `global-scanner` отвечает только за discovery и registry update;
- `project-scanner` отвечает за `terraform validate` и `TFLint`;
- `security-validator` отвечает за `Trivy`, `Gitleaks`, `Checkov`, `OPA` и `Conftest`;
- `repository-manager` отвечает за clone/pull/sync и provider-aware workflows;
- `auth` изолирован как самостоятельный модуль;
- lifecycle и runtime operations вынесены в отдельный слой.

Это снижает риск смешения ответственности между этапами 2, 3, 4 и 5.

### 2. Хорошая связка roadmap и acceptance

Roadmap разбит на 8 последовательных этапов, а `test-plan.md` маппит MVP acceptance criteria на проверяемые API/storage/runtime assertions. Это позволяет строить внедрение не по списку функций, а по релизным инкрементам.

### 3. Нормально проработанная операционная модель

Документы заранее фиксируют:

- source of truth в БД;
- atomic import/reload;
- `job_locks` для сериализации конфликтующих операций;
- `Idempotency-Key` для фоновых write requests;
- независимость global scan, project scan и repo operations;
- подготовку к монолитному и distributed deployment.

### 4. Security-by-design в пределах on-premise

Последовательно повторяется один и тот же принцип: Terraform-код, findings, правила и policy packs остаются локальными и не отправляются наружу. Это снижает риск архитектурных отклонений уже на ранних этапах.

## Критичные архитектурные зависимости между этапами

### Этап 1 -> все остальные

Foundation должен завершиться до старта продуктовых модулей, потому что он поставляет:

- runtime-каркас;
- storage abstraction;
- jobs framework;
- module lifecycle;
- базовую конфигурационную модель;
- import/reload/restart;
- API skeleton.

Без него последующие этапы будут строить собственные временные механики и размножат технический долг.

### Этап 2 -> этапы 3, 4, 6

Scanner MVP создаёт базовые registry-сущности `projects`, `repositories`, `root_paths`, `ignore_rules`, `jobs`. От качества этой модели зависит:

- куда клонировать репозитории на этапе 3;
- что сканировать на этапе 4;
- какие read/operate сценарии сможет показать frontend на этапе 6.

### Этап 3 -> этап 4

Repository Manager должен завершиться до полноценного project/security scanning, потому что значимая часть project scans запускается после `clone` и `pull`.

### Этап 4 -> этапы 5, 6, 7

Project/security scans и findings формируют основную пользовательскую ценность MVP. Без них frontend будет неполным, а RBAC не получит реальные security-scoped сценарии.

### Этап 5 -> этапы 6, 7, 8

Auth, SCIM и RBAC должны появиться до полного frontend MVP и до distributed deployment, иначе UI и межмодульные вызовы будут проектироваться без корректной модели прав.

### Этап 7 -> этап 8

Operational Hardening должен предшествовать distributed deployment, потому что именно на нём дорабатываются observability, scheduler hardening, runtime hardening и administrative UI, без которых multi-node режим будет тяжело эксплуатировать.

## Ключевые сквозные инварианты, которые нельзя нарушить

### 1. База данных как source of truth

После `thelper-ctl -reconfigure` runtime не должен читать `config.json` и `.t-helper.ignore` как рабочий источник данных.

### 2. Один backend API для всех клиентов

`GUI` и `Web UI` обязаны использовать один и тот же backend API. Любые frontend-специфичные обходные контракты будут противоречить документации.

### 3. Project scan и security scan сходятся в одном API

Отдельная сущность `security_scans` специально не вводится. Все проектные и security/validation проверки сходятся в `POST /api/project-scans` и `project_scans`.

### 4. Репозиторные операции сериализуются

`clone`, `pull`, `sync` по одному `repository.id` должны сериализоваться через `job_locks`. Это обязательный инвариант, а не оптимизация.

### 5. MVP scanner не читает Terraform source без необходимости

Для discovery в MVP достаточно наличия `*.tf`. Это влияет на производительность и на границы ответственности `global-scanner`.

### 6. Security stack локален

Интеграция внешних SaaS-сервисов для findings, rules или исходников противоречит текущим требованиям.

## Обнаруженные пробелы и открытые решения

### 1. Не выбран технологический стек реализации

Документация умышленно не фиксирует backend/frontend/desktop stack. Это нормально для pre-implementation стадии, но до старта scaffolding нужен отдельный ADR-пакет хотя бы по:

- backend framework/runtime;
- storage adapter strategy;
- frontend stack;
- desktop GUI stack;
- job execution model.

### 2. Не зафиксирована точная доменная модель связей `project` / `environment` / `workspace`

Связи заданы на уровне сущностей и API read models, но не хватает строгих бизнес-правил жизненного цикла и автоматического связывания.

### 3. Не определён минимальный состав внешних auth providers

Для этапа 5 это влияет на границы MVP: локальная аутентификация обязательна, а набор внешних provider'ов остаётся открытым.

### 4. Не определён стартовый состав локальных security rules и policy packs

Документация задаёт механизм, но не продуктовый baseline содержимого.

### 5. Не принято решение по `terragrunt.hcl`

Сейчас это явно вне MVP, но важно заранее зафиксировать, входит ли поддержка в первый production release.

## Рекомендации по порядку внедрения

1. Сначала закрыть этап 1 полностью, включая migrations, config import/reload, jobs, module lifecycle и API skeleton.
2. Затем довести этап 2 до состояния, в котором registry стабилен, идемпотентен и проверен на fixture-наборе из `test-plan.md`.
3. После этого вводить этап 3 с упором на safe path handling, provider adapters и `job_locks`.
4. Этап 4 реализовывать как orchestration layer поверх локальных CLI tools, не смешивая результаты `project-scanner` и `security-validator`.
5. Этап 5 завершить до production-grade frontend rollout, чтобы UI проектировался сразу на реальной permission-модели.
6. Этап 6 строить на documented API contracts без создания frontend-only backend поведения.
7. Этап 7 использовать как обязательную стабилизацию перед выносом модулей в распределённый режим.
8. Этап 8 начинать только после фиксации межмодульных контрактов, health model, retry semantics и deployment topology.

## Рекомендуемый формат управления внедрением

Для каждого этапа roadmap нужен отдельный пакет реализации, содержащий:

- цель этапа;
- список deliverables;
- объём in scope / out of scope;
- зависимости на предыдущие этапы;
- изменения по API, БД, конфигурации и permissions;
- сценарии тестирования;
- критерии приёмки, завязанные на `roadmap.md` и `test-plan.md`;
- риски и открытые вопросы.

Именно в таком виде оформлены отдельные ТЗ в каталоге `docs/implementation-specs/`.
