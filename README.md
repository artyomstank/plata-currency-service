# currency-quotes

Асинхронный сервис котировок валютных пар по тестовому заданию. HTTP-запрос
создаёт задачу и сразу возвращает её идентификатор; получение цены выполняется
воркером после завершения HTTP-обработчика.

## Архитектура

    client --HTTP/JSON--> gateway --gRPC--> service --SQL--> PostgreSQL
                                                |
                                          worker pool
                                                |
                                       external rates API

- gateway предоставляет публичный HTTP/JSON API, request ID, единую модель
  ошибок и health endpoints.
- service владеет бизнес-операциями и persistent-очередью. Его gRPC-порт не
  публикуется из Docker Compose.
- воркеры находятся в процессе service, но координируются через PostgreSQL;
  локальная память не является source of truth.
- api/quotes/v1/quotes.proto — source of truth для gRPC, HTTP mapping и
  генерируемой OpenAPI-спецификации.

Один Go-модуль оставлен намеренно: для сервиса такого размера три независимых
модуля и go.work усложняют генерацию контракта, CI и версионирование без
практической выгоды. Процессы и Docker-образы при этом разделены. Разнести их
по модулям стоит только при независимых командах или release cycles.

## Гарантии фоновой обработки

quote_jobs является persistent-очередью:

- FOR UPDATE SKIP LOCKED исключает одновременный claim одной задачи;
- claim получает lease_token и lease_until;
- истёкший processing claim доступен другому воркеру после сбоя процесса;
- старый воркер не может завершить уже перехваченную задачу;
- запись значения и перевод задачи в done выполняются одной транзакцией;
- временная ошибка провайдера возвращает задачу в pending с exponential
  backoff; после MAX_ATTEMPTS задача становится failed.

Гарантия — at-least-once вызов внешнего API и exactly-once сохранение результата
для одного job. Внешний GET идемпотентен, поэтому повторный вызов безопасен.

## HTTP API

### Создать обновление

    POST /v1/quote-updates
    Content-Type: application/json
    Idempotency-Key: client-request-42

    {"pair":"EUR/MXN"}

Новая задача возвращает 202 Accepted:

    {"jobId":"b18e9a84-0cdf-46bb-889d-ed4a7235c367","status":"JOB_STATUS_PENDING"}

Idempotency-Key необязателен. Повтор с тем же ключом и парой возвращает
исходную задачу; тот же ключ с другой парой возвращает 409 Conflict.

### Получить обновление по идентификатору

    GET /v1/quote-updates/{job_id}

Для pending/processing поля price и updatedAt отсутствуют. Для done они
заполнены. Для failed заполнено публичное errorMessage без внутренних деталей
провайдера.

### Получить последнюю котировку

    GET /v1/quotes/latest?pair=EUR%2FMXN

Цена передаётся decimal-строкой, чтобы JSON float не терял точность.
updatedAt — время сохранения результата сервисом в RFC 3339.

Поддерживаются только USD, EUR, MXN; пара должна иметь формат BASE/QUOTE,
валюты должны различаться.

Ошибки имеют стабильную форму:

    {
      "code": "INVALID_ARGUMENT",
      "message": "invalid currency pair: expected BASE/QUOTE",
      "requestId": "..."
    }

## Источник курсов

По умолчанию используется публичный Frankfurter-compatible endpoint
https://api.frankfurter.app. API key не требуется. Base URL и таймаут
настраиваются через PROVIDER_BASE_URL и PROVIDER_TIMEOUT. Worker сохраняет
дату курса источника отдельно, а клиенту возвращает время обновления сервиса.

## Генерация контракта

Generated-файлы нельзя редактировать вручную. Перед первой сборкой установите
buf, protoc-gen-go, protoc-gen-go-grpc, protoc-gen-grpc-gateway и
protoc-gen-openapiv2, затем выполните:

    buf dep update
    make generate
    go mod tidy

Команда создаёт Go-код в internal/genpb/ и OpenAPI в api/openapi/.

## Локальный запуск

Локальные параметры PostgreSQL и опубликованные порты читаются из
`deploy/.env.local`. Файл исключён из Git. Для нового checkout создайте его из
примера:

    cp deploy/.env.local.example deploy/.env.local

Compose передаёт приложению готовый `DATABASE_DSN`; приложение не содержит
fallback с логином и паролем и завершится с ошибкой, если DSN не задан.

Docker Compose поднимает локальный PostgreSQL на localhost:54322, отдельный
одноразовый migrator, service и gateway:

    make generate
    make docker-up

`docker-up` пересоздаёт контейнеры и запускает их в фоне, сохраняя PostgreSQL
volume. Это также восстанавливает Compose-сеть после прерванного запуска.
Проверить состояние и посмотреть логи:

    make docker-ps
    make docker-logs

Остановить контейнеры без удаления PostgreSQL volume:

    make docker-down

## Обновление схемы БД

Новые изменения схемы добавляются отдельной парой файлов, например
`migrations/0003_add_something.up.sql` и
`migrations/0003_add_something.down.sql`. Уже применённые миграции изменять нельзя.

SQL-файлы встроены в бинарник migrator, поэтому после добавления миграции
нужно пересобрать и запустить только его:

    docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml build migrate
    docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml run --rm --no-deps migrate

PostgreSQL, service и gateway при этом не перезапускаются. Migrator применяет
только ещё не выполненные `.up.sql`, каждую в отдельной транзакции, и хранит
версии в `schema_migrations`. Advisory lock защищает от одновременного запуска
нескольких migrator.

Автоматического rollback в проекте нет намеренно. Файлы `.down.sql`
документируют ручной откат обратимых изменений и могут использоваться локально
после проверки последствий. Для production предпочтителен roll-forward:
ошибка исправляется новой миграцией. Это исключает автоматический запуск
потенциально разрушающих операций вроде `DROP COLUMN` и рассинхронизацию схемы
с уже запущенной версией service.

Для запуска без Docker команды также читают `deploy/.env.local`, но подключают
service к PostgreSQL через `localhost`:

    make migrate
    make run-service
    make run-gateway

Проверки:

    make test

Endpoints процесса gateway:

- GET /healthz — процесс жив;
- GET /readyz — внутренний gRPC service отвечает SERVING.

## Границы решения

- Аутентификация и rate limiting не заданы в ТЗ и не включены.
- Внутренний gRPC использует plaintext внутри локальной Compose-сети. Для
  размещения gateway и service на разных доверительных границах нужен mTLS.
- Метрики и distributed tracing не добавлены: для тестового сервиса достаточно
  структурных HTTP/gRPC/worker-логов; интерфейсы допускают дальнейшее
  подключение наблюдаемости без изменения бизнес-логики.
