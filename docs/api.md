# HTTP API

[Главная](../README.md) · [Документация](README.md) · [Запуск](running.md) · [Домен](domain.md)

Базовый адрес локального сервиса — `http://localhost:8080`.
Транспорт принимает JSON и отвечает с `Content-Type: application/json`.
Цена передаётся строкой, время — RFC 3339 с возможной дробной частью, в UTC.
Аутентификация и авторизация в текущем роутере не реализованы.

<a id="first-request"></a>

## Первый запрос

```bash
curl -i http://localhost:8080/v1/quote-updates \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-1' \
  -H 'X-Request-ID: example-request-1' \
  -d '{"pair":"EUR/MXN"}'
```

Из ответа возьмите `jobId` и опрашивайте endpoint результата:

```bash
job_id='b18e9a84-0cdf-46bb-889d-ed4a7235c367'
curl -i "http://localhost:8080/v1/quote-updates/$job_id"
curl -i --get http://localhost:8080/v1/quotes/latest \
  --data-urlencode 'pair=EUR/MXN'
```

UUID и цены в примерах иллюстративные; используйте ID, полученный от своего
сервиса. `POST` не ждёт Frankfurter. Отсутствие новой котировки сразу после
`202` нормально: обработка идёт в фоне.

<a id="create"></a>

## Создать задачу

`POST /v1/quote-updates`

```json
{"pair":"EUR/MXN"}
```

Новая джоба: HTTP `202 Accepted`.

```json
{"jobId":"b18e9a84-0cdf-46bb-889d-ed4a7235c367","status":"JOB_STATUS_PENDING"}
```

Повтор с тем же ключом: исходный HTTP `202 Accepted` и то же тело ответа,
включая исходный `JOB_STATUS_PENDING`. Текущий статус читается через GET.
Ответ POST содержит только `jobId` и `status`, без котировки.

Тело должно содержать один JSON-объект с полем `pair`. Неизвестные поля,
невалидный JSON, несколько JSON-значений и неподдержанная пара дают `400`.
Лимит тела — 1 MiB, превышение даёт `413`.

<a id="result"></a>

## Получить джобу и результат

`GET /v1/quote-updates/{job_id}`

При `pending` или `processing`:

```json
{"jobId":"b18e9a84-0cdf-46bb-889d-ed4a7235c367","status":"JOB_STATUS_PROCESSING","pair":"EUR/MXN"}
```

При `done`:

```json
{"jobId":"b18e9a84-0cdf-46bb-889d-ed4a7235c367","status":"JOB_STATUS_DONE","pair":"EUR/MXN","price":"20.123456789012345678","updatedAt":"2026-10-01T12:00:00.123456Z"}
```

При `failed`:

```json
{"jobId":"b18e9a84-0cdf-46bb-889d-ed4a7235c367","status":"JOB_STATUS_FAILED","pair":"EUR/MXN","errorMessage":"quote provider is temporarily unavailable"}
```

Существующая джоба во всех состояниях возвращает `200`. `price` и `updatedAt`
появляются только при наличии результата, `errorMessage` — только при `failed`.
Число попыток, срок аренды и исходная дата курса в контракт не входят.
Невалидный или нулевой UUID даёт `400`, неизвестный UUID — `404`.

| Доменный статус | Статус API |
| --- | --- |
| `pending` | `JOB_STATUS_PENDING` |
| `processing` | `JOB_STATUS_PROCESSING` |
| `done` | `JOB_STATUS_DONE` |
| `failed` | `JOB_STATUS_FAILED` |

<a id="latest"></a>

## Последняя котировка

`GET /v1/quotes/latest?pair=EUR%2FMXN`

```json
{"pair":"EUR/MXN","price":"20.123456789012345678","updatedAt":"2026-10-01T12:00:00.123456Z"}
```

Endpoint читает БД и не создаёт джобу или запрос к источнику. Latest определяется
по времени сохранения `created_at`, а не по дате курса Frankfurter. Ответ может
содержать предыдущий результат, пока новая джоба ещё обрабатывается.
Если результатов нет — `404`; если пара не разрешена текущим конфигом — `400`.

<a id="idempotency"></a>

## Идемпотентность

`Idempotency-Key` необязателен, максимум — 128 байт. Без ключа каждый POST
создаёт новую джобу. Ключ применяется глобально к запросам создания задач,
без привязки к пользователю или конкретной паре.

Middleware сохраняет успешный HTTP-ответ в отдельной таблице `http_idempotency`.
Повтор возвращает исходные статус, тело и заголовки ответа; `X-Request-ID`
принадлежит текущему запросу. Usecase при повторе не вызывается.

Ключ определяет ответ независимо от нового тела: другая пара, невалидный JSON
или изменение списка разрешённых валют не меняют уже сохранённый ответ.
Для другого действия нужен новый ключ. Повтор не перезапускает завершённую
или failed-джобу; актуальное состояние читается через GET.

Ошибки HTTP не сохраняются: после исправления запроса или устранения сбоя
можно повторить его с тем же ключом. Конкурентный запрос ждёт завершения
транзакции первого; при истечении собственного deadline получает `504` и
может повториться позже. Срок удаления ключей сейчас не задан.
Подробности — [HTTP-идемпотентность](idempotency.md).

<a id="request-id"></a>

## Request ID и методы

`X-Request-ID` до 128 байт принимается от клиента; отсутствующий или слишком
длинный заменяется UUID. Значение попадает в контекст, ответный заголовок,
HTTP-лог и JSON ошибки. Текущий клиент Frankfurter не переносит этот заголовок
в исходящий запрос автоматически.

Все GET-маршруты также поддерживают HEAD. Для неподдержанного метода сервер
возвращает `405` и `Allow`, для неизвестного пути — `404`.

<a id="errors"></a>

## Ошибки

```json
{"code":"INVALID_ARGUMENT","message":"invalid job_id","requestId":"example-request-1"}
```

| HTTP | `code` | Причина |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | JSON, UUID, валютная пара или ключ невалидны |
| 404 | `NOT_FOUND` | Джоба, котировка или маршрут не найдены |
| 405 | `METHOD_NOT_ALLOWED` | Метод не поддержан маршрутом |
| 413 | `INVALID_ARGUMENT` | Тело запроса превышает 1 MiB |
| 500 | `INTERNAL_ERROR` | Внутренняя ошибка или panic |
| 504 | `GATEWAY_TIMEOUT` | Истёк контекстный таймаут запроса |

`GATEWAY_TIMEOUT` сохранён как публичный код, хотя шлюза в архитектуре нет.
Внутренние ошибки БД и тела ответов провайдера клиенту не выдаются. Неуспех
фонового запроса курса отражается в джобе, а не в ответе уже завершённого POST.

<a id="health"></a>

## Проверки состояния

| Endpoint | Успех | Неуспех |
| --- | --- | --- |
| `GET /healthz` | `200 {"status":"ok"}` | Проверяет только живой процесс |
| `GET /readyz` | `200 {"status":"ready"}` | `503 {"status":"not_ready"}` при ошибке Ping БД |

Readiness не проверяет наличие всех таблиц, доступность Frankfurter или
завершение очереди. Ответ `503` readiness использует модель состояния,
а не общий JSON ошибок из таблицы выше.

Реализация: [router.go](../internal/transport/http/router.go),
[handler.go](../internal/transport/http/handler/handler.go),
[dto.go](../internal/transport/http/handler/dto.go), [converter.go](../internal/transport/http/handler/converter.go),
[error.go](../internal/transport/http/error.go).
