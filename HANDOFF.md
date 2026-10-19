# Передаточный документ

## Контекст задачи

В проекте `snippetbook` развивается локальное Go-приложение с двумя разделами:

- **SnippetBook** на `/` — существующая записная книжка для сниппетов, заметок и секретов;
- **Local Uptime Board** на `/uptime` — панель контроля сервисов, endpoint-проверок и uptime.
- **Настройки мониторинга** на `/uptime/settings` — timeout, история, уведомления и reminders.
- **Domains** на `/uptime/domain` — DNS, регистрация домена и MX TLS.

Стек: Go, Gin, SQLite через `modernc.org/sqlite`, vanilla JavaScript без frontend-фреймворка.
Главное требование — сохранять существующую функциональность SnippetBook и развивать Ops Board рядом с ней.

Рабочий каталог проекта: `/Users/rooty/work/rooty/ai-test`.
Документация в репозитории пишется на русском языке.

## Что уже сделано

### Local Ops Board

Реализованы:

- CRUD сервисов;
- dashboard со счётчиками состояний;
- ручные проверки HTTP(s), SMTP(s), IMAP(s), POP3(s), MySQL, Redis(s), RabbitMQ/AMQP(s) и PostgreSQL;
- история проверок;
- отображение статуса, HTTP-кода и response time;
- environment, tags и description сервиса;
- включение и отключение сервиса;
- проверка всех включённых сервисов;
- background worker для автоматических проверок;
- graceful shutdown по `SIGINT` и `SIGTERM`;
- единый checker для ручных и автоматических проверок;
- ограничение количества хранимых результатов проверки на сервис.

Основные API:

```text
GET    /api/v1/dashboard
GET    /api/v1/services
POST   /api/v1/services
PUT    /api/v1/services/:id
DELETE /api/v1/services/:id
POST   /api/v1/services/:id/check
GET    /api/v1/services/:id/checks
GET    /api/v1/settings
PUT    /api/v1/settings
GET    /api/v1/domain/monitors
POST   /api/v1/domain/monitors
PUT    /api/v1/domain/monitors/:id
DELETE /api/v1/domain/monitors/:id
```

Классификация endpoint-проверок:

- `2xx` и `3xx` — `online`;
- `4xx` и `5xx` — `degraded`;
- ошибка сети, invalid URL или timeout — `offline`.

TLS-вариант определяется схемой URL. Для MySQL используется `tls=true`, для PostgreSQL —
`sslmode=require`.

### Хранение данных

В SQLite добавлены таблицы `services`, `service_checks` и `app_settings`.
Инициализация схемы идемпотентная и выполняется при старте приложения. Существующие таблицы
SnippetBook не удаляются.

Настройки мониторинга хранятся в `app_settings`, а не в ENV:

```text
services.check_interval=30s
check_timeout=10s
check_history_limit=500
```

При первом запуске отсутствующие значения заполняются defaults. Уже сохранённые значения не
перезаписываются. Значения меняются через страницу `/uptime/settings`.
Worker перечитывает их из базы между циклами.

### Путь к базе

`DB_PATH` поддерживается:

- если `DB_PATH` задан, используется указанный путь;
- если `DB_PATH` не задан, используется `./app.db` в текущем каталоге запуска.

Примеры:

```bash
go run .
DB_PATH=/path/to/app.db go run .
```

`PORT` также остаётся технической настройкой запуска и по умолчанию равен `8080`.
Настройки самого мониторинга через ENV не задаются.

## Важные файлы

- `main.go` — запуск store, settings, HTTP-сервера, worker и graceful shutdown;
- `internal/config/config.go` — загрузка `.env`, `PORT` и `DB_PATH`;
- `internal/auth/cloudflare.go` — проверка Cloudflare Access JWT;
- `internal/model/service.go` — модели `Service`, `Check`, `Dashboard`, `Settings`;
- `internal/store/db.go` — создание схемы SQLite;
- `internal/store/services.go` — CRUD сервисов, проверки, dashboard и очистка истории;
- `internal/store/settings.go` — defaults и чтение/обновление настроек;
- `internal/monitor/monitor.go` — worker и protocol-neutral checker;
- `internal/domaincheck/rdap.go` — регистрационные данные домена через RDAP;
- `internal/mailcheck/checker.go` — MX, SPF, DKIM, DMARC, DNSBL/RBL и MX TLS;
- `internal/monitor/monitor_test.go` — тесты HTTP, TLS и protocol greeting checks;
- `internal/handler/services.go` — API Ops Board и settings;
- `internal/handler/router.go` — маршруты API и статические маршруты;
- `web/ops.html` — интерфейс Ops Board;
- `web/ops.js` — клиент API и логика интерфейса;
- `web/ops.css` — стили Ops Board;
- `web/mail.html`, `web/mail.js`, `web/mail.css` — Domains и мониторинг доменов;
- `web/settings.html`, `web/settings.js` — настройки, уведомления и reminders;
- `implementation_plan.md` — общий план развития;
- `README.md` — пользовательское описание запуска и возможностей.
- `deploy/` — systemd, Nginx, certificate scripts и удалённый deployment;
- `.github/workflows/deploy.yml.example` — пример GitHub Actions workflow для production; автоматически не запускается.

