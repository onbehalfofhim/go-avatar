# GophProfile

Сервис управления пользовательскими аватарами на Go.

GophProfile принимает изображения, сохраняет оригиналы в S3-совместимое хранилище, хранит метаданные в PostgreSQL и асинхронно создаёт thumbnails через RabbitMQ и отдельный worker.

## Возможности

- загрузка пользовательских аватаров;
- поддержка JPEG, PNG и WebP;
- ограничение размера файла — **10 MB**;
- хранение оригиналов в S3/MinIO;
- асинхронная обработка изображений;
- создание thumbnails:
  - `100x100`;
  - `300x300`;
- thumbnails сохраняются в JPEG;
- сохранение пропорций изображения с center crop перед resize;
- несколько аватаров на одного пользователя;
- один текущий (`active`) аватар на пользователя;
- история предыдущих аватаров;
- soft delete в PostgreSQL;
- асинхронное физическое удаление объектов из S3;
- idempotent processing сообщений RabbitMQ;
- retry с exponential backoff;
- dead-letter queue для сообщений, которые не удалось обработать;
- health-check PostgreSQL, S3 и RabbitMQ;
- web-интерфейс для загрузки и просмотра аватаров;
- Docker Compose для локального запуска;
- unit- и integration-тесты.

---

## Архитектура

Сервис состоит из HTTP API, PostgreSQL, MinIO и RabbitMQ. Обработка изображений и удаление файлов выполняются отдельным worker-процессом.

```text
                         ┌──────────────────┐
                         │    Web client    │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │   HTTP Server    │
                         │      Go API      │
                         └───────┬─────┬────┘
                                 │     │
                    metadata     │     │ original image
                                 │     │
                                 ▼     ▼
                         ┌──────────┐ ┌──────────┐
                         │PostgreSQL│ │  MinIO   │
                         │ metadata │ │   S3     │
                         └──────────┘ └────┬─────┘
                                           │
                                           │
                              ┌────────────▼────────────┐
                              │       RabbitMQ          │
                              │                         │
                              │ upload / delete events  │
                              │ retry queues / DLQ      │
                              └────────────┬────────────┘
                                           │
                                           ▼
                                  ┌─────────────────┐
                                  │      Worker     │
                                  │                 │
                                  │ thumbnails      │
                                  │ S3 deletion     │
                                  │ retries         │
                                  │ idempotency     │
                                  └─────────────────┘
```

### Upload flow

```text
Client
  │
  │ POST /api/v1/avatars
  ▼
HTTP Server
  │
  ├── validate request
  ├── save original to S3
  ├── create avatar record
  └── publish AvatarUploadEvent
          │
          ▼
      RabbitMQ
          │
          ▼
       Worker
          │
          ├── download original
          ├── decode image
          ├── center crop
          ├── create 100x100 thumbnail
          ├── create 300x300 thumbnail
          ├── upload thumbnails to S3
          └── mark processing as completed
```

### Delete flow

```text
Client
  │
  │ DELETE /api/v1/avatars/{id}
  ▼
HTTP Server
  │
  ├── mark avatar as deleted
  └── publish AvatarDeleteEvent
          │
          ▼
      RabbitMQ
          │
          ▼
       Worker
          │
          └── delete original + thumbnails from S3
```

Удаление является асинхронным: запись в PostgreSQL сохраняется, а физическое удаление файлов из S3 выполняется worker-ом.

---

## Технологический стек

| Компонент | Технология |
|---|---|
| Language | Go |
| HTTP | net/http |
| Database | PostgreSQL |
| Object storage | S3-compatible storage / MinIO |
| Message broker | RabbitMQ |
| Image processing | Go image packages + `golang.org/x/image` |
| Containers | Docker / Docker Compose |
| Tests | Go testing, testify, testcontainers-go |
| Static analysis | `go vet`, `golangci-lint` |

---

## Структура проекта

