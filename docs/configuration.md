# Конфигурация

## Источник истины

После первичного импорта source of truth для runtime-конфигурации - БД.

Файлы используются только как вход для `thelper-ctl -reconfigure`:

- `config.json`
- `.t-helper.ignore`

`config.example.json` поставляется как валидный референс для структуры `config.json`.

`thelper` не должен читать эти файлы как runtime source of truth после успешного импорта.

Настройки storage backend являются особой частью конфигурации. `config.json` может
использоваться для первичного чтения и для подготовки migration target, но runtime
выбирает активную БД по записям в storage configuration table. В этой таблице
хранятся минимум два profile slot:

- `current` - БД, которая используется активным runtime сейчас;
- `migration` - БД, на которую может быть выполнена миграция через `thelper-ctl -migrate-db`.

`thelper-ctl -reconfigure` может обновлять runtime config и profile slot `migration`,
но не переключает активную БД. Переключение с `current` на `migration` разрешено
только после успешного `thelper-ctl -migrate-db`, который создаёт/обновляет схему,
переносит данные, проверяет результат и актуализирует статусы profiles. Информация
о старой БД сохраняется в storage configuration table; сама старая БД не удаляется
автоматически и может быть использована администратором для ручного отката.

## Пример `config.json`

Комментарии в примерах допускаются только в документации. Файл, который читает `thelper-ctl -reconfigure`, должен быть валидным JSON без комментариев.

```json
{
  "system_settings": {
    "app_name": "t-helper",
    "version": "1.0.0",
    "mode": "server"
  },
  "database": {
    "database_type": "sqlite",
    "database_path": "/var/lib/t-helper/t-helper.db"
  },
  "external_databases": {
    "enabled": false,
    "provider": "postgresql",
    "engine_flavor": "standard",
    "host": "postgres.example.internal",
    "port": 5432,
    "username": "secretref://env/THELPER_POSTGRES_USER",
    "password": "secretref://env/THELPER_POSTGRES_PASSWORD",
    "database_name": "t_helper"
  },
  "scanning": {
    "global_scan": [
      {
        "name": "Platform Terraform",
        "root_path": "/srv/t-helper/scan-roots/platform",
        "schedule": true,
        "frequency": "daily"
      },
      {
        "name": "Application Terraform",
        "root_path": "/srv/t-helper/scan-roots/applications",
        "schedule": true,
        "frequency": "weekly"
      }
    ],
    "security_scan": {
      "modules": [
        "trivy"
      ]
    },
    "toolchain": {
      "version_policy": "certified_only",
      "profile_paths": []
    }
  },
  "repositories": {
    "default_auth_type": "ssh",
    "poll_interval_default": "15m",
    "auto_sync_default": false
  },
  "security": {
    "active_rule_set_id": null
  },
  "api": {
    "listen_address": "127.0.0.1:8080"
  },
  "auth": {
    "local_enabled": true
  },
  "workers": {
    "enabled": true,
    "concurrency": 1
  },
  "modules": {
    "enabled": [
      "core",
      "worker-runtime",
      "config-manager",
      "module-runtime",
      "status-monitor",
      "global-scanner",
      "project-scanner",
      "security-validator",
      "repository-manager",
      "auth",
      "web"
    ]
  },
  "logging": {
    "level": "info",
    "format": "json",
    "log_path": "/var/log/t-helper"
  }
}
```

## Секции `config.json`

### `system_settings`

- `app_name` - имя приложения; для стандартной поставки `t-helper`;
- `version` - версия схемы/поставки конфигурации;
- `mode` - режим запуска, `server` или `local`.

### `database`

Описывает внутреннее хранилище, которое используется, когда `external_databases.enabled = false`.

- `database_type` - `sqlite`;
- `database_path` - путь к каталогу или файлу внутреннего хранилища.

### `external_databases`

Описывает внешний storage target. Если `enabled = true`, все обязательные поля
внешнего подключения должны быть заданы. В Stage 02 это создаёт или обновляет
`migration` profile; runtime начинает использовать внешнюю БД только после
успешного `thelper-ctl -migrate-db` и последующего запуска с promoted `current`
profile.

