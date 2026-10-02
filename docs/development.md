# Разработка, миграции и проверки

[Главная](../README.md) · [Документация](README.md) · [Архитектура](architecture.md) · [Запуск](running.md)

<a id="tests"></a>

## Локальные проверки

```bash
make test
go test -race ./...
go vet ./...
```

`make test` запускает `go test ./...`, `make test-unit` — `go test ./internal/... ./pkg/...`.
Обычные тесты не требуют PostgreSQL или реального Frankfurter. HTTP-тесты
поднимают локальные httptest-серверы. Проверка форматирования: `gofmt -l cmd internal pkg migrations`;
изменённые Go-файлы форматируются через `gofmt -w`.

Отдельный `golangci-lint` и target `make lint` не настроены. `go vet` —
имеющаяся стандартная статическая проверка, не замена отдельному набору линтеров.

Интеграционные проверки требуют локальную PostgreSQL на `localhost:54322`,
database/user/password — `postgres`:

```bash
go test -count=1 -tags=integration ./internal/repo/postgres/...
go test -count=1 -race -tags=integration ./...
```

Тесты repo используют временные таблицы в отдельном соединении и
`search_path=pg_temp`. Постоянные таблицы и данные приложения не меняются.
Схема создаётся из embedded `.up.sql`, после преобразования CREATE TABLE
в CREATE TEMP TABLE и исключения создания extension.

| Область | Что проверяется |
| --- | --- |
| [domain](../internal/domain) | Конструкторы, нормализация валют, ID и допустимые переходы |
| [usecase](../internal/usecase) | Сценарии, rollback, номер попытки, границы вызова источника, retry |
| [transport/http](../internal/transport/http) | Контракт API в router_test, сопоставление ошибок и JSON writer в error_test, связка Recoverer и ErrorHandler в router_recovery_test |
| [transport/http/handler](../internal/transport/http/handler) | Формирование input, перенос request context, возврат ошибок usecase и JSON writer |
| [provider/frankfurter](../internal/provider/frankfurter) | Внешний JSON, ACL, точность и интеграция клиента с middleware |
| [pkg/httpclient](../pkg/httpclient) | Заголовки, логирование без тела, timeout, отмена и закрытие idle connections |
| [pkg/httpserver/middleware](../pkg/httpserver/middleware) | Request ID, лимит тела, deadline, повтор исходного HTTP-ответа и отсутствие успешного ответа до commit |
| [pkg/postgres](../pkg/postgres) | Commit, rollback при ошибке, отмене и panic, сохранение context values, использование существующей транзакции |
| [repo/postgres/job](../internal/repo/postgres/job) и [quote](../internal/repo/postgres/quote) | Обязательная транзакция при записи и блокировках, точное восстановление UUID и decimal |
| [repo integration](../internal/repo/postgres/repository_integration_test.go) | Реальные транзакции, rollback двух репо, reclaim и backoff |
| [HTTP idempotency integration](../internal/repo/postgres/idempotency_integration_test.go) | Сохранённый ответ, откат Job и ключа при ошибке/panic/отмене, перенос существующих ключей и обратная миграция |
| [app lifecycle](../internal/app/lifecycle_test.go) | Drain HTTP/воркеров, дедлайн, ошибка HTTP Serve и порядок закрытия ресурсов; [описание механизма](lifecycle.md) |
| [migrations](../migrations/embed_test.go) | Наличие непустых SQL-миграций внутри бинарника |

Для повторения без результатов Go-кеша используйте `-count=1`. Если зависимости
уже есть локально, офлайн-запуск можно выполнить с `GOPROXY=off GOSUMDB=off
GOTOOLCHAIN=local`. При отсутствии модулей такая проверка завершится ошибкой,
а не скачает их.

<a id="migrations"></a>

## Миграции

Источник схемы — SQL-файлы в [migrations](../migrations). Изменение добавляется
новой парой `NNNN_name.up.sql` и `NNNN_name.down.sql`; старые применённые
миграции не редактируются. [cmd/migrate](../cmd/migrate/main.go) сортирует
имена `.up.sql`, пропускает версии из `schema_migrations`, применяет SQL и
записывает версию в одной транзакции. Advisory lock сериализует migrator.

Применение локально:

```bash
make migrate
```

После новой миграции для Docker:

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml build migrate
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml run --rm --no-deps migrate
```

Миграция [0003_attempt_claim.up.sql](../migrations/0003_attempt_claim.up.sql)
удаляет `lease_token`: владение теперь проверяется по `Attempts`. Перед
обновлением остановите все старые экземпляры сервиса, примените миграцию
и запустите новый код. Одновременно запускать обработчики старого и нового
протоколов нельзя. Статусы, сроки аренды, счётчики и котировки сохраняются.
Обратная миграция восстанавливает колонку, но не прежние UUID-токены.

Миграция [0004_http_idempotency.up.sql](../migrations/0004_http_idempotency.up.sql)
создаёт `http_idempotency`, переносит существующие ключи с восстановленным
исходным ответом 202/pending и удаляет `idempotency_key` из `quote_jobs`.
Перед обновлением остановите старые экземпляры сервиса. Повтор POST теперь
возвращает исходный 202 и тело; 200 с текущим статусом и конфликт пары по ключу
больше не используются. Down-миграция возвращает ключи существующим джобам,
а сохранённые HTTP-ответы удаляет. Подробности — [идемпотентность](idempotency.md).

В БД записи версии хранятся без `.up.sql`, например `0003_attempt_claim`.
Проверять их следует на локальном инстансе. Команда не выполняет автоматический
rollback; `.down.sql` предназначены для отдельного ручного отката после
оценки последствий.

SQL встроен в бинарник директивой `//go:embed *.up.sql` из
[embed.go](../migrations/embed.go). Эта строка выглядит как комментарий,
но необходима компилятору. Так же функциональна `//go:build integration`
в интеграционном тесте. Удалять такие директивы при очистке поясняющих
комментариев нельзя.

<a id="changes"></a>

## Как расширять проект

- Новое правило пары или перехода добавляйте в два файла домена и проверяйте доменными тестами.
- Новый сценарий оформляйте отдельным файлом usecase с `Execute` и узкими портами по месту использования.
- Новый HTTP endpoint добавляйте через DTO-конвертер и общий ErrorHandler; SQL остаётся в repo.
- Изменение хранилища включает новую миграцию, модели repo и проверки транзакций.
- Новый источник курсов оформляйте ресурсным пакетом под provider с внешними моделями, клиентом и ACL.
- Подключение новых зависимостей и управление ресурсами остаются в internal/app.

Необходимые границы объяснены в [архитектуре](architecture.md#boundaries).
Бизнес-код не должен получать `pgx.Tx`, HTTP response или внешнюю JSON-модель.
Для нового сценария, затрагивающего оба repo, явно определяйте общую транзакцию;
сетевую операцию внутрь неё не помещайте.

## Ручная проверка в Docker

После [запуска](running.md#docker) проверьте POST → статус джобы → latest,
повтор исходного ответа с одинаковым ключом, невалидный JSON и валютную пару.
Для ошибок источника, timeout и shutdown используйте контролируемый локальный
Frankfurter-compatible endpoint через Compose override.

Реальная интеграция и тестовый источник проверяют разные вещи: первая —
совместимость текущего источника с ACL, второй — предсказуемые failure paths.
Результаты прошлых прогонов не гарантируют текущее состояние контейнеров,
поэтому перед проверкой сверяйте образ, ENV и локальный порт БД.
