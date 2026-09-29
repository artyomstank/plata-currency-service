# currency-quotes

Асинхронный сервис котировок валютных пар. Клиент сначала запускает
обновление, затем опрашивает результат по идентификатору задачи; сама
котировка обновляется в фоне.

## Архитектура

```
клиент --HTTP/JSON--> [ gateway ] --gRPC--> [ service ] --SQL--> [ Postgres ]
                                                  ^
                                                  |
                                              [ worker(s) ]  (poll: SKIP LOCKED)
                                                  |
                                          внешний API курсов
```

- **service** — stateless gRPC-сервис с бизнес-логикой. Не имеет
  собственного in-memory состояния: любые данные — в Postgres, поэтому
  масштабируется горизонтально простым запуском новых реплик.
- **gateway** — отдельный HTTP-шлюз (grpc-gateway), транслирует
  внешний HTTP/JSON в gRPC к сервису. Здесь единая точка для сквозной
  логики (auth, rate limiting, логирование, см. `internal/gateway/middleware.go`)
  — она добавляется без изменений в бизнес-логике `service`.
- **контракт** — единый источник правды в `api/quotes/v1/quotes.proto`.
  Из него генерируются: gRPC-код, grpc-gateway обвязка и OpenAPI-файл
  (`api/openapi/`) — документация не пишется вручную.
- **воркер** встроен в бинарник `service` (несколько горутин), забирает
  задачи из Postgres через `SELECT ... FOR UPDATE SKIP LOCKED`. Это даёт
  и масштабирование обработки, и отсутствие дублирования между
  инстансами, и устойчивость к падению — незавершённая задача остаётся
  в БД. При желании воркер легко вынести в отдельный `cmd/worker`
  бинарник для независимого масштабирования обработки от приёма запросов.

## Модель данных (2 таблицы, см. `migrations/0001_init.up.sql`)

- `quote_jobs` — задача и её статус (`pending/processing/done/failed`).
- `quote_values` — результат обновления, `job_id` — FK на `quote_jobs.id`.
  Через этот FK связаны получение по job_id и history курса.

## Генерация кода из proto

Контракт описан в `api/quotes/v1/quotes.proto`. Сгенерированный код в
`internal/genpb/` **не входит** в этот черновик — сгенерируйте его перед
первым запуском:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest
go install github.com/bufbuild/buf/cmd/buf@latest

buf dep update   # один раз, требует сеть: тянет google/api/annotations.proto
make generate    # buf generate — офлайн, плагины локальные
go mod tidy
```

## Запуск

```bash
PROVIDER_API_KEY=xxx make docker-up
```

Пример запросов:

```bash
curl -X POST localhost:8080/v1/quotes:update \
  -d '{"pair":"USD/MXN"}'
# => {"jobId":"...", "status":"JOB_STATUS_PENDING"}

curl localhost:8080/v1/quotes/updates/<job_id>

curl "localhost:8080/v1/quotes/latest?pair=USD/MXN"
```

## Осознанные допущения (нужно свериться с требованиями/заказчиком)

- Набор валют захардкожен: USD, EUR, MXN (`internal/usecase.AllowedCurrencies`).
- Код пары — `BASE/QUOTE` в теле запроса или query-параметре (не в path,
  чтобы не конфликтовать с `/`).
- Ошибка получения курса сразу переводит задачу в `failed`, без ретраев
  с backoff — это отмечено `TODO` в `internal/worker/worker.go`.
- Идемпотентность обновления не реализована — TODO: `Idempotency-Key`
  или дедупликация повторных запросов на одну пару в короткий период.
- Бесплатный тариф exchangeratesapi.io отдаёт котировки только с базой
  EUR — кросс-курсы считаются в `internal/provider`.

## Дальнейшие шаги

- unit-тесты для `internal/usecase` (не требуют БД/сети);
- `authMiddleware` на шлюзе (заготовка уже есть);
- ретраи с backoff и обработка зависших `processing`-задач после падения
  воркера в момент обработки;
- вынос воркера в отдельный `cmd/worker` при необходимости независимого
  масштабирования обработки очереди от приёма HTTP/gRPC-запросов.
