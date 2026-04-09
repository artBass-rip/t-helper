# Конфигурация

## Источник истины

После первичного импорта source of truth для runtime-конфигурации - БД.

Файлы используются только как вход для `thelper-ctl -reconfigure`:

- `config.json`
- `.t-helper.ignore`

`config.example.json` поставляется как валидный референс для структуры `config.json`.

`thelper` не должен читать эти файлы как runtime source of truth после успешного импорта.

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
    "database_type": "badger",
    "database_path": "./database"
  },
  "external_databases": {
    "enabled": true,
    "provider": "postgresql",
    "host": "localhost",
    "port": 5432,
    "username": "dC1oZWxwZXItZGF0YWJhc2UK",
    "password": "secure_password",
    "database_name": "t_helper"
  },
  "scanning": {
    "global_scann": [
      {
        "root_path": "/Users/artbass/work/git/gitlab.foodtech.team/devops_only/example_1",
        "schedule": true,
        "frequency": "daily"
      },
      {
        "root_path": "/Users/artbass/work/git/gitlab.foodtech.team/devops_only/example_2",
        "schedule": true,
        "frequency": "weekly"
      }
    ],
    "security_scan": {
      "modules": [
        "trivy",
        "gitleaks",
        "checkov",
        "opa",
        "conftest"
      ]
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
  "modules": {
    "enabled": [
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
    "log_path": "/var/log/t-helper/"
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

- `database_type` - `sqlite` или `badger`;
- `database_path` - путь к каталогу или файлу внутреннего хранилища.

### `external_databases`

Описывает внешнюю БД. Если `enabled = true`, внутренняя БД из секции `database` не используется для runtime storage, а все обязательные поля внешнего подключения должны быть заданы.

- `enabled` - включает внешний storage backend;
- `provider` - `postgresql` или `mysql`;
- `host` - IP-адрес или FQDN;
- `port` - TCP-порт провайдера;
- `username` - имя пользователя или закодированное представление секрета, если используется secret codec;
- `password` - пароль или ссылка/закодированное представление секрета;
- `database_name` - имя базы данных.

### `scanning`

Описывает глобальные настройки сканирования. Настройки project-level scan и security/validation scan хранятся относительно отдельного проекта и не должны задаваться глобальными default-параметрами в `config.json`.

- `global_scann` - список корневых путей для глобального сканирования;
- `global_scann[].root_path` - абсолютный корневой путь, который обходит `global-scanner`;
- `global_scann[].schedule` - включает запуск по расписанию для конкретного пути;
- `global_scann[].frequency` - `daily`, `weekly` или `monthly`;
- `security_scan.modules` - список доступных security/validation модулей и policy engines, которые можно подключать в настройках отдельного проекта. Базовый локальный стек: `trivy`, `gitleaks`, `checkov`; enterprise-policy checks: `opa`, `conftest`.

`global_scann` сохраняет имя поля из входной конфигурации. Внутри storage эти записи могут мапиться на сущность `root_paths`.

Для scan и clone используется один и тот же список путей. При clone пользователь выбирает существующий `global_scann[].root_path` или создаёт новый root path. Если clone выполняется в новый root path, он должен быть добавлен в `scanning.global_scann` и сохранён как новый `root_path`.

### `repositories`

Секция содержит default-параметры repository operations и не задаёт отдельный `repo_root`.

- `default_auth_type` - тип аутентификации по умолчанию для clone/pull/sync;
- `poll_interval_default` - интервал polling sync по умолчанию;
- `auto_sync_default` - auto sync по умолчанию для новых repository cards.

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

- `scanning.global_scann`, применяется к новым global scan jobs
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
- параметры provider-specific auth adapters
- `system_settings.mode`
- `database.database_type`
- `database.database_path`
- `external_databases.*`

`thelper-ctl -reload` должен явно вернуть список применённых параметров и список параметров, требующих `thelper-ctl -restart <module>` или полного service restart.

## Валидация

Минимальные правила:

- `system_settings.app_name` не должен быть пустым;
- `system_settings.mode` принимает `server` или `local`;
- `database.database_type` принимает `sqlite` или `badger`;
- `database.database_path` должен быть нормализованным путём;
- если `external_databases.enabled = true`, `provider`, `host`, `port`, `username`, `password` и `database_name` обязательны;
- `external_databases.provider` принимает `postgresql` или `mysql`;
- `external_databases.port` должен быть положительным TCP-портом;
- `scanning.global_scann[].root_path` должен быть абсолютным нормализованным путём;
- `scanning.global_scann[].schedule` должен быть boolean;
- `scanning.global_scann[].frequency` принимает `daily`, `weekly` или `monthly`;
- `scanning.security_scan.modules` должен содержать непустые уникальные имена модулей;
- `repositories.default_auth_type` принимает `ssh`, `https` или `token`;
- `repositories.poll_interval_default` должен быть положительным duration;
- `api.listen_address` должен быть валидным host:port;
- `logging.level` принимает `debug`, `info`, `warn`, `error`;
- `logging.format` принимает `json` или `text`;
- `logging.log_path` должен быть нормализованным путём к каталогу;
- секреты и tokens не должны сохраняться в открытом виде в `config_entries.value`.

Ошибочная конфигурация не должна частично применяться: импорт и reload выполняются атомарно относительно одного набора изменений.
