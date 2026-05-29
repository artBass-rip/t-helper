# Технологический стек

Документ фиксирует базовый стек реализации `t-helper` для code scaffolding и последующих этапов roadmap.

## Backend

Backend реализуется на `Go`.

Область применения:

- runtime service `thelper`;
- worker process `thelper-worker`;
- administrative CLI `thelper-ctl`;
- backend HTTP API;
- module lifecycle и jobs framework;
- status-monitor и aggregate status read models;
- storage abstraction и adapters;
- orchestration локальных toolchain-команд для scanning и repository operations.

Требования к backend-реализации:

- доменная логика не должна зависеть от конкретного storage backend;
- provider-specific integrations must be isolated in pluggable modules/libraries behind internal interfaces;
- API contracts должны соответствовать `docs/api.md`;
- CLI contracts должны соответствовать `docs/interfaces.md`;
- background jobs должны использовать documented payload schemas из `docs/payload-schemas.md`;
- outbound calls для security stack не допускаются, кроме явно настроенных repository/provider integrations.

## Storage

MVP storage stack:

- `SQLite` - default internal SQL-like storage for local setup;
- `PostgreSQL` - default external storage for server setup.

Platform storage targets:

- `SQLite`;
- `PostgreSQL`;
- `MySQL`;
- `MSSQL`.

Managed engine compatibility:

- Aurora PostgreSQL through the PostgreSQL adapter;
- Aurora MySQL through the MySQL adapter;
- Babelfish for Aurora PostgreSQL is not a substitute for the native MSSQL adapter.

Требования:

- оба MVP storage backend используют SQL-like модель и поддерживают миграции;
- логическая модель данных и storage interfaces едины для всех supported adapters;
- SQL migrations используют dialect-specific SQL при синхронизированных logical migration versions;
- `MySQL` и `MSSQL` добавляются на Stage 10 Storage Adapter Expansion как first-class SQL adapters.

Database providers are implemented as pluggable storage adapter libraries selected by configuration. Provider-specific SQL, connection handling and health checks must not leak into domain services or HTTP handlers.

## Jobs и Workers

Background jobs выполняются отдельными worker-процессами.

Базовые компоненты:

- `thelper` - API/runtime process, создаёт jobs, управляет конфигурацией, module states и HTTP API;
- `thelper-worker` - отдельный worker process, atomically claims queued jobs through storage-level leases, выполняет job handlers и сохраняет result payload;
- `thelper-ctl` - administrative CLI, создаёт lifecycle/config jobs или вызывает documented backend API.

Инварианты:

- API/CLI не выполняют long-running jobs inline;
- jobs persisted в БД и имеют documented payload/result schemas;
- job leases определяют ownership конкретного job worker-процессом;
- `job_locks` сериализуют конфликтующие бизнес-операции между worker-процессами;
- workers поддерживают heartbeat, lease expiry recovery и retry/backoff;
- jobs публикуют status events/metrics, которые агрегирует `status-monitor`;
- UI и внутренние сервисы читают aggregate status из status-monitor read models;
- несколько worker-процессов могут работать параллельно, если их lock keys не конфликтуют;
- distributed deployment расширяет эту модель, но не меняет storage contracts jobs.

## Frontend

Единый frontend-контур реализуется на:

- `React`;
- `TypeScript`;
- `Vite`;
- `TanStack Router`;
- `TanStack Query`;
- `Zod`;
- `React Hook Form`;
- `Ant Design`.

Область применения:

- `Web UI`;
- shared UI codebase для локального `GUI`;
- read/operate сценарии MVP;
- full MVP administrative UI delivered in Stage 08, with Stage 12 reserved for admin hardening and platform-only administrative extensions.

Требования к frontend-реализации:

- `Web UI` и `GUI` используют один documented backend API;
- frontend не вводит и не требует frontend-only backend endpoints;
- API DTO валидируются на границе клиента через typed schemas;
- long-running operations отображаются через `jobs` и documented job references;
- UI должен быть пригоден для плотных operational сценариев: таблицы, фильтры, формы, статусы, findings, RBAC и audit views.
- route map, navigation model and operational density rules must follow [`frontend-ui-contract.md`](frontend-ui-contract.md).

## Desktop GUI

Локальный desktop GUI реализуется на `Tauri` с использованием той же React/TypeScript codebase.

Требования к GUI:

- `GUI` работает только локально;
- удалённый доступ к `GUI` не поддерживается;
- взаимодействие с системой идёт через тот же backend API, что и `Web UI`;
- GUI-specific поведение не должно менять backend contracts;
- Tauri packaging/signing and local runtime discovery policy must follow [`frontend-ui-contract.md`](frontend-ui-contract.md).

## Singleton Runtime Policy

В одной локальной установке может быть запущен только один экземпляр `t-helper` runtime.

Правила:

- если `thelper` уже запущен для `Web UI`, Tauri GUI подключается к существующему local runtime;
- если `thelper` ещё не запущен, Tauri GUI запускает local `thelper`, после чего `Web UI` подключается к этому же runtime;
- повторный запуск `thelper` должен обнаруживать существующий runtime через lock/health mechanism и завершаться без создания второго активного процесса;
- `Web UI` и `GUI` всегда работают с одним backend API и одной runtime БД.

## Stack Invariants

- Backend: `Go`.
- Web frontend: `React + TypeScript + Vite`.
- Routing: `TanStack Router`.
- Server state: `TanStack Query`.
- Runtime/client validation: `Zod`.
- Forms: `React Hook Form`.
- UI component system: `Ant Design`.
- Desktop shell: `Tauri`.
- Internal local storage: `SQLite`.
- External server storage: `PostgreSQL`.
- Platform storage targets: `SQLite`, `PostgreSQL`, `MySQL`, `MSSQL`.
- Background execution: separate `thelper-worker` processes.
