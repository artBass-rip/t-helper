# Auth, SCIM и RBAC

## Обязательные возможности модуля `auth`

- локальная аутентификация;
- расширяемая модель внешних authentication providers;
- `SCIM`;
- `RBAC`;
- управление users, groups, roles и role bindings;
- audit security-событий.

## Local password policy

Local auth uses Argon2id PHC password hashes as defined in `docs/adr/0014-local-password-hashing.md`.

API responses, logs, job payloads, workflow payloads and audit payloads must never expose raw passwords, password hashes, reset tokens or reset token hashes. Authentication failures must use generic errors and must not reveal whether a username exists.

Runtime auth endpoints are separate from administrative auth endpoints. Login, logout, session, password reset and password change flows operate on session/local credential state; users/groups/roles/role-bindings/SCIM endpoints remain administrative APIs.

## Scope-модель

Поддерживаемые scope:

- `system`
- `project`
- `environment`
- `workspace`
- `repository`

Субъекты назначения ролей:

- `user`
- `group`

Правила разрешения:

- системная роль имеет приоритет над объектной;
- permissions нескольких ролей объединяются;
- базовая модель строится на `allow` без явного `deny`;
- права группы наследуются её участниками.

## Системные роли

- `system_admin` - полный доступ ко всей системе
- `platform_operator` - scan, runtime, config, module operations, repo sync, security scan
- `security_admin` - auth, RBAC, `SCIM`, security rule sets и findings
- `auditor` - расширенный read-only по объектам, config, jobs, audit и findings
- `viewer` - базовый глобальный read-only

## Объектные роли

- `owner` - полный доступ к объекту, delete, role binding management
- `maintainer` - operational access без управления RBAC
- `editor` - изменение метаданных без delete и без RBAC management
- `viewer` - read-only доступ

## Role-to-permission matrix

Wildcard permissions вида `project.*` используются в документации как сокращение. В persistent storage и seed/migration они должны разворачиваться в конкретные permissions из раздела "Machine-readable permissions"; runtime authorization не должен полагаться на строковое сопоставление wildcard.

### Системные роли

| Role | Permissions |
| --- | --- |
| `system_admin` | все `system.*`, `auth.*`, `security.*`, `project.*`, `environment.*`, `workspace.*`, `repository.*` |
| `platform_operator` | `system.config.read`, `system.globalscan.run`, `system.rootpath.*`, `system.ignore.*`, `system.module.*`, `system.job.read`, `system.status.read`, `system.audit.read`, `system.runtime.*`, `security.scan.*`, `security.finding.read`, `project.read`, `project.job.read`, `project.scan.*`, `environment.read`, `workspace.read`, `repository.read`, `repository.job.read`, `repository.provider.*`, `repository.credential.*`, `repository.clone`, `repository.pull`, `repository.sync`, `repository.webhook.write`, `repository.polling.write` |
| `security_admin` | `system.audit.read`, `system.job.read`, `auth.*`, `security.*`, `project.read`, `project.scan.read`, `environment.read`, `workspace.read`, `repository.read` |
| `auditor` | `system.config.read`, `system.security.read`, `system.module.read`, `system.job.read`, `system.status.read`, `system.audit.read`, `system.runtime.read`, `auth.user.read`, `auth.group.read`, `auth.role.read`, `auth.rolebind.read`, `auth.provider.read`, `auth.scim.read`, `security.ruleset.read`, `security.toolprofile.read`, `security.finding.read`, `security.scan.read`, `security.exception.read`, `project.read`, `project.job.read`, `project.log.read`, `project.scan.read`, `environment.read`, `environment.job.read`, `environment.log.read`, `workspace.read`, `workspace.job.read`, `workspace.log.read`, `repository.read`, `repository.job.read`, `repository.log.read` |
| `viewer` | `system.runtime.read`, `project.read`, `environment.read`, `workspace.read`, `repository.read` |

### Объектные роли

Permissions объектных ролей применяются только внутри scope, к которому привязан role binding. Например, `maintainer` на `repository:<id>` выдаёт перечисленные repository permissions только для этого репозитория.

| Scope | `owner` | `maintainer` | `editor` | `viewer` |
| --- | --- | --- | --- | --- |
| `project` | `project.*` | `project.read`, `project.write`, `project.job.read`, `project.log.read`, `project.scan.*`, `project.relation.write` | `project.read`, `project.write`, `project.relation.write` | `project.read` |
| `environment` | `environment.*` | `environment.read`, `environment.write`, `environment.job.read`, `environment.log.read`, `environment.relation.write` | `environment.read`, `environment.write`, `environment.relation.write` | `environment.read` |
| `workspace` | `workspace.*` | `workspace.read`, `workspace.write`, `workspace.job.read`, `workspace.log.read`, `workspace.relation.write` | `workspace.read`, `workspace.write`, `workspace.relation.write` | `workspace.read` |
| `repository` | `repository.*` | `repository.read`, `repository.write`, `repository.job.read`, `repository.log.read`, `repository.clone`, `repository.pull`, `repository.sync`, `repository.webhook.write`, `repository.polling.write` | `repository.read`, `repository.write` | `repository.read` |