## Текущее состояние

Текущая задача не заблокирована. Реализованы SSL checks, детали сервисов, runbooks, reminders,
расширенный dashboard, очистка старой истории, глобальные browser/Telegram/Slack/SMTP notifications,
повторные уведомления активных проблем и полноценные
runbooks с редактированием, удалением, preview и безопасным рендерингом Markdown. Сервисы Ops Board
по-прежнему удаляются физически, а записи SnippetBook — через soft delete.

Добавлен Domains на `/uptime/domain`: MX, SPF, DKIM, DMARC, DNSBL/RBL, RDAP-регистрация,
дата окончания домена, MX TLS, история результатов и API `/api/v1/domain/*`. Добавлен постоянный
мониторинг доменов с индивидуальным интервалом, включением/выключением, удалением, уведомлением
при изменении статуса и alert при изменении RDAP-снимка.

Проверки сервисов сохраняют подробную диагностику: endpoint, hostname, порт, этап сбоя и TLS-данные.
Старые записи, созданные до добавления диагностических полей, могут не содержать расширенной информации.

Проверки после последнего изменения прошли:

```bash
go test ./...
go vet ./...
go build ./...
```

Также ранее проверялся JavaScript:

```bash
node --check web/ops.js
```

В проекте нет отдельной системы миграций. Все изменения схемы нужно добавлять в идемпотентную
инициализацию `store.New()`.

## Актуальный статус

Рекомендуемый порядок продолжения:

1. Выполнять production smoke tests после изменений deployment-конфигурации.
2. Повторно проверять RDAP и MX TLS для постоянных доменов.
3. Расширять диагностические поля и тесты при добавлении новых протоколов.

Автоматический restart сервисов, shell-команды, Kubernetes, cloud sync и multi-user пока не входят
в задачу и не добавляются без отдельного запроса.

## На какие грабли нельзя наступать

1. **Не удалять и не ломать SnippetBook.** Раздел `/` и существующие API должны продолжать работать.
2. **Не возвращать настройки мониторинга в ENV.** `CHECK_INTERVAL`, `CHECK_TIMEOUT` и
   `CHECK_HISTORY_LIMIT` больше не используются. Настройки редактируются через `/uptime` и хранятся
   в `app_settings`.
3. **Не удалять `DB_PATH`.** Default обязан оставаться `./app.db` в текущем каталоге запуска.
4. **Не менять базу без идемпотентной инициализации.** Используй `CREATE TABLE IF NOT EXISTS` и
   существующий подход `ensureColumn`, если добавляются колонки.
5. **Не собирать SQL конкатенацией.** Все значения пользователя передавай параметрами через `?`.
6. **Помнить о SQLite cursor lock.** При одном соединении базы нельзя оставлять открытый `rows` и
   параллельно выполнять запросы. Сначала закрывай cursor или полностью считывай результат.
7. **Не использовать `r.Static("/", "./web")`.** Это конфликтует с `/api`; текущая схема использует
   явные статические маршруты и `NoRoute` fallback для SPA.
8. **Не забывать soft delete.** Удаление сервисов выполняется через `SoftDelete`, а обычный список
   исключает записи с `deleted_at IS NOT NULL`.
9. **Не хранить runtime-файлы в исходниках.** `*.db`, `*.sqlite`, `.env` и секреты не коммитить.
10. **После изменений запускать проверки:**

    ```bash
    gofmt -w .
    go test ./...
    go vet ./...
    go build ./...
    node --check web/ops.js
    ```

11. **Учитывать текущие сигнатуры.** Используются `handler.SetupRouter(st, v)`,
    `NewServicesHandler(st)` и `monitor.New(st)`. При изменении API обновляй все места вызова.
12. **Не перезаписывать пользовательские настройки при старте.** `EnsureSettings()` должен только
    добавлять отсутствующие ключи через `ON CONFLICT DO NOTHING`.

## Команды запуска

Из корня проекта:

```bash
go mod download
go run .
```

После запуска:

- SnippetBook: `http://localhost:8080/`;
- Local Uptime Board: `http://localhost:8080/uptime`.