```text
.
├── cmd/
│   ├── server/
│   │   └── main.go
│   └── worker/
│       └── main.go
│
├── internal/
│   ├── broker/
│   │   ├── events/
│   │   └── rabbitmq/
│   │
│   ├── config/
│   │
│   ├── domain/
│   │
│   ├── health/
│   │
│   ├── http/
│   │   └── mocks/
│   │
│   ├── image/
│   │
│   ├── service/
│   │
│   ├── storage/
│   │   ├── postgres/
│   │   │   └── migrations/
│   │   └── s3/
│   │
│   └── worker/
│
├── web/
│   └── static/
│
├── .dockerignore
├── .env.example
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
└── README.md
```

### `cmd/server`

Точка входа HTTP-сервера.

Сервер создаёт подключения к PostgreSQL, MinIO и RabbitMQ, запускает миграции и HTTP API.

### `cmd/worker`

Точка входа фонового worker-а.

Worker подписывается на очереди обработки загрузок и удаления объектов.

### `internal/domain`

Доменные модели и статусы аватара.

### `internal/http`

HTTP handlers и маршрутизация API.

### `internal/storage/postgres`

Работа с PostgreSQL и миграции.

### `internal/storage/s3`

Работа с S3-совместимым object storage.

### `internal/broker/rabbitmq`

RabbitMQ client, consumer, exchange, queues и retry infrastructure.

### `internal/worker`

Фоновая обработка avatar events:

- создание thumbnails;
- удаление S3 objects;
- retries;
- idempotency.

### `internal/image`

Валидация изображений и создание thumbnails.

---

# Быстрый запуск

## Требования

Для локального запуска необходимы:

- Go 1.25+;
- Docker;
- Docker Compose;
- `golangci-lint` — для статического анализа.

Проверить версии:

```bash
go version
docker --version
docker compose version
golangci-lint --version
```

---

## 1. Клонирование

```bash
git clone https://github.com/onbehalfofhim/go-avatar.git
cd go-avatar
```

---

## 2. Конфигурация

Создать `.env`:

```bash
cp .env.example .env
```

Основные параметры:

```dotenv
POSTGRES_USER=avatar
POSTGRES_PASSWORD=avatar
POSTGRES_DB=avatar
POSTGRES_HOST=localhost
POSTGRES_PORT=5433

MINIO_ROOT_USER=minio
MINIO_ROOT_PASSWORD=miniosecret
MINIO_PORT=9000
MINIO_CONSOLE_PORT=9001

RABBITMQ_USER=avatar
RABBITMQ_PASSWORD=avatar
RABBITMQ_PORT=5672
RABBITMQ_MANAGEMENT_PORT=15672

HTTP_PORT=8080
```

Для запуска внутри Docker Compose сервисы используют внутренние имена:

```text
postgres:5432
minio:9000
rabbitmq:5672
```

---

## 3. Запуск инфраструктуры

Запустить PostgreSQL, MinIO и RabbitMQ:

```bash
docker compose up -d postgres minio rabbitmq
```

Проверить состояние:

```bash
docker compose ps
```

Все три инфраструктурных сервиса должны перейти в состояние `healthy`.

---

## 4. Запуск приложения

### HTTP server

В одном терминале:

```bash
go run ./cmd/server
```

### Worker

Во втором терминале:

```bash
go run ./cmd/worker
```

После запуска:

```text
HTTP API: http://localhost:8080
```

Web-интерфейс доступен по адресу:

```text
http://localhost:8080/
```

---

# Запуск всего проекта через Docker Compose

Можно собрать и запустить server и worker вместе с инфраструктурой:

```bash
docker compose up --build
```

В фоне:

```bash
docker compose up --build -d
```

Проверить состояние:

```bash
docker compose ps
```

Посмотреть логи:

```bash
docker compose logs -f server
```

```bash
docker compose logs -f worker
```

Остановить сервисы:

```bash
docker compose down
```

Удалить также persistent volumes:

```bash
docker compose down -v
```

