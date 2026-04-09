# Трассировка требований

Этот документ связывает продуктовые требования с API, моделью данных, payload schemas, permissions, этапами roadmap, test plan и критериями приёмки. Он должен обновляться вместе с `requirements.md`, `interfaces.md`, `api.md`, `payload-schemas.md`, `data-model.md`, `access-control.md` и `test-plan.md`.

| Capability | Requirements | API | Data model | Permissions | Roadmap | Acceptance |
| --- | --- | --- | --- | --- | --- | --- |
| Global scan | `requirements.md` -> "Глобальное сканирование" | `POST /api/scans`, `GET /api/scans/{job_id}` | `root_paths`, `projects`, `repositories`, `jobs` | `system.globalscan.run`, `system.job.read` | Этап 2 | MVP 1-5 |
| Root paths | `requirements.md` -> "Глобальное сканирование" | `GET /api/root-paths`, `PUT /api/root-paths` | `root_paths`, `config_entries` | `system.rootpath.read`, `system.rootpath.write` | Этап 2 | MVP 21 |
| Ignore rules | `requirements.md` -> "Исключения при сканировании" | `GET /api/ignore-rules`, `PUT /api/ignore-rules` | `ignore_rules` | `system.ignore.read`, `system.ignore.write` | Этап 2, Этап 7 | MVP 4, Platform 4 |
| Project registry | `requirements.md` -> "Регистрация объектов" | `GET /api/projects`, `GET /api/projects/{id}` | `projects`, `environments`, `workspaces`, `repositories` | `project.read`, `project.write`, `project.relation.write` | Этап 2 | MVP 5, 15 |
| Environments and workspaces | `requirements.md` -> "Frontend" | `GET /api/environments`, `GET /api/environments/{id}`, `GET /api/workspaces`, `GET /api/workspaces/{id}` | `environments`, `workspaces` | `environment.read`, `workspace.read` | Этап 6 | MVP 15, 21 |
| Repository operations | `requirements.md` -> "Работа с репозиториями" | `GET /api/repos`, `GET /api/repos/{id}`, `POST /api/repos/clone`, `POST /api/repos/pull`, `POST /api/repos/sync` | `repositories`, `jobs`, `job_locks`, `root_paths` | `repository.read`, `repository.clone`, `repository.pull`, `repository.sync` | Этап 3 | MVP 14, 21, 22 |
| Project scan (`terraform validate`, `TFLint`) | `requirements.md` -> "Базовый стек сканирования", "Проектное Terraform-сканирование" | `POST /api/project-scans`, `GET /api/project-scans/{project_scan_id}`, `GET/PUT /api/projects/{id}/scan-settings` | `project_scans`, `project_scan_settings`, `jobs`, `security_rule_sets` | `project.scan.run`, `project.scan.read`, `project.scan.write`, `security.scan.run`, `security.scan.read` | Этап 4 | MVP 6, 20 |
| Security validation scan (`Trivy`, `Gitleaks`, `Checkov`, `OPA`/`Conftest`) | `requirements.md` -> "Базовый стек сканирования", "Сканирование безопасности и валидация кода" | `POST /api/project-scans`, `GET/PUT /api/projects/{id}/scan-settings`, `GET /api/project-scans/{project_scan_id}/findings` | `project_security_scan_settings`, `project_scans`, `security_findings`, `jobs` | `security.scan.run`, `security.scan.read`, `security.scan.write`, `security.finding.read` | Этап 4, Этап 7 | MVP 6, 18-20, Platform 3 |
| Findings | `requirements.md` -> "Сканирование безопасности и валидация кода" | `GET /api/project-scans/{project_scan_id}/findings`, `GET /api/security/findings`, `GET /api/security/findings/{id}` | `security_findings`, `security_rule_sets` | `security.finding.read`, `security.finding.write` | Этап 4 | MVP 18-20 |
| Security rule sets | `requirements.md` -> "Сканирование безопасности и валидация кода" | `GET /api/security/rule-sets`, `PUT /api/security/rule-sets` | `security_rule_sets` | `security.ruleset.read`, `security.ruleset.write` | Этап 4, Этап 7 | MVP 19, Platform 3 |
| Config lifecycle | `requirements.md` -> "Конфигурация и lifecycle" | `GET /api/config`, `PUT /api/config`, `POST /api/modules/reload`, `POST /api/modules/restart` | `config_entries`, `module_states`, `jobs` | `system.config.read`, `system.config.write`, `system.module.reload`, `system.module.restart` | Этап 1 | MVP 7-10 |
| Jobs visibility | `requirements.md` -> "Frontend" and "Надёжность" | `GET /api/jobs`, `GET /api/jobs/{id}` | `jobs`, `job_locks` | `system.job.read`, object-scoped `*.job.read` | Этап 1-4 | MVP 14, 21 |
| Module states | `requirements.md` -> "Конфигурация и lifecycle" | `GET /api/modules` | `module_states` | `system.module.read` | Этап 1 | MVP 10, 21 |
| Auth and RBAC | `requirements.md` -> "Безопасность" | `GET/PUT /api/auth/users`, `GET/PUT /api/auth/groups`, `GET/PUT /api/auth/roles`, `GET/PUT /api/auth/role-bindings` | `users`, `groups`, `group_members`, `permissions`, `roles`, `role_permissions`, `role_bindings` | `auth.*`, object-scoped role permissions | Этап 5 | MVP 16-17, Platform 2 |
| SCIM | `requirements.md` -> "Frontend" and "Безопасность" | `GET /api/auth/scim/identities`, `POST /api/auth/scim/sync` | `scim_identities`, `users`, `groups` | `auth.scim.read`, `auth.scim.write` | Этап 5, Этап 7 | MVP 17, Platform 2 |
| Audit | `requirements.md` -> "Надёжность" and "Безопасность" | `GET /api/audit` | `audit_log` | `system.audit.read` | Этап 5 | MVP 17-19 |
| Storage adapters | `architecture.md` -> "Storage strategy" | Storage abstraction, not public API | all persistent entities | n/a | Этап 1, Этап 7 | MVP 13, Platform 1 |
| Distributed deployment | `architecture.md` -> "Сценарии развёртывания" | compatible inter-module contracts | module-specific persistent state | module/runtime permissions | Этап 8 | Platform 5 |

## Правила поддержки трассировки

- Новое функциональное требование должно иметь хотя бы один API или явное объяснение, почему API не нужен.
- Новый write endpoint должен иметь permission и либо идемпотентное поведение, либо `job_id`.
- Новая persistent сущность должна иметь индексы/уникальность или явное объяснение, почему они не требуются.
- Новый roadmap acceptance criterion должен ссылаться на capability из таблицы.
- Новый API endpoint должен быть добавлен в `interfaces.md`, `api.md` и API authorization matrix в `access-control.md`.
- Новый job type должен быть добавлен в `data-model.md`, `payload-schemas.md` и `test-plan.md`.