- `enabled` - включает внешний storage backend;
- `provider` - `postgresql`, `mysql` или `mssql`;
- `engine_flavor` - optional operational hint for compatible managed engines; supported values: `standard`, `aurora`;
- `host` - IP-адрес или FQDN;
- `port` - TCP-порт провайдера;
- `username` - имя пользователя или закодированное представление секрета, если используется secret codec;
- `password` - пароль или ссылка/закодированное представление секрета;
- `database_name` - имя базы данных.

`provider = postgresql` покрывает PostgreSQL-compatible engines, включая Amazon Aurora PostgreSQL. `provider = mysql` покрывает MySQL-compatible engines, включая Amazon Aurora MySQL. Aurora не является отдельным provider/dialect: для неё используются `postgresql` или `mysql` migrations/adapters.

`provider = mssql` предназначен для native Microsoft SQL Server-compatible engines. Babelfish for Aurora PostgreSQL не считается эквивалентом `mssql` adapter без отдельного compatibility decision.

Все database providers реализуются как подключаемые storage adapter libraries. Unknown provider должен приводить к `validation_error` без частичного применения конфигурации.

`database` и `external_databases` в файле импорта описывают initial `current`
profile и optional `migration` profile. Для пустой установки с
`external_databases.enabled = true` `database` остаётся initial `current`
profile, а `external_databases` создаёт `migration` target. Они не являются
reloadable runtime settings и не должны переключать активный storage backend
без `thelper-ctl -migrate-db`.

### `workers`

Описывает отдельные worker-процессы, выполняющие background jobs.

- `enabled` - включает обработку queued jobs через `thelper-worker`;
- `concurrency` - максимальное число jobs, одновременно выполняемых одним worker process.

`thelper` не должен выполнять long-running jobs inline. Он создаёт jobs и отдаёт их на выполнение отдельным `thelper-worker` процессам.

Worker execution defaults are provider-specific. `workers.concurrency` в
top-level config остаётся compatibility/default value для текущего active provider,
но effective limits, busy timeout, lease defaults и concurrency policy должны
храниться и применяться отдельно для каждого database provider/profile.

Минимальные MVP defaults:

- `sqlite`: one active worker process, `concurrency = 1`, `journal_mode = WAL`, `foreign_keys = ON`, `busy_timeout = 5s`;
- `postgresql`: multiple worker processes allowed, concurrency is installation-specific and can be increased after load testing.

`thelper-worker` enforces the SQLite process limit with a local worker lock file
under `.artifacts/runtime` by default. The lock is keyed by the active database
fingerprint and is released on normal worker shutdown; stale locks left by dead
processes are replaced.

Изменение worker settings для одного provider/profile не должно менять settings
другого provider/profile. Например, настройка PostgreSQL concurrency перед
миграцией не должна менять SQLite local-mode concurrency.

### `scanning`

Описывает глобальные настройки сканирования. Настройки project-level scan и security/validation scan хранятся относительно отдельного проекта и не должны задаваться глобальными default-параметрами в `config.json`.

- `global_scan` - список корневых путей для глобального сканирования;
- `global_scan[].root_path` - абсолютный корневой путь, который обходит `global-scanner`;
- `global_scan[].schedule` - включает запуск по расписанию для конкретного пути;
- `global_scan[].frequency` - `daily`, `weekly` или `monthly`;
- `security_scan.modules` - список доступных security/validation модулей и policy engines, которые можно подключать в настройках отдельного проекта. MVP example включает только обязательный `trivy`; `gitleaks`, `checkov`, `opa` и `conftest` являются extension modules outside mandatory MVP acceptance.
- `scanning.toolchain.version_policy` - политика допуска внешних CLI tool versions: `certified_only`, `compatible_range` или `latest_best_effort`;
- `scanning.toolchain.profile_paths` - optional локальные directories/files with additional tool profile files from ADR 0018. Profiles imported from these paths must pass validation before activation.

`global_scan` является каноническим именем поля входной конфигурации. Внутри storage эти записи мапятся на сущность `root_paths`.

Для scan и clone используется один и тот же список путей. При clone пользователь выбирает существующий `global_scan[].root_path` или создаёт новый root path. Если clone выполняется в новый root path, он должен быть добавлен в `scanning.global_scan` и сохранён как новый `root_path`.

### `repositories`

Секция содержит default-параметры repository operations и не задаёт отдельный `repo_root`.

- `default_auth_type` - default transport/auth hint для clone/pull/sync, не credential source;
- `poll_interval_default` - интервал polling sync по умолчанию;
- `auto_sync_default` - auto sync по умолчанию для новых repository cards.