## Machine-readable permissions

### System

```text
system.config.read
system.config.write
system.security.read
system.security.write
system.globalscan.run
system.rootpath.read
system.rootpath.write
system.ignore.read
system.ignore.write
system.module.read
system.module.reload
system.module.restart
system.job.read
system.status.read
system.audit.read
system.runtime.read
system.runtime.write
```

### Auth

```text
auth.user.read
auth.user.write
auth.group.read
auth.group.write
auth.role.read
auth.role.write
auth.rolebind.read
auth.rolebind.write
auth.provider.read
auth.provider.write
auth.scim.read
auth.scim.write
```

### Security

```text
security.ruleset.read
security.ruleset.write
security.toolprofile.read
security.toolprofile.write
security.toolprofile.analyze
security.finding.read
security.finding.write
security.scan.run
security.scan.read
security.scan.write
security.exception.read
security.exception.write
```

### Project

```text
project.read
project.write
project.delete
project.rolebind.read
project.rolebind.write
project.job.read
project.log.read
project.scan.run
project.scan.read
project.scan.write
project.relation.write
```

### Environment

```text
environment.read
environment.write
environment.delete
environment.rolebind.read
environment.rolebind.write
environment.job.read
environment.log.read
environment.relation.write
```

### Workspace

```text
workspace.read
workspace.write
workspace.delete
workspace.rolebind.read
workspace.rolebind.write
workspace.job.read
workspace.log.read
workspace.relation.write
```

### Repository

```text
repository.read
repository.write
repository.delete
repository.rolebind.read
repository.rolebind.write
repository.job.read
repository.log.read
repository.clone
repository.pull
repository.sync
repository.provider.read
repository.provider.write
repository.credential.read
repository.credential.write
repository.webhook.write
repository.polling.write
```

## Минимальные правила авторизации API

- чтение требует `viewer` и выше в системном или объектном scope;
- изменение требует `editor` и выше либо соответствующий system permission;
- hard delete требует `owner` или `system_admin` only after a future endpoint explicitly implements delete semantics;
- управление role bindings требует `security_admin`, `system_admin` или `owner` в объектном scope;
- runtime-операции требуют `platform_operator` или `system_admin`;
- `SCIM`-операции требуют `security_admin` или `system_admin`.
- `system.runtime.read` разрешает читать runtime summary и списочные metadata, но не должен автоматически раскрывать все object-scoped детали без соответствующего object permission или отдельного системного read permission.
- Confirmed MVP behavior: `GET /api/health` доступен без аутентификации только как safe readiness/discovery endpoint и не раскрывает config, filesystem paths, DSN, users, secrets или object-scoped details.
- Confirmed MVP behavior: public `DELETE` endpoints are out of scope; seeded `*.delete` permissions are reserved for future lifecycle endpoints.
- Confirmed MVP lifecycle behavior: disable/deactivate/mark-missing/supersede operations are write operations controlled by the corresponding `*.write`, runtime or specialized operation permissions, not by reserved `*.delete` permissions.

### `system.runtime.read` visibility examples

`system.runtime.read` may expose:

- aggregate runtime readiness and module state names;
- counts of active jobs, projects, repositories and findings;
- worker status summary without host-sensitive diagnostics;
- list metadata needed for dashboards.

`system.runtime.read` must not expose by itself:

- filesystem paths such as `root_paths.path`, `repositories.local_path` or log paths;
- DSNs, database paths, provider credentials, full `secretref://env/NAME` values or resolved secrets;
- repository full details for objects where the subject lacks `repository.read`;
- project/workspace/environment object details where the subject lacks the corresponding object permission;
- job payloads, result payloads or event payloads that contain object-scoped diagnostics.

Examples:

- `viewer` with only `system.runtime.read` may see "12 active projects" but not their filesystem paths.
- `auditor` with `system.job.read` may read job metadata and sanitized payloads, but still receives masked secret refs.
- `platform_operator` may run global scans and module operations, but object-scoped repository details still require `repository.read` or an appropriate system permission.

## API authorization matrix

