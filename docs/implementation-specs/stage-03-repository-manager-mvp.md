# ТЗ: Этап 3. Repository Manager MVP

## Цель

Добавить управляемые repository operations поверх registry, не нарушая инварианты путей, идемпотентности и сериализации конкурентных действий.

## Результат этапа

Система должна уметь выполнять `clone`, `pull`, `sync`, работать с `gitlab`/`github`/`bitbucket`, выбирать `https|ssh`, использовать существующий или новый `root_path`, поддерживать target directory и безопасно сериализовать операции по репозиторию.

## In Scope

- модуль `repository-manager`;
- доменная модель `repositories`;
- provider-aware clone adapters;
- `clone`, `pull`, `sync`;
- webhook-based и polling-based sync;
- recursive clone GitLab group/subgroups;
- `job_locks` для сериализации;
- API и jobs repository operations.

## Out Of Scope

- project/security scan orchestration после clone/pull;
- UI реализации clone workflow;
- расширенные auth providers;
- distributed execution repository-manager.

## Зависимости

- завершённый этап 1;
- завершённый этап 2 с рабочими `root_paths` и registry.

## Основные требования к реализации

### Path safety

- `local_path` должен всегда находиться внутри выбранного `root_path`;
- `full_path`, `target_directory` и итоговый путь должны нормализоваться;
- path traversal за пределы `root_path` должен отклоняться на уровне API и domain layer.

### Repository behavior

- если локальный каталог уже содержит корректный Git-репозиторий с ожидаемым remote, вместо `clone` выполняется `pull`;
- новый `root_path`, созданный при clone, автоматически добавляется в `root_paths`;
- provider влияет на URL parsing, transport selection и bulk operations.

### Concurrency

- одновременно должен существовать только один активный lock по `repository:<repository_id>`;
- конфликтующие `clone`, `pull`, `sync` должны либо ожидать, либо отклоняться согласно выбранной execution policy;
- expired locks не должны блокировать новые операции.

## Изменения в модели данных

- полноценно реализовать `repositories`;
- обеспечить использование `job_locks` для `repo_clone`, `repo_pull`, `repo_sync`;
- при необходимости добавить индексы на `repositories.full_path`, `repositories.clone_url`, `jobs.lock_key`.

## Изменения в payload schemas

- `jobs.repo_clone.payload.v1`
- `jobs.repo_pull.payload.v1`
- `jobs.repo_sync.payload.v1`
- `jobs.repo_operation.result.v1`

## Изменения в API

- `GET /api/repos`
- `GET /api/repos/{id}`
- `POST /api/repos/clone`
- `POST /api/repos/pull`
- `POST /api/repos/sync`

## Тестирование

- provider-specific contract tests для `gitlab`, `github`, `bitbucket`;
- тесты `https|ssh` transport selection;
- тесты clone в существующий и новый `root_path`;
- тесты clone в существующую и новую `target_directory`;
- конкурентные тесты на `job_locks`;
- тесты `gitlab_group_recursive` на группах и nested subgroups.

## Критерии приёмки

- MVP 14
- MVP 21 в части repositories/jobs
- MVP 22

## Риски этапа

- ошибки нормализации пути приведут к записи за пределы разрешённого root;
- provider adapters могут разъехаться по поведению и payload;
- сложность корректной деградации, если webhook/polling настроены, но provider временно недоступен.

## Артефакты поставки

- модуль `repository-manager`;
- provider-aware adapters;
- API и job execution для repository operations;
- тестовый пакет конкурентности и path safety.
