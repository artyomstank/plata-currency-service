# Конфигурация

[Главная](../README.md) · [Документация](README.md) · [Запуск](running.md) · [Провайдер](provider.md)

Настройки читает [LoadServiceConfig](../internal/config/config.go) из
окружения процесса. Чтение ENV-файла делает Make или Compose, а не приложение.
Локальный пример — [deploy/.env.local.example](../deploy/.env.local.example).

<a id="structure"></a>

## Структура конфигурации

`ServiceConfig` собирает настройки компонентов, а не повторяет их поля:

| Поле | Тип и место объявления | Использование |
| --- | --- | --- |
| `Postgres` | [postgres.PoolConfig](../pkg/postgres/pool.go) | Создание пула PostgreSQL |
| `HTTPServer` | [httpserver.Config](../pkg/httpserver/server.go) | Listener address, read/write/idle timeouts и лимит заголовков |
| `HTTPTransport` | [http.Config](../internal/transport/http/router.go) | Deadline запроса и лимит тела в middleware |
| `Frankfurter` | [frankfurter.ClientConfig](../internal/provider/frankfurter/client.go) | Base URL клиента источника |
| `FrankfurterHTTP` | [httpclient.Config](../pkg/httpclient/client.go) | Timeout исходящего HTTP client |
| `Worker` | [worker.Config](../internal/worker/worker.go) | Интервал опроса одного воркера |
| `Currencies` | [usecase.CurrencyConfig](../internal/usecase/request_update.go) | Допуск валют для RequestUpdate и GetLatest |
| `ClaimPending` | [usecase.ClaimPendingConfig](../internal/usecase/claim_pending.go) | Длительность lease при claim/reclaim |
| `RetryJob` | [usecase.RetryConfig](../internal/usecase/retry_job.go) | Число попыток и backoff |
| `Runtime` | [config.RuntimeConfig](../internal/config/config.go) | Число воркеров и общий shutdown timeout приложения |

Типы компонентов объявлены возле потребителей. Они не читают ENV и не
импортируют общий пакет `internal/config`. `RuntimeConfig` остаётся в общем
конфиге, чтобы загрузчик не импортировал `internal/app` и не создавал цикл.

`LoadServiceConfig` назначает defaults, читает ENV, нормализует валюты и
проверяет значения, включая связь lease и timeout провайдера. `app.Run`
по-прежнему вызывает загрузчик до открытия ресурсов. Композиция передаёт
готовые группы в конструкторы: `postgres.NewPool(ctx, cfg.Postgres)`,
`httpclient.New(cfg.FrankfurterHTTP, ...)`, `worker.New(processor, cfg.Worker, log)`.

Имена ENV и defaults сохранены. `HTTP_READ_TIMEOUT` заполняет оба поля
`ReadTimeout` и `ReadHeaderTimeout`. Лимиты тела и заголовков равны 1 MiB;
отдельных ENV для них нет. Migrator продолжает использовать тот же загрузчик,
берёт настройки PostgreSQL и задаёт размер своего пула равным одному.

<a id="currencies"></a>

## Разрешённые валюты

По умолчанию разрешены `EUR,MXN,USD`. Чтобы добавить GBP, CHF и JPY,
задайте в `deploy/.env.local`:

```dotenv
ALLOWED_CURRENCIES=EUR,MXN,USD,GBP,CHF,JPY
```