> `docker compose down -v` удаляет локальные данные PostgreSQL, MinIO и RabbitMQ.

---

# Health check

```http
GET /health
```

Пример:

```bash
curl http://localhost:8080/health
```

Успешный ответ:

```json
{
  "checks": {
    "postgres": "ok",
    "rabbitmq": "ok",
    "s3": "ok"
  },
  "status": "ok"
}
```

Health endpoint проверяет доступность:

- PostgreSQL;
- RabbitMQ;
- S3/MinIO.

---

# API

Все API endpoints находятся под `/api/v1`.

## User ID

Аутентификация пользователя в MVP выполняется через HTTP header:

```http
X-User-ID: <user-id>
```

User ID может быть любой непустой строкой.

---

## Upload avatar

```http
POST /api/v1/avatars
```

Headers:

```http
X-User-ID: user-123
```

Request:

```text
multipart/form-data
```

Поле:

```text
file
```

Пример:

```bash
curl -X POST http://localhost:8080/api/v1/avatars \
  -H "X-User-ID: user-123" \
  -F "file=@./avatar.jpg"
```

Успешный ответ:

```http
201 Created
```

```json
{
  "id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "status": "pending"
}
```

После загрузки изображение обрабатывается асинхронно worker-ом.

Поддерживаемые форматы:

- JPEG;
- PNG;
- WebP.

Максимальный размер:

```text
10 MB
```

---

# Get avatar image

```http
GET /api/v1/avatars/{avatar_id}
```

По умолчанию возвращается оригинальное изображение.

### Original

```bash
curl http://localhost:8080/api/v1/avatars/{avatar_id}
```

### 100x100

```bash
curl "http://localhost:8080/api/v1/avatars/{avatar_id}?size=100"
```

### 300x300

```bash
curl "http://localhost:8080/api/v1/avatars/{avatar_id}?size=300"
```

Поддерживаемые значения `size`:

```text
100
300
```

Оригинал возвращается без указания `size`.

Thumbnails создаются только в JPEG.

Если thumbnail ещё не был создан worker-ом, соответствующий ресурс может быть недоступен до завершения обработки.

---

# Get current user avatar

```http
GET /api/v1/users/{user_id}/avatar
```

Пример:

```bash
curl http://localhost:8080/api/v1/users/user-123/avatar
```

Endpoint возвращает текущий активный аватар пользователя.

При загрузке нового аватара предыдущий текущий аватар становится неактивным.

---

# List user avatars

```http
GET /api/v1/users/{user_id}/avatars
```

Пример:

```bash
curl http://localhost:8080/api/v1/users/user-123/avatars
```

Endpoint возвращает историю аватаров пользователя.

В списке:

- присутствуют текущий и старые аватары;
- отсутствуют удалённые аватары;
- результаты отсортированы от новых к старым.

---

# Get avatar metadata

```http
GET /api/v1/avatars/{avatar_id}/metadata
```

Пример:

```bash
curl http://localhost:8080/api/v1/avatars/{avatar_id}/metadata
```

Метаданные включают информацию об аватаре, в частности:

- ID;
- user ID;
- имя исходного файла;
- MIME type;
- размер;
- статус загрузки;
- статус обработки;
- дату создания;
- дату обновления;
- информацию о thumbnails.

---

# Delete avatar

```http
DELETE /api/v1/avatars/{avatar_id}
```

Требуется:

```http
X-User-ID: user-123
```

Пример:

```bash
curl -X DELETE \
  http://localhost:8080/api/v1/avatars/{avatar_id} \
  -H "X-User-ID: user-123"
```

Удаление выполняется в два этапа:

1. avatar помечается удалённым в PostgreSQL;
2. публикуется `AvatarDeleteEvent`;
3. worker удаляет соответствующие объекты из S3.

Запись в PostgreSQL физически не удаляется.

Удалённый avatar:

