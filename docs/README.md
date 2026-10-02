# Документация

[Главная](../README.md)

Документация описывает текущую реализацию. Ссылки на исходники ведут к месту,
где реализовано соответствующее поведение.

## С чего начать

- Запустить сервис: [запуск](running.md), затем [первый запрос](api.md#first-request).
- Разобраться в коде: [архитектура](architecture.md), [домен](domain.md), [обработка джоб](jobs.md).
- Подключить клиент: [HTTP API](api.md), [валюты и настройки](configuration.md).
- Изменить источник курсов: [Frankfurter и ACL](provider.md), [границы адаптеров](architecture.md#boundaries).
- Разрабатывать и проверять изменения: [разработка](development.md).
- Понять причины выбора архитектуры: [решения и trade-offs](trade-offs.md).
- Разобраться в остановке процесса: [lifecycle](lifecycle.md).
- Разобраться в доработках надёжности: [идемпотентность, джобы и stateless](reliability.md).

## Карта разделов

| Страница | Основные темы |
| --- | --- |
| [Архитектура](architecture.md) | [Границы](architecture.md#boundaries), [сценарии](architecture.md#scenarios), [контекст](architecture.md#context), [конвертеры](architecture.md#converters) |
| [Домен](domain.md) | [Job](domain.md#job), [статусы](domain.md#transitions), [Quote](domain.md#quote), [правила валют](domain.md#currencies) |
| [API](api.md) | [Создание](api.md#create), [результат](api.md#result), [latest](api.md#latest), [ошибки](api.md#errors), [идемпотентность](api.md#idempotency) |
| [HTTP-идемпотентность](idempotency.md) | [Контракт](idempotency.md#contract), [транзакция](idempotency.md#transaction), [таблица и миграция](idempotency.md#storage), [границы](idempotency.md#limits) |
| [Идемпотентность, джобы и stateless](reliability.md) | [Изменения](reliability.md#changes), [HTTP](reliability.md#idempotency), [job](reliability.md#jobs), [stateless](reliability.md#stateless), [гарантии](reliability.md#guarantees), [миграции](reliability.md#migrations), [проверки](reliability.md#checks) |
| [Запуск](running.md) | [Docker](running.md#docker), [хост](running.md#host), [остановка](running.md#stop), [диагностика](running.md#troubleshooting) |
| [Конфигурация](configuration.md) | [Валюты](configuration.md#currencies), [ENV в Compose](configuration.md#compose), [переменные](configuration.md#variables) |
| [Провайдер](provider.md) | [Выбор Frankfurter](provider.md#choice), [HTTP-клиент и ACL](provider.md#adapter), [ошибки](provider.md#failures), [замена](provider.md#replacement) |
| [Джобы](jobs.md) | [Polling](jobs.md#polling), [обработка](jobs.md#processing), [lease](jobs.md#lease), [retry](jobs.md#retry) |
| [Lifecycle](lifecycle.md) | [Владение ресурсами](lifecycle.md#ownership), [startup](lifecycle.md#startup), [контексты](lifecycle.md#contexts), [shutdown](lifecycle.md#shutdown), [границы гарантий](lifecycle.md#limits) |
| [Trade-offs](trade-offs.md) | [История проектирования](trade-offs.md#history), [принятые решения](trade-offs.md#agreed), [компромиссы](trade-offs.md#implementation), [границы гарантий](reliability.md#guarantees) |
| [Разработка](development.md) | [Тесты](development.md#tests), [CI](development.md#ci), [миграции](development.md#migrations), [расширение](development.md#changes) |
