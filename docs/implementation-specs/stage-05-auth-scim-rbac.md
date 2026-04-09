# ТЗ: Этап 5. Auth, SCIM, RBAC

## Цель

Ввести отдельный модуль `auth`, обеспечивающий аутентификацию, авторизацию, SCIM и аудит security-событий для backend API.

## Результат этапа

Система должна поддерживать локальную аутентификацию, расширяемую модель внешних auth providers, пользователей, группы, роли, role bindings, enforcement в API и базовые SCIM контракты.

## In Scope

- модуль `auth`;
- локальная аутентификация;
- каркас внешних auth providers;
- `users`, `groups`, `memberships`;
- `roles`, `permissions`, `role_bindings`;
- API authorization enforcement;
- `SCIM`;
- `audit_log` для security-событий.

## Out Of Scope

- полный administrative UI;
- enterprise IAM integrations сверх согласованного минимального набора providers;
- детальная policy authoring UI.

## Зависимости

- завершённый этап 1;
- рабочий backend API, jobs, module lifecycle;
- желательно завершённые этапы 2-4, чтобы RBAC накрывал реальные доменные операции.

## Основные требования к реализации

### RBAC model

- поддержать системные и объектные scope из `docs/access-control.md`;
- поддержать системные роли и объектные роли;
- runtime authorization не должен полагаться на wildcard string matching;
- effective permissions вычисляются из объединения системных и объектных bindings.

### API enforcement

- каждый endpoint должен проверяться по authorization matrix;
- `system.runtime.read` не должен автоматически раскрывать object-scoped details;
- отрицательные authorization tests обязательны.

### SCIM and audit

- должны быть доступны endpoints чтения identities и запуска sync;
- security-значимые действия должны писать записи в `audit_log`.

## Изменения в модели данных

- `users`
- `groups`
- `group_members`
- `permissions`
- `roles`
- `role_permissions`
- `role_bindings`
- `scim_identities`
- `audit_log`

## Изменения в API

- `GET/PUT /api/auth/users`
- `GET/PUT /api/auth/groups`
- `GET/PUT /api/auth/roles`
- `GET/PUT /api/auth/role-bindings`
- `GET /api/auth/scim/identities`
- `POST /api/auth/scim/sync`
- `GET /api/audit`

## Изменения в payload schemas

- `jobs.scim_sync.payload.v1`
- `jobs.scim_sync.result.v1`

## Тестирование

- positive/negative authorization tests по всей матрице;
- tests на inheritance прав группы;
- tests на `system` vs object scope precedence;
- SCIM sync tests;
- audit trail tests по auth/rbac/scim операциям.

## Критерии приёмки

- MVP 16
- MVP 17

## Риски этапа

- слишком позднее внедрение RBAC приведёт к переделке ранних API handlers;
- неоднозначность в precedence системных и объектных ролей;
- выбор внешних auth providers остаётся открытым продуктовым решением.

## Артефакты поставки

- модуль `auth`;
- persistence model для RBAC и SCIM;
- API enforcement middleware;
- набор authorization и audit tests.
