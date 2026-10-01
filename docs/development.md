# Разработка, миграции и проверки

[Главная](../README.md) · [Документация](README.md) · [Архитектура](architecture.md) · [Запуск](running.md)

<a id="tests"></a>

## Локальные проверки

```bash
make test
go test -race ./...
go vet ./...
```

`make test` запускает `go test ./...`, `make test-unit` — `go test ./internal/...`.
Обычные тесты не требуют PostgreSQL или реального Frankfurter. HTTP-тесты
поднимают локальные httptest-серверы. Проверка форматирования: `gofmt -l cmd internal migrations`;
изменённые Go-файлы форматируются через `gofmt -w`.

Отдельный `golangci-lint` и target `make lint` не настроены. `go vet` —
имеющаяся стандартная статическая проверка, не замена отдельному набору линтеров.

Интеграционные проверки требуют локальную PostgreSQL на `localhost:54322`,
database/user/password — `postgres`:

```bash
go test -count=1 -tags=integration ./internal/repo
go test -count=1 -race -tags=integration ./...
```

Тесты repo используют временные таблицы в отдельном соединении и
`search_path=pg_temp`. Постоянные таблицы и данные приложения не меняются.
Схема создаётся из embedded `.up.sql`, после преобразования CREATE TABLE
в CREATE TEMP TABLE и исключения создания extension.

| Область | Что проверяется |
| --- | --- |
| [domain](../internal/domain) | Конструкторы, нормализация валют, ID и допустимые переходы |
| [usecase](../internal/usecase) | Сценарии, rollback, lease token, границы вызова источника, retry |
| [transport/http](../internal/transport/http) | API, middleware, DTO, ошибки, request ID |
| [provider/frankfurter](../internal/provider/frankfurter) | Заголовки, точность, внешний JSON, timeout и отмена |
| [repo integration](../internal/repo/repository_integration_test.go) | Реальные транзакции, идемпотентность, rollback двух репо, reclaim и backoff |
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

В БД записи версии хранятся без `.up.sql`, например `0002_job_reliability`.
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
повтор с одинаковым ключом, конфликт ключа, невалидный JSON и валютную пару.
Для ошибок источника, timeout и shutdown используйте контролируемый локальный
Frankfurter-compatible endpoint через Compose override.

Реальная интеграция и тестовый источник проверяют разные вещи: первая —
совместимость текущего источника с ACL, второй — предсказуемые failure paths.
Результаты прошлых прогонов не гарантируют текущее состояние контейнеров,
поэтому перед проверкой сверяйте образ, ENV и локальный порт БД.
