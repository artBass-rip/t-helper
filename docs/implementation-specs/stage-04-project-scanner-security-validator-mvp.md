# ТЗ: Этап 4. Project Scanner и Security Validator MVP

## Цель

Реализовать локальный анализ Terraform-проектов и security/validation checks как управляемый orchestration layer над локальными CLI-инструментами.

## Результат этапа

Система должна уметь запускать project-level scan по проекту, применять настройки проекта, запускать `terraform validate` и `TFLint`, подключать `Trivy`, `Gitleaks`, `Checkov`, а также опционально `OPA`/`Conftest`, и сохранять findings локально.

## In Scope

- модуль `project-scanner`;
- модуль `security-validator`;
- `project_scans`;
- `project_scan_settings`;
- `project_security_scan_settings`;
- `security_rule_sets`;
- `security_findings`;
- единый endpoint `POST /api/project-scans`;
- локальное хранение findings и scan summary.

## Out Of Scope

- administrative UI для rule sets;
- внешняя передача findings;
- новый отдельный API/сущность `security_scans`;
- distributed execution project/security scanners.

## Зависимости

- завершённые этапы 1, 2 и 3;
- стабильный registry проектов и репозиториев;
- рабочий jobs framework и module lifecycle.

## Основные требования к реализации

### Scan orchestration

- project-level scan запускается вручную, по расписанию, после `clone`, после `pull` или по внутреннему событию;
- глобальная конфигурация не содержит default-настроек project scan;
- подключаемые security modules выбираются из `scanning.security_scan.modules`;
- модуль не должен запускаться для проекта, если он не включён в project settings.

### Toolchain baseline

- `project-scanner` обязан запускать `terraform validate` и `TFLint`;
- `security-validator` обязан поддерживать `Trivy`, `Gitleaks`, `Checkov`;
- enterprise-policy checks подключаются через `OPA` и `Conftest`.

### Model and API rules

- единый `POST /api/project-scans` создаёт `jobs.job_type = project_scan` и запись в `project_scans`;
- findings читаются через scoped endpoint и global security endpoints;
- подробные findings лежат в `security_findings`, а summary в `project_scans.result_payload`.

### Security constraints

- код, findings, rule sets и policy packs не отправляются наружу;
- payload/result schemas не должны включать raw Terraform source, токены и ключи.

## Изменения в модели данных

- `project_scan_settings`
- `project_security_scan_settings`
- `project_scans`
- `security_rule_sets`
- `security_findings`

## Изменения в payload schemas

- `jobs.project_scan.payload.v1`
- `jobs.project_scan.result.v1`
- `jobs.security_validation_scan.payload.v1`
- `jobs.security_validation_scan.result.v1`
- `project_scans.result.v1`

## Изменения в API

- `GET /api/projects/{id}/scan-settings`
- `PUT /api/projects/{id}/scan-settings`
- `POST /api/project-scans`
- `GET /api/project-scans/{project_scan_id}`
- `GET /api/project-scans/{project_scan_id}/findings`
- `GET /api/security/findings`
- `GET /api/security/findings/{id}`
- `GET /api/security/rule-sets`
- `PUT /api/security/rule-sets`

## Тестирование

- e2e-тесты project scan на Terraform fixtures;
- тесты включения/выключения security modules на уровне проекта;
- тесты fingerprinting findings и обновления existing findings;
- тесты запуска после `clone`/`pull`, если включено в проектных настройках;
- negative-тесты отсутствия outbound calls в network-restricted окружении.

## Критерии приёмки

- MVP 6
- MVP 18
- MVP 19
- MVP 20

## Риски этапа

- смешение summary-результатов и детальных findings в одной модели;
- нестабильность внешних CLI toolchain версий;
- рост времени выполнения без отдельной очереди security jobs.

## Артефакты поставки

- модули `project-scanner` и `security-validator`;
- scan settings API;
- finding storage и rule set registry;
- локальный test harness для security stack.
