# ТЗ: Этап 1. Foundation

## Цель

Построить базовый runtime-каркас `t-helper`, достаточный для последующей реализации доменных модулей без временных обходных решений.

## Результат этапа

По завершении этапа система должна уметь:

- запускать `thelper` и `thelper-ctl`;
- хранить runtime-конфигурацию в БД через storage abstraction;
- работать минимум с `PostgreSQL` и `Badger`;
- выполнять базовые миграции;
- импортировать конфигурацию через `thelper-ctl -reconfigure`;
- применять reloadable-конфигурацию через `thelper-ctl -reload`;
- перезапускать любой модуль через `thelper-ctl -restart <module>`;
- публиковать API skeleton, jobs и module states.

## In Scope

- каркас сервиса `thelper`;
- каркас CLI `thelper-ctl`;
- storage abstraction и адаптеры `Badger`/`PostgreSQL`;
- migrations framework;
- сущности `config_entries`, `module_states`, `jobs`, `job_locks`;
- базовое логирование;
- jobs framework;
- lifecycle менеджер модулей;
- API skeleton и минимальные системные endpoints;
- атомарный import/reload конфигурации.

## Out Of Scope

- реальный filesystem scan;
- clone/pull/sync;
- project/security scans;
- полноценная auth/RBAC логика;
- frontend-клиенты;
- distributed deployment.

## Зависимости

Этап стартовый. Внешних зависимостей на предыдущие этапы нет.

## Основные требования к реализации

### Runtime и lifecycle

- должен существовать регистр модулей с унифицированным lifecycle: `start`, `stop`, `reload`, `health`;
- `module_states` должен отражать актуальный state модуля;
- `thelper-ctl -restart <module>` обязан обновлять `module_states` и создавать `jobs.job_type = module_restart`.

### Конфигурация

- `config.json` и `.t-helper.ignore` используются только как вход для `thelper-ctl -reconfigure`;
- после импорта source of truth находится в `config_entries` и `ignore_rules`;
- импорт выполняется атомарно;
- `thelper-ctl -reload` должен возвращать applied keys и restart-required keys по схеме `jobs.config_reload.result.v1`.

### Хранилище

- логическая модель должна быть общей для `Badger` и `PostgreSQL`;
- SQL backend обязан поддерживать миграции;
- storage abstraction не должен раскрывать особенности конкретного backend в доменную логику.

### API и CLI

- должны быть доступны минимальные системные endpoints: `GET/PUT /api/config`, `GET /api/modules`, `POST /api/modules/reload`, `POST /api/modules/restart`, `GET /api/jobs`, `GET /api/jobs/{id}`;
- CLI должен поддерживать обязательные команды из `docs/interfaces.md`.

## Изменения в модели данных

- реализовать `config_entries`;
- реализовать `module_states`;
- реализовать `jobs`;
- реализовать `job_locks` как общий механизм для следующих этапов.

## Изменения в payload schemas

- реализовать поддержку:
- `jobs.config_reload.payload.v1`
- `jobs.config_reload.result.v1`
- `jobs.module_restart.payload.v1`
- `jobs.module_restart.result.v1`
- `module_states.details.v1`

## Изменения в API

- реализовать skeleton согласно `docs/api.md`;
- обеспечить единый формат `api_error`;
- поддержать `Idempotency-Key` для job-producing write endpoints.

## Тестирование

- unit-тесты для валидации `config.json`;
- integration-тесты import/reload/restart;
- storage test suite против `Badger` и `PostgreSQL`;
- contract-тесты системных endpoints;
- negative-тесты частичного применения ошибочной конфигурации.

## Критерии приёмки

- MVP 7
- MVP 8
- MVP 9
- MVP 10
- MVP 13

## Риски этапа

- расхождение между in-memory runtime model и persisted `config_entries`;
- слишком ранняя жёсткая привязка доменной логики к SQL;
- неполное разделение reloadable и restart-required параметров.

## Артефакты поставки

- исполняемые бинарники `thelper` и `thelper-ctl`;
- storage adapters для `Badger` и `PostgreSQL`;
- пакет миграций;
- runtime module registry;
- базовые системные API endpoints;
- автотесты foundation-слоя.
