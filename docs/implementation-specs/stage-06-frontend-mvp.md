# ТЗ: Этап 6. Frontend MVP

## Цель

Поставить единый frontend-контур из `Web UI` и локального `GUI`, работающих поверх одного backend API и покрывающих MVP read/operate сценарии.

## Результат этапа

Пользователь должен иметь возможность через UI просматривать проекты, environments, workspaces, findings, jobs, module states, конфигурацию и запускать ключевые операции сканирования и repository management.

## In Scope

- `Web UI`;
- локальный `GUI`;
- единая frontend codebase;
- screens и flows для MVP read/operate сценариев;
- интеграция только с documented backend API.

## Out Of Scope

- полный administrative UI для auth/RBAC/SCIM;
- UI редактирования configuration и security rule sets;
- frontend-only backend endpoints;
- удалённый доступ к `GUI`.

## Зависимости

- завершённые этапы 1-5;
- полный backend API для scans, repositories, jobs, environments, workspaces, modules, findings и config.

## Основные требования к реализации

### Frontend coverage

- просмотр проектов, environments и workspaces;
- управление scan roots и ignore rules;
- запуск global scan и project scan;
- управление clone/pull/sync;
- просмотр findings, jobs, module states, audit log и configuration.

### Architectural rules

- `GUI` и `Web UI` используют один backend API;
- `GUI` работает только локально;
- frontend не должен зависеть от недокументированных endpoint'ов.

### UX requirements

- long-running operations отображают `job_id`, статус и итог;
- clone workflow должен показывать protocol selector рядом с URL field;
- UI должен уметь работать с scoped findings и общим security view.

## Изменения в API

Новых обязательных backend endpoint'ов этап не вводит. Этап зависит от полноты уже задокументированного API.

## Тестирование

- contract tests: frontend использует только documented API;
- e2e-тесты read/operate сценариев MVP;
- тесты локальности `GUI`;
- smoke-тесты авторизации и отображения доступа в UI.

## Критерии приёмки

- MVP 11
- MVP 12
- MVP 15 в UI read model
- MVP 21

## Риски этапа

- попытка компенсировать пробелы backend контрактов frontend-обходами;
- расхождение между `GUI` и `Web UI` по поддерживаемым сценариям;
- слишком ранний старт frontend до стабилизации этапов 3-5.

## Артефакты поставки

- единая frontend codebase;
- `Web UI`;
- локальный `GUI`;
- e2e и contract test suite для MVP сценариев.
