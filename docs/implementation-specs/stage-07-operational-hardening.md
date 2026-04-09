# ТЗ: Этап 7. Operational Hardening

## Цель

Довести систему от MVP до эксплуатационно устойчивой platform release с расширенным storage stack, hardening runtime и административными сценариями.

## Результат этапа

Система должна получить дополнительные storage adapters, observability, hardened scheduler/runtime, full `.gitignore` semantics, расширенные локальные security rules и administrative UI для auth/RBAC/SCIM, configuration и security rule sets.

## In Scope

- adapters для `MySQL`, `SQLite`, `MongoDB`;
- runtime hardening;
- observability;
- scheduler hardening;
- локальные rule sets и enterprise-policy packs;
- full `.gitignore` semantics;
- UI для auth/RBAC/SCIM;
- UI для configuration и security rule sets.

## Out Of Scope

- multi-node orchestration и распределённый deployment;
- изменение базовой MVP-доменной модели без веской причины.

## Зависимости

- завершённые этапы 1-6;
- стабильный auth backend и frontend MVP.

## Основные требования к реализации

### Storage expansion

- общий storage contract должен быть повторно пройден для `MySQL`, `SQLite`, `MongoDB`;
- platform release не считается завершённым без поддержки всех БД из roadmap.

### Runtime and scheduler hardening

- улучшить диагностику деградации модулей;
- ввести наблюдаемость jobs, locks, module lifecycle и scan execution;
- усилить отказоустойчивость scheduler и фоновoй обработки.

### Security and policy hardening

- определить стартовый набор локальных rule sets;
- обеспечить подключение enterprise-policy packs через `OPA`/`Conftest`;
- реализовать полный matcher `.gitignore`, включая `!pattern`.

### Administrative UI

- добавить UI для auth/RBAC/SCIM;
- добавить UI редактирования configuration;
- добавить UI редактирования/регистрации security rule sets.

## Изменения в API и модели данных

Базовые контракты уже заданы. На этапе допускаются расширения без нарушения совместимости:

- observability/read endpoints;
- расширение metadata для rule sets, jobs и module states;
- backend support для full `.gitignore` semantics.

## Тестирование

- расширенный storage compatibility suite;
- нагрузочные тесты scheduler и job queues;
- chaos/failure tests для module runtime;
- acceptance tests полного `.gitignore` matcher;
- e2e-тесты administrative UI.

## Критерии приёмки

- Platform 1
- Platform 2
- Platform 3
- Platform 4

## Риски этапа

- сильное расширение объёма этапа за счёт одновременного hardening backend и UI;
- разница поведения SQL и document storage backends;
- рост операционной сложности без формализованной observability-модели.

## Артефакты поставки

- дополнительные storage adapters;
- observability stack;
- hardened scheduler/runtime;
- administrative UI;
- full `.gitignore` matcher;
- расширенный platform acceptance suite.