Это замена списка целиком, а не добавление к значению по умолчанию.
Пересоздайте Docker-сервис по [инструкции запуска](running.md#stop).
Для запуска на хосте перезапустите процесс после изменения ENV.

Коды нормализуются: внешний whitespace удаляется, регистр становится верхним,
дубликаты исключаются. Нужны минимум два разных трёхбуквенных ASCII-кода.
Пустой или некорректный список не позволяет запустить приложение.

Список ограничивает создание новых джоб и запрос latest. Он не подтверждает
поддержку валюты у Frankfurter: для разрешённого, но неизвестного источнику
кода POST может принять джобу, а обработка завершиться retry/fail.
Сохранённые джобы и котировки не удаляются при изменении списка; доступ
к ним описан в [доменной модели](domain.md#currencies).

<a id="compose"></a>

## Окружение хоста и Compose

`--env-file` даёт Compose значения для подстановки `${...}`. Переменная
попадает внутрь контейнера только если объявлена в `environment`/`env_file`.
Сейчас [Compose](../deploy/docker-compose.yml) передаёт сервису
`DATABASE_DSN`, фиксированный `HTTP_ADDR=:8080`, `ALLOWED_CURRENCIES` и
`PROVIDER_BASE_URL`. Остальные настройки внутри контейнера используют
значения по умолчанию, даже если просто дописать их в локальный ENV.

Для дополнительных настроек можно создать локальный override, например
`deploy/compose.local.yml`:

```yaml
services:
  currency-service:
    environment:
      WORKER_COUNT: "2"
      PROVIDER_TIMEOUT: 3s
      JOB_LEASE_DURATION: 30s
      SHUTDOWN_TIMEOUT: 10s
```

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml \
  -f deploy/compose.local.yml up -d --no-build --force-recreate currency-service
```

`HTTP_PORT` меняет публикацию порта на хосте, а не порт HTTP внутри контейнера.
Для подключения из контейнеров используется `postgres:5432`; с хоста —
`localhost:54322`. Переменные `POSTGRES_DB`, `POSTGRES_USER`,
`POSTGRES_PASSWORD`, `POSTGRES_PORT`, `HTTP_PORT` служат Make/Compose и
не являются настройками, которые напрямую читает `LoadServiceConfig`.

<a id="variables"></a>

## Переменные приложения

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `DATABASE_DSN` | Нет, обязательна | Подключение PostgreSQL |
| `DATABASE_MAX_CONNS` | `10` | Максимум соединений пула |
| `DATABASE_MIN_CONNS` | `1` | Минимум соединений пула |
| `DATABASE_HEALTH_CHECK_PERIOD` | `30s` | Интервал проверки пула |
| `ALLOWED_CURRENCIES` | `EUR,MXN,USD` | Список разрешённых валют |
| `HTTP_ADDR` | `:8080` | Адрес HTTP listener |
| `HTTP_REQUEST_TIMEOUT` | `3s` | Deadline контекста handler, включая readiness |
| `HTTP_READ_TIMEOUT` | `5s` | Read timeout и read header timeout HTTP-сервера |
| `HTTP_WRITE_TIMEOUT` | `10s` | Write timeout HTTP-сервера |
| `HTTP_IDLE_TIMEOUT` | `60s` | Idle timeout keep-alive соединений |
| `WORKER_COUNT` | `3` | Число параллельных воркеров |
| `POLL_INTERVAL` | `500ms` | Интервал опроса для каждого воркера |
| `JOB_LEASE_DURATION` | `30s` | Время владения джобой до возможного reclaim |
| `MAX_ATTEMPTS` | `5` | Лимит при обработке ошибки через RetryOrFail |
| `RETRY_BASE` | `1s` | Начальная задержка retry |
| `RETRY_MAX` | `30s` | Верхняя граница задержки retry |
| `PROVIDER_BASE_URL` | `https://api.frankfurter.app` | Базовый адрес Frankfurter-compatible API |
| `PROVIDER_TIMEOUT` | `5s` | Общий таймаут исходящего HTTP-запроса |
| `SHUTDOWN_TIMEOUT` | `10s` | Период ожидания активных HTTP-запросов и джоб |

Длительности задаются в формате Go: `100ms`, `3s`, `1m`. Они должны быть
положительными. Число воркеров и `MAX_ATTEMPTS` — не меньше одного;
размеры пула — положительные int32, минимум не превышает максимум.
`RETRY_BASE` не превышает `RETRY_MAX`, а `JOB_LEASE_DURATION` строго больше
`PROVIDER_TIMEOUT`. DSN и HTTP address не могут быть пустыми; URL источника
требует HTTP(S) и host.

## Как соотносятся таймауты

HTTP deadline действует на текущий API-запрос, а не на будущую джобу.
`PROVIDER_TIMEOUT` ограничивает получение курса воркером. Lease должен
оставлять запас на сетевой запрос и последующую транзакцию; heartbeat для
продления lease сейчас нет. При его истечении другой воркер сможет сделать
reclaim, а прежний потеряет право завершить задачу.

`SHUTDOWN_TIMEOUT` действует одновременно на ожидание HTTP и воркеров.
Docker даёт контейнеру `stop_grace_period: 20s`: сначала SIGTERM, затем
SIGKILL, если процесс не завершился. Этот запас нужен и для выхода после
отмены операций; rollback транзакции имеет отдельный таймаут до 5 секунд.
Если меняете shutdown timeout, согласуйте и Compose grace period.
Подробнее — [lifecycle и границы ожидания](lifecycle.md#limits).