- не возвращается через `GET /api/v1/avatars/{id}`;
- не возвращается в списке пользователя;
- остаётся в базе как историческая запись.

---

# Delete current user avatar

```http
DELETE /api/v1/users/{user_id}/avatar
```

Требуется:

```http
X-User-ID: user-123
```

Пример:

```bash
curl -X DELETE \
  http://localhost:8080/api/v1/users/user-123/avatar \
  -H "X-User-ID: user-123"
```

Удаляется текущий активный avatar пользователя.

Автоматическое назначение предыдущего avatar текущим после удаления не выполняется.

---

# Image processing

После успешной загрузки worker получает событие:

```json
{
  "avatar_id": "...",
  "user_id": "...",
  "s3_key": "avatars/.../original.jpg"
}
```

Worker:

1. скачивает оригинал из S3;
2. проверяет формат изображения;
3. декодирует изображение;
4. выполняет center crop до квадрата;
5. масштабирует изображение;
6. сохраняет thumbnail в JPEG;
7. загружает thumbnails в S3;
8. сохраняет S3 keys в PostgreSQL;
9. переводит processing status в `completed`.

Поддерживаются размеры:

```text
100x100
300x300
```

### Center crop

Исходное изображение сначала обрезается до центрального квадрата с сохранением пропорций содержимого, после чего масштабируется до необходимого размера.

---

# Storage

## PostgreSQL

PostgreSQL хранит metadata аватаров.

Основная таблица:

```text
avatars
```

В ней хранятся:

- `id`;
- `user_id`;
- `file_name`;
- `mime_type`;
- `size_bytes`;
- `s3_key`;
- `thumbnail_s3_keys`;
- `upload_status`;
- `processing_status`;
- `is_active`;
- `created_at`;
- `updated_at`;
- `deleted_at`.

Для пользователя гарантируется не более одного активного неудалённого avatar.

Это обеспечивается partial unique index:

```text
(user_id)
WHERE is_active = TRUE AND deleted_at IS NULL
```

Также используется таблица:

```text
processed_messages
```

для idempotency обработки RabbitMQ сообщений.

---

## S3 / MinIO

Оригинал и thumbnails хранятся в S3-compatible storage.

Пример структуры:

```text
avatars/
└── {avatar_id}/
    ├── original.jpg
    ├── 100x100.jpg
    └── 300x300.jpg
```

Конкретное расширение оригинала зависит от исходного формата.

Thumbnails всегда сохраняются как JPEG.

---

# RabbitMQ

Для асинхронной обработки используется RabbitMQ.

Основной exchange:

```text
avatars.exchange
```

Routing keys:

```text
avatar.uploaded
avatar.deleted
```

Основные очереди:

```text
avatars.processing
avatars.deletion
```

Для повторных попыток обработки используются retry queues с задержками:

```text
5 seconds
10 seconds
20 seconds
```

После исчерпания retry message попадает в dead-letter queue:

```text
avatars.dlq
```

### Upload event

```text
avatar.uploaded
```

Используется для запуска image processing.

### Delete event

```text
avatar.deleted
```

Используется для физического удаления файлов из S3.

---

# Retry и idempotency

Worker не удаляет сообщение сразу после получения.

При успешной обработке:

```text
process
  ↓
mark message as processed
  ↓
ACK
```

Для уже обработанного сообщения бизнес-операция повторно не выполняется.

Идентификатор RabbitMQ message используется как `message_id`.

Обработанные message IDs сохраняются в PostgreSQL:

```text
processed_messages
```

При ошибке worker использует retry queues:

```text
attempt 1 → 5s
attempt 2 → 10s
attempt 3 → 20s
```

Если все попытки исчерпаны, сообщение отправляется в DLQ.

---

# Avatar statuses

## Upload status

```text
uploaded
deleted
```

## Processing status

```text
pending
processing
completed
failed
```

После загрузки avatar первоначально находится в состоянии:

```text
upload_status = uploaded
processing_status = pending
```

После успешной обработки:

