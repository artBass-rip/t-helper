# ТЗ: Этап 2. Scanner MVP

## Цель

Реализовать discovery-слой, который обнаруживает Terraform-проекты и Git-репозитории в `root_path`, применяет ignore rules и поддерживает стабильный registry в БД.

## Результат этапа

Система должна уметь запускать глобальное сканирование, находить Terraform working directories по `*.tf`, определять Git markers, прекращать углубление после нахождения проекта и обновлять registry без дублей.

## In Scope

- модуль `global-scanner`;
- `root_paths` и `ignore_rules`;
- worker pool обхода;
- exclude-only matcher с сохранением `!pattern`;
- обнаружение Terraform-проектов по `*.tf`;
- обнаружение Git-репозиториев по `.git` directory/file и явно разрешённым markers;
- запись и обновление `projects`, `repositories`, `jobs`;
- API для scan/root paths/ignore rules/projects.

## Out Of Scope

- provider-aware repository operations;
- project-level checks и security findings;
- auth enforcement кроме минимальных контрактов API;
- full `.gitignore` semantics.

## Зависимости

- завершённый этап 1;
- рабочие `jobs`, `config_entries`, `module_states`, API skeleton и storage abstraction.

## Основные требования к реализации

### Алгоритм discovery

- сканер читает активные `root_path` из `scanning.global_scann`;
- ignore matcher строится относительно `root_path`;
- для обнаружения Terraform-проекта достаточно наличия `*.tf`;
- содержимое `.tf` файлов не читается на этапе discovery;
- после обнаружения Terraform-проекта углубление ниже директории прекращается;
- ошибки отдельных директорий не должны валить весь scan job.

### Registry behavior

- повторный scan не создаёт дубли `projects` и `repositories`;
- `last_seen_at` обновляется при повторном обнаружении;
- связи с `repository`, `environment`, `workspace` сохраняются, если уже известны;
- результаты scan отражаются в `jobs.global_scan.result.v1`.

### Ignore rules

- правила импортируются из `.t-helper.ignore` и хранятся в `ignore_rules`;
- отрицательные шаблоны `!pattern` сохраняются без потери данных;
- в MVP применяются только exclude-only semantics.

## Изменения в модели данных

- реализовать `root_paths`;
- реализовать `projects`;
- при необходимости расширить `repositories` минимально для registry режима;
- реализовать `ignore_rules`.

## Изменения в payload schemas

- `jobs.global_scan.payload.v1`
- `jobs.global_scan.result.v1`

## Изменения в API

- `GET /api/root-paths`
- `PUT /api/root-paths`
- `POST /api/scans`
- `GET /api/scans/{job_id}`
- `GET /api/projects`
- `GET /api/projects/{id}`
- `GET /api/ignore-rules`
- `PUT /api/ignore-rules`

## Тестирование

- fixture-тесты из `docs/test-plan.md`:
- `basic_tf_project`
- `nested_tf_project`
- `git_repo_marker_directory`
- `git_repo_marker_file`
- `ignored_directory`
- `negative_ignore_pattern`
- тесты идемпотентности повторного scan;
- тесты производительности на ограниченном worker pool;
- тесты корректной остановки обхода после обнаружения Terraform working directory.

## Критерии приёмки

- MVP 1
- MVP 2
- MVP 3
- MVP 4
- MVP 5
- MVP 15 частично на уровне связей registry
- MVP 21 в части roots/projects/jobs

## Риски этапа

- ошибочная нормализация путей и дубли карточек проектов;
- чрезмерное число `stat/open` операций при большом дереве;
- неправильная обработка `.git` file для worktree/submodule.

## Артефакты поставки

- модуль `global-scanner`;
- registry API для root paths, scans, projects и ignore rules;
- набор scan fixtures и acceptance tests;
- документация по алгоритму discovery и ограничениям MVP matcher.