Provider hosts and repository credentials are managed through `repository_provider_instances` and `repository_credentials`, not through `config.json`. Credential secret values must be referenced through `secretref://...` and are resolved by workers at use time.

### `modules`

Секция содержит список зарегистрированных runtime-модулей, которые должны быть активны в текущей установке.

Initial module registry создаётся seed step на foundation stages и содержит:

- `core`
- `worker-runtime`
- `config-manager`
- `module-runtime`
- `status-monitor`
- `global-scanner`
- `repository-manager`
- `project-scanner`
- `security-validator`
- `auth`
- `web`

`modules.enabled` может ссылаться только на registered modules. Unknown module name должен приводить к `validation_error` без частичного применения конфигурации.

Модуль может быть зарегистрирован до полной реализации. В таком случае `GET /api/modules` возвращает его с состоянием `unavailable`, а `restart`/`reload` возвращает controlled error вместо `404` или panic.

## `.t-helper.ignore`

Правила применяются относительно `root_path`.

```gitignore
.terraform/
**/.git/
**/node_modules/
**/.cache/
```

MVP matcher может быть exclude-only. Правила вида `!pattern` должны импортироваться и храниться без потери данных, но применяются только после реализации full `.gitignore` semantics.

## Reloadability

Reloadable без полного рестарта:

- `scanning.global_scan`, применяется к новым global scan jobs
- `scanning.security_scan.modules`, применяется к новым project security/validation jobs
- `repositories.default_auth_type`
- `repositories.poll_interval_default`
- `repositories.auto_sync_default`
- `security.active_rule_set_id`
- `logging.level`
- `logging.format`
- `logging.log_path`, если backend логирования поддерживает reopen без restart
- `modules.enabled`, если модуль поддерживает graceful start/stop

Требуют restart отдельного модуля:

- `api.listen_address`
- `auth.local_enabled`
- `workers.enabled`
- `workers.concurrency`
- параметры provider-specific auth adapters
- `system_settings.mode`

Не применяются через reload/restart:

- `database.database_type`
- `database.database_path`
- `external_databases.*`

Эти keys обновляют только storage profile metadata для initial bootstrap или
`migration` slot. Переключение active database выполняется только через
`thelper-ctl -migrate-db`.

`thelper-ctl -reload` должен явно вернуть список принятых reloadable
параметров, список фактически применённых в Stage 02 параметров и список
параметров, требующих `thelper-ctl -restart <module>` или полного service
restart. Reloadable key не должен попадать в `applied_keys`, если текущий
runtime ещё не реализует его применение без restart.
Explicit unknown reload keys возвращаются в `failed_keys` и не должны
молчаливо считаться применёнными.

## Валидация

`thelper-ctl -reconfigure` и `PUT /api/config` используют строгий schema contract:

- unknown top-level или nested keys должны возвращать `validation_error`;
- malformed JSON, trailing payload после первого JSON object и `null` вместо
  config object должны возвращать `validation_error`;
- deprecated/legacy aliases не принимаются;
- `scanning.global_scan` является единственным допустимым ключом для global scan roots;
- `scanning.global_scann`, `globalScan`, `global_scan_roots`, `scan_roots` и любые другие aliases должны отклоняться как unknown keys;
- validation errors не должны частично изменять `config_entries`,
  `ignore_rules` или runtime state;
- `PUT /api/config` не принимает `.t-helper.ignore` payload и поэтому не
  удаляет ранее imported system `ignore_rules`; очистка rules выполняется через
  существующий empty `.t-helper.ignore` при `thelper-ctl -reconfigure` или через
  Stage 04 ignore-rules API.

Минимальные правила:

- `system_settings.app_name` не должен быть пустым;
- `system_settings.mode` принимает `server` или `local`;
- `database.database_type` принимает `sqlite`;
- `database.database_path` должен быть нормализованным путём;
- если `external_databases.enabled = true`, `provider`, `host`, `port`, `username`, `password` и `database_name` обязательны;
- `external_databases.provider` принимает `postgresql`, `mysql` или `mssql`;
- `external_databases.engine_flavor`, если задан, принимает `standard` или `aurora`;
- `external_databases.port` должен быть положительным TCP-портом;
- `secretref://env/...` в MVP означает ссылку на переменную окружения, а не literal-значение для сохранения в БД;
- `scanning.global_scan[].root_path` должен быть абсолютным нормализованным путём;
- `scanning.global_scan[].schedule` должен быть boolean;
- `scanning.global_scan[].frequency` принимает `daily`, `weekly` или `monthly`;
- `scanning.security_scan.modules` должен содержать непустые уникальные имена модулей;
- `scanning.toolchain.version_policy` принимает `certified_only`, `compatible_range` или `latest_best_effort`; default MVP value is `certified_only`;
- `scanning.toolchain.profile_paths[]` должен содержать абсолютные нормализованные локальные paths, если задан;
- `repositories.default_auth_type` принимает `ssh`, `https` или `token` and is not a credential source;
- `repositories.poll_interval_default` должен быть положительным duration;
- `workers.enabled` должен быть boolean;
- `workers.concurrency` должен быть положительным integer;
- для `sqlite` effective `workers.concurrency` должен быть `1`, а попытка применить большее значение к active SQLite profile должна возвращать `sqlite_worker_concurrency_unsupported`;
- `modules.enabled` должен содержать только имена из initial module registry или из явно зарегистрированных extension modules;
- `api.listen_address` должен быть валидным host:port;
- `logging.level` принимает `debug`, `info`, `warn`, `error`;
- `logging.format` принимает `json` или `text`;
- `logging.log_path` должен быть нормализованным путём к каталогу;
- sensitive keys должны использовать `secretref://env/...`; literal values для sensitive keys должны возвращать `validation_error`;
- секреты и tokens не должны сохраняться в открытом виде в `config_entries.value`.

Ошибочная конфигурация не должна частично применяться: импорт и reload выполняются атомарно относительно одного набора изменений.

Sensitive keys:

- `external_databases.username`
- `external_databases.password`
- provider tokens и Git HTTPS tokens
- webhook secrets
- auth provider client secrets
- private keys или passphrases

`config_entries.value` хранит secret reference, а не resolved secret. Resolved secrets не должны попадать в `jobs.payload`, `jobs.result_payload`, `job_events.payload`, `workflow_statuses.summary_payload`, `audit_log.payload` или logs.

## Masked config output

`GET /api/config` возвращает активную runtime-конфигурацию с masked sensitive values.

API response не должен раскрывать resolved secret values. `secretref://env/NAME`
не является raw secret, но имя переменной окружения может раскрывать детали
инфраструктуры, поэтому output mode зависит от permissions:

- `system_admin` и субъекты с `system.config.read` могут видеть full secret reference, например `secretref://env/THELPER_POSTGRES_PASSWORD`, но никогда resolved value;
- `viewer`, object-scoped readers и ответы, разрешённые только через `system.runtime.read`, получают masked metadata без имени переменной;
- audit, job payloads, job results, workflow payloads and logs never contain resolved values and should prefer masked metadata over full refs unless the event is explicitly administrative.

Допустимая masked форма:

```json
{
  "masked": true,
  "ref_type": "env"
}
```

Raw `secretref://env/...` может храниться в `config_entries.value`, но API output должен избегать раскрытия resolved secret и не должен показывать literal secret values.

## Storage migration command

`thelper-ctl -migrate-db` выполняет controlled migration с `current` profile на
`migration` profile.

Минимальный Stage 02 contract:

- проверить доступность migration DB, provider, credentials и permissions;
- применить schema migrations к migration DB;
- перенести Stage 02-owned runtime data: `config_entries`,
  `storage_profiles`, `storage_provider_settings`, `module_states`,
  `ignore_rules` and system migration metadata;
- не переносить active transient locks как active state;
- проверить logical migration version, FK integrity и базовые counts/checksums;
- актуализировать profile statuses: migration становится `current`, предыдущий current сохраняется как historical/rollback candidate;
- поддерживать последующие migration target после successful switch без перезаписи active `current` profile;
- не удалять старую БД автоматически;
- требовать restart runtime после successful switch.

Если migration не завершилась успешно, active `current` profile не меняется.

Later stages must extend this same migration contract when they introduce their
own persistent tables. For example, Stage 03 extends it for jobs/workflows,
Stage 06 for findings/rule sets and Stage 07 for auth/RBAC/audit, including
audit event `storage.migration_completed` once audit storage exists.