```text
processing_status = completed
```

При удалении:

```text
upload_status = deleted
is_active = false
deleted_at != NULL
```

---

# Database migrations

Миграции находятся в:

```text
internal/storage/postgres/migrations
```

Текущие миграции создают:

- `avatars`;
- `processed_messages`;
- необходимые индексы и ограничения.

Миграции применяются приложением при запуске.

---

# Testing

Запустить все тесты:

```bash
go test ./...
```

Запустить тесты с coverage:

```bash
go test -cover ./...
```


Для integration-тестов используются Docker-контейнеры необходимых зависимостей.

---

# Code quality

Форматирование:

```bash
gofmt -l .
```

Автоматическое форматирование:

```bash
gofmt -w .
```

Static analysis:

```bash
go vet ./...
```

Lint:

```bash
golangci-lint run
```

Перед отправкой изменений рекомендуется выполнить:

```bash
gofmt -w .
go vet ./...
go test ./...
golangci-lint run
```

Все проверки должны завершаться без ошибок.

---

# Docker

Проект использует multi-stage Dockerfile.

Доступны два target:

```text
server
worker
```

Собрать server:

```bash
docker build --target server -t gophprofile-server .
```

Собрать worker:

```bash
docker build --target worker --build-arg TARGET=worker -t gophprofile-worker .
```

Запуск production-like окружения рекомендуется выполнять через:

```bash
docker compose up --build
```

---

# Local infrastructure

Docker Compose поднимает следующие сервисы:

| Service | Port | Purpose |
|---|---:|---|
| PostgreSQL | `5433` | Metadata |
| MinIO API | `9000` | S3-compatible storage |
| MinIO Console | `9001` | Storage administration |
| RabbitMQ | `5672` | Message broker |
| RabbitMQ Management | `15672` | Broker administration |
| HTTP Server | `8080` | REST API + Web UI |

MinIO Console:

```text
http://localhost:9001
```

RabbitMQ Management UI:

```text
http://localhost:15672
```

Credentials задаются через `.env`.

---

# Web interface

Сервис также содержит простой web-интерфейс для работы с avatar API.

После запуска server откройте:

```text
http://localhost:8080/
```

Web UI позволяет выполнять основные операции с аватарами через HTTP API.

---

# Example: complete upload flow

Создадим avatar:

```bash
curl -X POST http://localhost:8080/api/v1/avatars \
  -H "X-User-ID: user-123" \
  -F "file=@./avatar.png"
```

Ответ:

```json
{
  "id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "status": "pending"
}
```

Проверим metadata:

```bash
curl \
  http://localhost:8080/api/v1/avatars/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx/metadata
```

После завершения обработки можно получить оригинал:

```bash
curl \
  http://localhost:8080/api/v1/avatars/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx \
  --output avatar.jpg
```

Thumbnail:

```bash
curl \
  "http://localhost:8080/api/v1/avatars/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx?size=100" \
  --output avatar-100.jpg
```

И второй размер:

```bash
curl \
  "http://localhost:8080/api/v1/avatars/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx?size=300" \
  --output avatar-300.jpg
```

---

# Error handling

API валидирует:

- наличие `X-User-ID`, когда он требуется;
- непустой user ID;
- наличие multipart file;
- размер файла;
- фактический формат изображения;
- существование avatar;
- принадлежность avatar пользователю при операциях удаления;
- допустимое значение `size`.

Worker отдельно обрабатывает ошибки:

- RabbitMQ;
- PostgreSQL;
- S3;
- декодирования изображений;
- создания thumbnails.

Ошибки фоновой обработки не блокируют HTTP request после постановки операции в очередь.

---

# Development workflow

Основная ветка разработки:

```text
dev
```

Перед изменениями:

```bash
git checkout dev
git pull
```

После изменений рекомендуется проверить:

```bash
gofmt -w .
go vet ./...
go test ./...
golangci-lint run
```

Проверить состояние:

```bash
git status
```