# currency-service

Асинхронный HTTP-сервис валютных котировок на Go. Клиент создаёт джобу,
воркер получает курс у Frankfurter и сохраняет результат в PostgreSQL.
HTTP и воркеры работают в одном процессе; PostgreSQL служит также очередью.

Используются chi, pgx и decimal. Сценарии приложения отделены от доменных
правил, HTTP-транспорта, репозиториев и внешнего источника курсов.

## Быстрый запуск

Нужны Docker с Compose и Make. Если локального ENV-файла ещё нет:

```bash
cp deploy/.env.local.example deploy/.env.local
```

```bash
make docker-up
curl -fsS http://localhost:8080/readyz
```

Compose запускает PostgreSQL, migrator и сервис. Локальная БД доступна на
`localhost:54322`; HTTP — на `localhost:8080`. Сборка требует доступных
базовых образов и Go-зависимостей. [Подробный запуск](docs/running.md).

## Основной API

| Метод и путь | Назначение |
| --- | --- |
| `POST /v1/quote-updates` | Создать джобу; необязательный `Idempotency-Key` |
| `GET /v1/quote-updates/{job_id}` | Получить статус джобы и результат |
| `GET /v1/quotes/latest?pair=EUR%2FMXN` | Получить последнюю сохранённую котировку |
| `GET /healthz`, `GET /readyz` | Проверить процесс и подключение к БД |

```bash
curl -i http://localhost:8080/v1/quote-updates \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-1' \
  -d '{"pair":"EUR/MXN"}'
```

Ответ новой джобы — `202` с `jobId`. Его нужно передать в запрос статуса.
Цена возвращается строкой. По умолчанию разрешены EUR, MXN и USD; список
задаётся через `ALLOWED_CURRENCIES`. [Контракт API и примеры](docs/api.md).

## Документация

| Раздел | Что внутри |
| --- | --- |
| [Навигация по документации](docs/README.md) | Порядок чтения и карта тем |
| [Архитектура](docs/architecture.md) | Границы слоёв, зависимости, middleware, сценарии |
| [Доменная модель](docs/domain.md) | Job, Quote, типы ID, правила и переходы статусов |
| [HTTP API](docs/api.md) | Запросы, ответы, ошибки и идемпотентность |
| [HTTP-идемпотентность](docs/idempotency.md) | Сохранённый ответ, конкурентные запросы и атомарность |
| [Идемпотентность, джобы и stateless](docs/reliability.md) | Доработки протокола, восстановление и границы гарантий |
| [Запуск](docs/running.md) | Docker, запуск на хосте, остановка, диагностика |
| [Конфигурация](docs/configuration.md) | Валюты, ENV, таймауты и передача настроек в Compose |
| [Frankfurter](docs/provider.md) | Выбор источника, HTTP-клиент, внешние модели и ACL |
| [Обработка джоб](docs/jobs.md) | Транзакции, lease и retry |
| [Lifecycle](docs/lifecycle.md) | Запуск, контексты, drain и остановка ресурсов |
| [Trade-offs](docs/trade-offs.md) | Принятые решения, их цена и ограничения реализации |
| [Разработка и проверки](docs/development.md) | Тесты, миграции и правила расширения проекта |

## Проверки

```bash
make test
go vet ./...
```

Остановка контейнеров с сохранением данных: `make docker-down`.
Подробности интеграционных проверок — в [разделе разработки](docs/development.md).