| Endpoint | Permission |
| --- | --- |
| `GET /api/health` | unauthenticated safe metadata only |
| `GET /api/root-paths` | `system.rootpath.read` |
| `PUT /api/root-paths` | `system.rootpath.write` |
| `GET /api/projects` | `project.read` or `system.runtime.read` |
| `GET /api/projects/{id}` | `project.read` on project |
| `GET /api/projects/{id}/scan-settings` | `project.scan.read` on project or `security.scan.read` |
| `PUT /api/projects/{id}/scan-settings` | `project.scan.write` on project or `security.scan.write` |
| `GET /api/environments` | `environment.read` or `system.runtime.read` |
| `GET /api/environments/{id}` | `environment.read` on environment |
| `GET /api/workspaces` | `workspace.read` or `system.runtime.read` |
| `GET /api/workspaces/{id}` | `workspace.read` on workspace |
| `POST /api/scans` | `system.globalscan.run` |
| `GET /api/scans/{job_id}` | `system.job.read` |
| `POST /api/project-scans` | `project.scan.run` on project or `security.scan.run` |
| `GET /api/project-scans/{project_scan_id}` | `project.scan.read` on project or `security.scan.read` |
| `GET /api/project-scans/{project_scan_id}/findings` | `security.finding.read` or `project.scan.read` on project |
| `GET /api/jobs` | `system.job.read` or object-scoped `*.job.read` |
| `GET /api/jobs/{id}` | `system.job.read` or object-scoped `*.job.read` |
| `GET /api/status` | `system.status.read` or `system.runtime.read` |
| `GET /api/status/workflows` | `system.status.read` or object-scoped `*.job.read` |
| `GET /api/status/workflows/{job_group_id}` | `system.status.read` or object-scoped `*.job.read` |
| `GET /api/status/jobs/{job_id}` | `system.status.read` or object-scoped `*.job.read` |
| `GET /api/status/workers` | `system.status.read` or `system.runtime.read` |
| `GET /api/config` | `system.config.read` |
| `PUT /api/config` | `system.config.write` |
| `GET /api/ignore-rules` | `system.ignore.read` |
| `PUT /api/ignore-rules` | `system.ignore.write` |
| `GET /api/repos` | `repository.read` or `system.runtime.read` |
| `GET /api/repos/{id}` | `repository.read` on repository |
| `GET /api/repo-provider-instances` | `repository.provider.read` or `repository.read` |
| `PUT /api/repo-provider-instances` | `repository.provider.write` |
| `GET /api/repo-credentials` | `repository.credential.read` |
| `PUT /api/repo-credentials` | `repository.credential.write` |
| `POST /api/repos/clone` | `repository.clone` or `system.runtime.write` |
| `POST /api/repos/pull` | `repository.pull` or `system.runtime.write` |
| `POST /api/repos/sync` | `repository.sync` or `system.runtime.write` |
| `GET /api/security/findings` | `security.finding.read` |
| `GET /api/security/findings/{id}` | `security.finding.read` |
| `GET /api/security/rule-sets` | `security.ruleset.read` |
| `PUT /api/security/rule-sets` | `security.ruleset.write` |
| `GET /api/tool-profiles` | `security.toolprofile.read` |
| `POST /api/tool-profiles/validate` | `security.toolprofile.write` |
| `POST /api/tool-profiles/import` | `security.toolprofile.write` |
| `POST /api/tool-profiles/activate` | `security.toolprofile.write` |
| `POST /api/tool-profiles/analyze` | `security.toolprofile.analyze` |
| `GET /api/modules` | `system.module.read` |
| `POST /api/modules/reload` | `system.module.reload` |
| `POST /api/modules/restart` | `system.module.restart` |
| `POST /api/auth/login` | unauthenticated; creates session only after successful local/external auth |
| `POST /api/auth/logout` | authenticated session |
| `GET /api/auth/session` | authenticated current session |
| `POST /api/auth/password-reset/request` | unauthenticated generic response |
| `POST /api/auth/password-reset/confirm` | unauthenticated token-bound operation |
| `POST /api/auth/password/change` | authenticated current local user |
| `GET /api/auth/users` | `auth.user.read` |
| `PUT /api/auth/users` | `auth.user.write` |
| `GET /api/auth/groups` | `auth.group.read` |
| `PUT /api/auth/groups` | `auth.group.write` |
| `GET /api/auth/roles` | `auth.role.read` |
| `PUT /api/auth/roles` | `auth.role.write` |
| `GET /api/auth/role-bindings` | `auth.rolebind.read` |
| `PUT /api/auth/role-bindings` | `auth.rolebind.write` |
| `GET /api/auth/scim/identities` | `auth.scim.read` |
| `POST /api/auth/scim/sync` | `auth.scim.write` |
| `GET /api/audit` | `system.audit.read` |

## SQL-ориентированные инварианты

Для SQL-реализации должны соблюдаться ограничения:

- `subject_type` принимает только `user` или `group`;
- `scope_type` принимает только `system`, `project`, `environment`, `workspace`, `repository`;
- для `system` scope `scope_id = NULL`;
- для объектных scope `scope_id` обязателен;
- `scim_identities` ссылается либо на `user_id`, либо на `group_id`.

Рекомендации:

- в `PostgreSQL` использовать `CHECK CONSTRAINT`;
- в `MySQL`, `MSSQL` и `SQLite` дублировать критичную валидацию на уровне приложения, если конкретный constraint нельзя надёжно выразить средствами dialect;
- effective permissions кэшировать после объединения системных и объектных bindings.
