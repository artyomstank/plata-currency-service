# Запуск проекта

[Главная](../README.md) · [Документация](README.md) · [Конфигурация](configuration.md) · [API](api.md)

Все команды ниже запускаются из корня проекта. Для Docker нужны Compose v2 или новее
и Make; для запуска на хосте и тестов — Go 1.26 согласно [go.mod](../go.mod).
Локальная PostgreSQL: `localhost:54322`, database/user/password — `postgres`.

<a id="docker"></a>

## Docker Compose

Если `deploy/.env.local` ещё не существует, создайте его из
[локального примера](../deploy/.env.local.example):

```bash
cp deploy/.env.local.example deploy/.env.local
```

Существующий локальный файл сохраняйте. В нём задаются параметры PostgreSQL,
HTTP-порт, разрешённые валюты и адрес источника. ENV-файл не загружается Go-кодом
автоматически: его читает Make/Compose. [Как передаются настройки](configuration.md#compose).

```bash
make docker-up
make docker-ps
make docker-logs
```

Порядок запуска в [docker-compose.yml](../deploy/docker-compose.yml):

1. PostgreSQL начинает принимать соединения и проходит healthcheck.
2. `migrate` применяет ещё не выполненные миграции и завершается.
3. `currency-service` запускается после успешного выхода migrator.

Сервис содержит HTTP и воркеры. Данные PostgreSQL лежат в volume `pgdata`.
`make docker-up` собирает образы, пересоздаёт контейнеры и удаляет orphan
контейнеры этого Compose-проекта; volume сохраняется. При наличии уже
собранных образов можно запускать без сборки и скачивания:

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml \
  up -d --no-build --pull never
```

[Dockerfile](../deploy/Dockerfile) один, с двумя runtime targets:

| Target | Бинарник и entrypoint |
| --- | --- |
| `currency-service` | `/currency-service` |
| `migrate` | `/migrate` |

Build stage использует Go 1.26, runtime — distroless nonroot. Полная сборка
требует базовых образов и Go-модулей; при работе офлайн они должны быть
доступны в локальном кеше. Shell внутри runtime-контейнера не предусмотрен.

Проверка запуска:

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Ожидаются `{"status":"ok"}` и `{"status":"ready"}`. После этого выполните
[первый запрос к API](api.md#first-request).

Интерактивная документация доступна на [http://localhost:8080/docs/](http://localhost:8080/docs/).
Она входит в бинарник сервиса; отдельный контейнер или запуск UI не нужен.
При изменении HTTP-порта откройте `/docs/` на адресе своего сервера.

<a id="host"></a>

## Запуск Go-сервиса на хосте

Нужна уже работающая локальная PostgreSQL. Её можно запустить отдельно:

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml up -d postgres
make migrate
make run-service
```

Обе Make-команды загружают `deploy/.env.local`. Они собирают `DATABASE_DSN`
для `localhost`, учитывая `POSTGRES_PORT`, пользователя и БД. Для локальной
разработки оставляйте порт `54322`. `run-service` берёт `HTTP_ADDR`, если он
задан, иначе использует `HTTP_PORT` или `8080`.

Если Docker-сервис уже занимает HTTP-порт, остановите только его перед
запуском на хосте:

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml stop currency-service
```

При прямом `go run ./cmd/service` переменные должны быть экспортированы
в окружение. Сервис требует `DATABASE_DSN` и не имеет встроенного DSN.
Подробности — [список переменных](configuration.md#variables).

<a id="stop"></a>

## Остановка и перезапуск

```bash
make docker-down
```

Команда останавливает и удаляет контейнеры проекта, сохраняя volume.
Удаление volume не требуется для обычного перезапуска. Завершение активных
операций описано в [graceful shutdown](lifecycle.md#shutdown).

Для перезапуска только сервиса с тем же образом и окружением:

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml restart currency-service
```

`restart` не применяет новый ENV и не пересобирает код. После изменения
`ALLOWED_CURRENCIES` или `PROVIDER_BASE_URL` пересоздайте сервис:

```bash
docker compose --env-file deploy/.env.local -f deploy/docker-compose.yml \
  up -d --no-deps --no-build --force-recreate currency-service
```

БД и миграции при этом уже должны быть готовы: `--no-deps` затрагивает
только контейнер сервиса.

После изменения Go-кода пересоберите образ через `make docker-up`.

Если обновление включает миграции, сначала остановите все экземпляры
приложения и выполните [обновление кода и схемы](development.md#migrations).
Обычный запуск Compose сам по себе не задаёт порядок остановки старых
воркеров перед изменением несовместимой схемы.

<a id="troubleshooting"></a>

## Диагностика

| Симптом | Что проверить |
| --- | --- |
| Сервис не стартует | Логи `currency-service`/`migrate`, `DATABASE_DSN`, валидность ENV, свободный HTTP-порт |
| Migrator завершился с ошибкой | Локальная БД, SQL миграций, отсутствие параллельного зависшего migrator |
| `/readyz` даёт 503 | Доступность PostgreSQL и параметры подключения |
| POST даёт 400 для новой валюты | Оба кода входят в `ALLOWED_CURRENCIES`, контейнер пересоздан |
| Джоба pending | Есть ли воркеры, не назначен ли будущий `next_attempt_at` после retry |
| Джоба processing после сбоя | Дождаться истечения lease и повторного claim |
| Джоба failed | Логи ошибки источника, поддержка пары, адрес и таймаут провайдера |
| Latest возвращает предыдущую цену | Проверить статус новой джобы; latest читает только сохранённые результаты |

Readiness — проверка подключения, а не гарантия наличия схемы или доступности
внешнего API. Логи HTTP содержат request ID; логи воркера помогают связать
ошибку обработки с джобой. [Устройство провайдера](provider.md#failures).
