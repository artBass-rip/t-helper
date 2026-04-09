# ТЗ: Этап 8. Distributed Deployment

## Цель

Подготовить систему к multi-node и HA deployment без ломки доменных контрактов и без расхождения с монолитным режимом.

## Результат этапа

Ключевые модули должны быть способны работать как отдельные процессы или узлы с согласованным межмодульным взаимодействием, общей моделью jobs, health и security.

## In Scope

- вынос `global-scanner`;
- вынос `project-scanner`;
- вынос `repository-manager`;
- вынос `security-validator`;
- вынос `auth`;
- согласование межмодульного взаимодействия;
- подготовка deployment topology для multi-node и HA.

## Out Of Scope

- изменение продуктовых сценариев MVP;
- обходные контракты только для distributed режима;
- введение второго источника истины для конфигурации.

## Зависимости

- завершённые этапы 1-7;
- hardened runtime, observability и authorization model;
- стабильные payload schemas и межмодульные контракты.

## Основные требования к реализации

### Contract compatibility

- монолитный и распределённый режим должны использовать совместимую модель взаимодействия;
- jobs, locks, payload schemas и module states остаются едиными;
- межмодульные вызовы не должны ломать существующий API contract.

### Operational requirements

- определить service discovery и health model;
- определить retry, timeout и idempotency policy между модулями;
- определить ownership фоновых очередей и locks в multi-node режиме;
- подготовить HA deployment для backend-модулей и БД.

### Security requirements

- межмодульная аутентификация и авторизация должны быть формализованы;
- auditability распределённых операций должна сохраняться.

## Изменения в API и модели данных

Изменения должны быть минимальными и совместимыми. Основной фокус этапа не на пользовательском API, а на runtime contracts между модулями.

## Тестирование

- integration-тесты разнесённых модулей;
- failover tests;
- tests на повторную доставку сообщений и идемпотентность jobs;
- deployment validation для multi-node и HA топологий.

## Критерии приёмки

- Platform 5

## Риски этапа

- вынос модулей до завершения hardening приведёт к экспоненциальному росту сложности;
- неформализованные межмодульные контракты создадут расхождение между монолитом и distributed режимом;
- возможны гонки вокруг jobs и locks в multi-node среде.

## Артефакты поставки

- distributed runtime architecture package;
- deployment manifests/инструкции;
- межмодульные contracts;
- failover и HA test suite.
