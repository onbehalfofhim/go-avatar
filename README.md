# Avatar Service

Avatar Service — сервис управления пользовательскими аватарами на Go.

Сервис принимает изображения, сохраняет оригиналы в S3-совместимом хранилище, хранит метаданные в PostgreSQL и асинхронно создаёт thumbnails отдельным worker-процессом через RabbitMQ.

## Возможности

- загрузка пользовательских аватаров;
- поддержка JPEG, PNG и WebP;
- ограничение размера изображения — 10 MiB;
- хранение оригиналов и thumbnails в S3/MinIO;
- асинхронная обработка изображений;
- thumbnails размером `100x100` и `300x300`;
- thumbnails сохраняются в JPEG;
- center crop с последующим resize;
- несколько аватаров на одного пользователя;
- не более одного активного аватара пользователя;
- история предыдущих аватаров;
- soft delete в PostgreSQL;
- асинхронное физическое удаление объектов из S3;
- idempotent processing RabbitMQ-сообщений;
- retry с задержками `5s`, `10s`, `20s`;
- dead-letter queue для сообщений, которые не удалось обработать;
- transactional outbox для надёжной передачи событий из PostgreSQL в RabbitMQ;
- health check PostgreSQL, S3 и RabbitMQ;
- встроенный web-интерфейс;
- unit- и integration-тесты.

## Архитектура

Сервис состоит из HTTP API, PostgreSQL, S3-compatible storage (MinIO), RabbitMQ и отдельного worker-процесса.

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
                      metadata   │     │ original image
                                 │     │
                                 ▼     ▼
                         ┌──────────┐ ┌──────────┐
                         │PostgreSQL│ │  MinIO   │
                         │ metadata │ │   S3     │
                         └─────┬────┘ └────┬─────┘
                               │           │
                         outbox events     │
                               │           │
                               ▼           │
                         ┌──────────┐      │
                         │  Outbox  │      │
                         │ publisher│      │
                         └─────┬────┘      │
                               │           │
                               ▼           │
                         ┌─────────────────┐
                         │    RabbitMQ      │
                         │ events / retry   │
                         │       / DLQ      │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌─────────────────┐
                         │     Worker      │
                         │ thumbnails /    │
                         │ S3 deletion /   │
                         │ retry / idempot. │
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
  ├── validate request and image
  ├── save original to S3
  └── PostgreSQL transaction:
        ├── create avatar
        └── create outbox event
                │
                ▼
          Outbox publisher
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

Transactional outbox позволяет не терять событие между изменением состояния в PostgreSQL и публикацией в RabbitMQ: событие сначала сохраняется в `outbox_events`, а отдельный publisher публикует его в RabbitMQ.

### Delete flow

```text
Client
  │
  │ DELETE /api/v1/avatars/{id}
  ▼
HTTP Server
  │
  └── PostgreSQL transaction:
        ├── mark avatar as deleted
        └── create outbox event
                │
                ▼
          Outbox publisher
                │
                ▼
            RabbitMQ
                │
                ▼
             Worker
                │
                └── delete original + thumbnails from S3
```

Удаление является асинхронным: запись в PostgreSQL сохраняется как soft-deleted, а физическое удаление объектов из S3 выполняется worker-ом.

## Технологический стек

| Компонент | Технология |
|---|---|
| Language | Go 1.26 |
| HTTP | `net/http` + `chi` |
| Database | PostgreSQL |
| Object storage | S3-compatible storage / MinIO |
| Message broker | RabbitMQ |
| Image processing | Go image packages + `golang.org/x/image` |
| Containers | Docker / Docker Compose |
| Tests | Go testing + Testify |
| Static analysis | `go vet`, `golangci-lint` |

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
│   ├── config/
│   ├── domain/
│   ├── health/
│   ├── http/
│   │   └── mocks/
│   ├── image/
│   ├── service/
│   ├── storage/
│   │   ├── postgres/
│   │   │   └── migrations/
│   │   └── s3/
│   └── worker/
│
├── web/
│   └── static/
│
├── api/
│   └── openapi.yaml
│
├── docs/
│   ├── architecture-k8s.md
│   └── rancher-desktop.md
│
├── helm/
│   └── avatar-service/
│
├── internal/resilience/
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

При запуске сервер:

- загружает конфигурацию;
- подключается к PostgreSQL;
- применяет embedded database migrations;
- подключается к MinIO/S3;
- создаёт bucket при необходимости;
- подключается к RabbitMQ;
- запускает HTTP API и web-интерфейс.

### `cmd/worker`

Точка входа фонового worker-процесса.

Worker:

- получает upload/delete события из RabbitMQ;
- выполняет обработку изображений;
- удаляет объекты из S3;
- отслеживает обработанные message IDs;
- выполняет retry и отправляет окончательно неуспешные сообщения в DLQ.

### `internal/domain`

Доменные модели аватара, статусы и модель outbox-события.

### `internal/http`

HTTP handlers, маршрутизация и HTTP-модели ответов.

### `internal/service`

Бизнес-логика работы с аватарами.

### `internal/storage/postgres`

Работа с PostgreSQL, репозитории и database migrations.

### `internal/storage/s3`

Работа с S3-compatible object storage.

### `internal/broker/rabbitmq`

RabbitMQ client, topology, producer, consumer, retry queues и DLQ.

### `internal/worker`

Обработка RabbitMQ-сообщений, генерация thumbnails, удаление S3-объектов и idempotency.

### `internal/image`

Проверка формата изображения, декодирование, center crop и генерация thumbnails.

## Быстрый запуск

### Требования

- Go 1.26;
- Docker;
- Docker Compose;
- `golangci-lint` — только если требуется запускать lint локально.

Проверить версии:

```bash
go version
docker --version
docker compose version
```

### 1. Клонирование

```bash
git clone https://github.com/onbehalfofhim/go-avatar.git
cd go-avatar
```

### 2. Конфигурация

Для Docker Compose достаточно создать `.env` из примера:

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

MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=minio
MINIO_SECRET_KEY=miniosecret
MINIO_BUCKET=avatars
MINIO_USE_SSL=false

RABBITMQ_USER=avatar
RABBITMQ_PASSWORD=avatar
RABBITMQ_PORT=5672
RABBITMQ_MANAGEMENT_PORT=15672
RABBITMQ_URL=amqp://avatar:avatar@localhost:5672/

HTTP_PORT=8080
```

Секретные параметры, необходимые приложению (`POSTGRES_PASSWORD`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` и `RABBITMQ_URL`), должны быть заданы явно.

### 3. Запуск через Docker Compose

Рекомендуемый способ запуска:

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

После запуска:

- Web UI: `http://localhost:8080/`
- HTTP API: `http://localhost:8080`
- Health: `http://localhost:8080/health`
- MinIO Console: `http://localhost:9001`
- RabbitMQ Management UI: `http://localhost:15672`

Остановить сервисы:

```bash
docker compose down
```

Удалить также persistent volumes:

```bash
docker compose down -v
```

> `docker compose down -v` удаляет локальные данные PostgreSQL, MinIO и RabbitMQ.

### Запуск server и worker без контейнеров приложения

Можно запустить инфраструктуру отдельно:

```bash
docker compose up -d postgres minio rabbitmq
```

Перед запуском Go-процессов переменные из `.env` должны быть доступны в окружении shell:

```bash
set -a
source .env
set +a
```

Запустить HTTP server:

```bash
go run ./cmd/server
```

В другом терминале запустить worker:

```bash
go run ./cmd/worker
```

## Health check

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
  "status": "ok",
  "checks": {
    "postgres": "ok",
    "rabbitmq": "ok",
    "s3": "ok"
  }
}
```

Если хотя бы одна зависимость недоступна, endpoint возвращает HTTP `503 Service Unavailable` и показывает состояние отдельных checks.

## API

Все API endpoints находятся под `/api/v1`.

### User ID

В MVP идентификатор пользователя передаётся через HTTP header:

```http
X-User-ID: <user-id>
```

Для `DELETE /api/v1/users/{user_id}/avatar` значение header должно совпадать с `{user_id}`.

---

### Upload avatar

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

Поле файла:

```text
file
```

Также handler принимает поле `image`.

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

Поддерживаемые форматы:

- JPEG;
- PNG;
- WebP.

Максимальный размер изображения — `10 MiB`.

При превышении лимита возвращается `413 Request Entity Too Large`.

После загрузки изображение обрабатывается worker-ом асинхронно.

---

### Get avatar image

```http
GET /api/v1/avatars/{avatar_id}
```

По умолчанию возвращается оригинальное изображение.

Оригинал:

```bash
curl http://localhost:8080/api/v1/avatars/{avatar_id}
```

Thumbnail `100x100`:

```bash
curl "http://localhost:8080/api/v1/avatars/{avatar_id}?size=100"
```

Thumbnail `300x300`:

```bash
curl "http://localhost:8080/api/v1/avatars/{avatar_id}?size=300"
```

Поддерживаемые значения `size`:

- отсутствует или `original` — оригинал;
- `100` — thumbnail `100x100`;
- `300` — thumbnail `300x300`.

Thumbnails создаются только в JPEG.

Если запрошенный thumbnail ещё не создан worker-ом, endpoint возвращает `404`.

---

### Get current user avatar

```http
GET /api/v1/users/{user_id}/avatar
```

Пример:

```bash
curl http://localhost:8080/api/v1/users/user-123/avatar
```

Endpoint возвращает текущий активный аватар пользователя.

При загрузке нового аватара предыдущий активный аватар становится неактивным.

---

### List user avatars

```http
GET /api/v1/users/{user_id}/avatars
```

Пример:

```bash
curl http://localhost:8080/api/v1/users/user-123/avatars
```

Endpoint возвращает историю неудалённых аватаров пользователя.

Результаты отсортированы от новых к старым.

Удалённые аватары в список не входят.

---

### Get avatar metadata

```http
GET /api/v1/avatars/{avatar_id}/metadata
```

Пример:

```bash
curl http://localhost:8080/api/v1/avatars/{avatar_id}/metadata
```

Ответ содержит:

- `id`;
- `user_id`;
- `file_name`;
- `mime_type`;
- `size_bytes`;
- `upload_status`;
- `processing_status`;
- `is_active`;
- `created_at`;
- `updated_at`;
- `deleted_at`.

---

### Delete avatar

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

Успешный ответ:

```http
204 No Content
```

Удаление выполняется асинхронно:

1. аватар помечается удалённым в PostgreSQL;
2. событие удаления попадает в outbox;
3. outbox publisher публикует событие в RabbitMQ;
4. worker удаляет оригинал и thumbnails из S3.

Если аватар существует, но принадлежит другому пользователю, возвращается:

```http
403 Forbidden
```

Удалённая запись физически не удаляется из PostgreSQL.

---

### Delete current user avatar

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

Удаляется текущий активный аватар пользователя.

После удаления предыдущий аватар автоматически активным не становится.

Успешный ответ:

```http
204 No Content
```

## Image processing

Worker обрабатывает upload event и:

1. скачивает оригинал из S3;
2. декодирует изображение;
3. выполняет center crop до квадрата;
4. масштабирует изображение;
5. создаёт thumbnail `100x100`;
6. создаёт thumbnail `300x300`;
7. сохраняет thumbnails в JPEG;
8. загружает thumbnails в S3;
9. сохраняет S3 keys в PostgreSQL;
10. переводит `processing_status` в `completed`.

Пример структуры объектов:

```text
avatars/
└── {avatar_id}/
    ├── original.jpg
    ├── 100x100.jpg
    └── 300x300.jpg
```

Расширение оригинала зависит от исходного формата.

## Storage

### PostgreSQL

Основная таблица:

```text
avatars
```

Хранит metadata аватаров:

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

Для пользователя допускается не более одного активного неудалённого аватара. Это обеспечивается partial unique index.

Также используются:

```text
processed_messages
```

для idempotency RabbitMQ-сообщений и:

```text
outbox_events
```

для transactional outbox.

Статусы `upload_status` и `processing_status` представлены PostgreSQL ENUM.

### S3 / MinIO

Оригиналы и thumbnails хранятся в S3-compatible object storage.

MinIO используется в локальном окружении.

## RabbitMQ

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

Retry queues используют задержки:

```text
5 seconds
10 seconds
20 seconds
```

После исчерпания retry сообщение отправляется в:

```text
avatars.dlq
```

### Upload event

```text
avatar.uploaded
```

Запускает обработку изображения worker-ом.

### Delete event

```text
avatar.deleted
```

Запускает физическое удаление объектов из S3.

## Retry и idempotency

Worker не подтверждает сообщение до завершения бизнес-операции.

Упрощённый успешный flow:

```text
receive message
      ↓
check message_id
      ↓
process
      ↓
mark message as processed
      ↓
ACK
```

Обработанные message IDs сохраняются в PostgreSQL в таблице `processed_messages`.

Если обработка завершается ошибкой, worker публикует сообщение в соответствующую retry queue.

```text
attempt 1 → 5s
attempt 2 → 10s
attempt 3 → 20s
```

После исчерпания retry сообщение отклоняется и попадает в DLQ.

## Avatar statuses

### Upload status

```text
uploaded
deleted
```

### Processing status

```text
pending
processing
completed
failed
```

После загрузки:

```text
upload_status = uploaded
processing_status = pending
is_active = true
```

После успешной обработки:

```text
processing_status = completed
```

После удаления:

```text
upload_status = deleted
is_active = false
deleted_at != NULL
```

## Database migrations

Миграции находятся в:

```text
internal/storage/postgres/migrations
```

Они создают:

- `avatars`;
- `processed_messages`;
- `outbox_events`;
- PostgreSQL ENUM для avatar statuses;
- индексы и ограничения.

При запуске через Docker Compose HTTP server применяет embedded migrations автоматически.

В Kubernetes автоматический запуск migrations из нескольких server replicas отключён. Helm выполняет отдельный migration Job как `pre-install` / `pre-upgrade` hook, после чего запускаются application Deployments.

## Testing

Запустить все тесты:

```bash
go test ./...
```

Запустить с coverage:

```bash
go test -cover ./...
```

Integration-тесты PostgreSQL, S3 и RabbitMQ используют внешние сервисы из конфигурации. Они не поднимают зависимости через Testcontainers.

Для локального запуска integration-тестов удобно предварительно запустить инфраструктуру:

```bash
docker compose up -d postgres minio rabbitmq
```

и загрузить переменные окружения:

```bash
set -a
source .env
set +a
```

Затем:

```bash
go test ./...
```

Для проверки race conditions:

```bash
go test -race ./...
```

## Code quality

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

## Docker

Проект использует multi-stage Dockerfile.

Доступны два target:

```text
server
worker
```

Собрать server:

```bash
docker build --target server -t avatar-service-server:dev .
```

Собрать worker:

```bash
docker build --target worker --build-arg TARGET=worker -t avatar-service-worker:dev .
```

Для локального запуска всего окружения рекомендуется:

```bash
docker compose up --build
```

## Local infrastructure

Docker Compose поднимает:

| Service | Port | Purpose |
|---|---:|---|
| PostgreSQL | `5433` | Metadata |
| MinIO API | `9000` | S3-compatible storage |
| MinIO Console | `9001` | Storage administration |
| RabbitMQ | `5672` | Message broker |
| RabbitMQ Management | `15672` | Broker administration |
| HTTP Server | `8080` | REST API + Web UI |

Management interfaces:

- MinIO Console: `http://localhost:9001`
- RabbitMQ Management UI: `http://localhost:15672`

# Kubernetes / Helm

Проект подготовлен для локального Kubernetes-кластера в Rancher Desktop.

В Kubernetes разворачиваются два компонента приложения:

- `server` — HTTP API и web UI;
- `worker` — асинхронная обработка RabbitMQ-сообщений.

PostgreSQL, RabbitMQ, MinIO и Jaeger в локальном сценарии остаются внешними зависимостями и могут запускаться через Docker Compose. Kubernetes Pod'ы обращаются к ним через `host.rancher-desktop.internal`.

## Kubernetes prerequisites

Требуются:

- Rancher Desktop с включённым Kubernetes;
- `kubectl`;
- Helm 3;
- Metrics Server для HPA;
- встроенный в Rancher Desktop Traefik Ingress Controller;
- Prometheus Operator / kube-prometheus-stack для `ServiceMonitor`.

Подробная инструкция находится в [`docs/rancher-desktop.md`](docs/rancher-desktop.md).

## Helm Chart

Chart расположен в:

```text
helm/avatar-service/
```

Основные ресурсы:

- Server Deployment;
- Worker Deployment;
- ClusterIP Services;
- Ingress;
- ConfigMap;
- Secret;
- ServiceAccount;
- Role / RoleBinding без разрешений Kubernetes API;
- HPA;
- ServiceMonitor для server и worker;
- NetworkPolicy;
- Helm migration Job.

## Сборка локальных образов

Для Rancher Desktop с containerd:

```bash
nerdctl --namespace k8s.io build --target server -t avatar-service-server:dev .
nerdctl --namespace k8s.io build --target worker -t avatar-service-worker:dev .
```

Для Rancher Desktop с Docker/Moby:

```bash
docker build --target server -t avatar-service-server:dev .
docker build --target worker -t avatar-service-worker:dev .
```

## Установка в Rancher Desktop

Сначала запустите внешние зависимости:

```bash
docker compose up -d postgres minio rabbitmq jaeger
```

Создайте локальный values-файл:

```bash
cp helm/avatar-service/values-rancher-desktop.example.yaml \
   helm/avatar-service/values-rancher-desktop.yaml
```

Создайте локальный values-файл. Он содержит локальные credentials и не должен коммититься в Git:

```bash
cp helm/avatar-service/values-rancher-desktop.example.yaml \
  helm/avatar-service/values-rancher-desktop.yaml
```

Создайте namespace:

```bash
kubectl create namespace avatar --dry-run=client -o yaml | kubectl apply -f -
```

Проверьте Chart:

```bash
helm lint helm/avatar-service
helm template avatar-service helm/avatar-service \
  --namespace avatar \
  -f helm/avatar-service/values-rancher-desktop.yaml
```

Установите релиз:

```bash
helm upgrade --install avatar-service helm/avatar-service \
  --namespace avatar \
  -f helm/avatar-service/values-rancher-desktop.yaml \
  --wait \
  --timeout 5m
```

Проверка:

```bash
helm status avatar-service -n avatar
kubectl get pods -n avatar -o wide
kubectl get deploy -n avatar
kubectl get svc -n avatar
kubectl get ingress -n avatar
kubectl get hpa -n avatar
kubectl get servicemonitor -n avatar
kubectl get networkpolicy -n avatar
kubectl get jobs -n avatar
```

Успешный migration Job удаляется Helm hook'ом после выполнения, поэтому отсутствие Job после успешного deployment является ожидаемым поведением.

## Health probes

Для Kubernetes разделены две проверки:

```text
GET /health/live   -> liveness
GET /health/ready  -> readiness
GET /health        -> readiness compatibility endpoint
```

`/health/live` не зависит от PostgreSQL, RabbitMQ или MinIO и показывает, что процесс жив.

`/health/ready` проверяет внешние зависимости. При получении `SIGTERM` приложение сначала переводит readiness в состояние `draining`, а затем выполняет graceful shutdown.

## HPA

Server HPA настроен по CPU и memory:

- minimum: 2 replicas;
- maximum: 10 replicas;
- CPU target: 70%;
- memory target: 80%.

Для работы HPA в кластере должен быть доступен Metrics Server:

```bash
kubectl top nodes
kubectl top pods -n gophprofile
```

## Monitoring

Application metrics уже предоставляются через `/metrics` на порту `9091`.

Helm создаёт `ServiceMonitor` для:

- `server`;
- `worker`.

Prometheus Operator должен быть установлен в Kubernetes-кластере.

## Security

Kubernetes deployment использует:

- отдельный ServiceAccount;
- `automountServiceAccountToken: false`;
- пустой Role без разрешений Kubernetes API;
- `runAsNonRoot: true`;
- `allowPrivilegeEscalation: false`;
- `readOnlyRootFilesystem: true`;
- `capabilities.drop: [ALL]`;
- `seccompProfile: RuntimeDefault`;
- NetworkPolicy для server и worker;
- Secret для PostgreSQL/MinIO/RabbitMQ credentials.

Docker image также уже запускается от `nobody:nobody`.

## Rate limiting

В chart предусмотрены NGINX-specific annotations для rate limiting и ограничения размера request body. Однако в проверенном Rancher Desktop окружении используется встроенный Traefik, поэтому NGINX annotations не применяются Traefik автоматически.

Ограничение размера изображения на уровне HTTP API — `10 MiB` — продолжает действовать независимо от Ingress Controller.

Перед использованием rate limiting в production необходимо настроить соответствующий Traefik Middleware либо использовать NGINX Ingress с текущими annotations. README не считает NGINX rate limiting включённым в Rancher Desktop сценарии, пока это явно не настроено на уровне Traefik.

## Circuit breaker

Для runtime-операций с внешними сетевыми зависимостями добавлен circuit breaker:

- RabbitMQ publish operations;
- MinIO object operations.

Текущая политика:

- 5 последовательных ошибок открывают circuit;
- 15 секунд circuit остаётся открытым;
- затем выполняется half-open пробный запрос;
- успешный запрос возвращает circuit в closed state.

## Database migrations

В локальном Docker Compose server сохраняет автоматический запуск embedded migrations.

В Kubernetes server запускается с:

```text
RUN_MIGRATIONS=false
```

а Helm выполняет отдельный `pre-install` / `pre-upgrade` Job в migration-only режиме.

Это предотвращает одновременный запуск миграций несколькими server replicas.

## Observability в Kubernetes

Application metrics доступны на порту `9091` через endpoint `/metrics`.

Helm создаёт отдельный `ServiceMonitor` для server и worker. Для автоматического discovery ресурсов `ServiceMonitor` в кластере должен быть установлен Prometheus Operator / kube-prometheus-stack.

Проверка наличия Metrics Server:

```bash
kubectl top nodes
kubectl top pods -n avatar
```

Проверка ServiceMonitor:

```bash
kubectl get servicemonitor -n avatar
kubectl describe servicemonitor -n avatar
```

HPA server использует CPU и memory metrics:

- minimum: 2 replicas;
- maximum: 10 replicas;
- CPU target: 70%;
- memory target: 80%.

## NetworkPolicy и доступ к внешним зависимостям

В Kubernetes server и worker работают с NetworkPolicy. В Rancher Desktop ingress traffic от Traefik разрешён из namespace `kube-system`.

Внешние зависимости в текущем локальном сценарии остаются за пределами Kubernetes:

```text
Kubernetes
  ├── server
  └── worker
       │
       ├── PostgreSQL   -> host.rancher-desktop.internal:5433
       ├── RabbitMQ     -> host.rancher-desktop.internal:5672
       ├── MinIO        -> host.rancher-desktop.internal:9000
       └── Jaeger       -> host.rancher-desktop.internal:4317
```

## Kubernetes configuration and secrets

Конфигурация приложения передаётся через ConfigMap. Credentials для PostgreSQL, MinIO и RabbitMQ передаются через Kubernetes Secret.

Для локального Rancher Desktop используется файл:

```text
helm/avatar-service/values-rancher-desktop.yaml
```

Этот файл создаётся из `values-rancher-desktop.example.yaml` и должен оставаться локальным. Он добавлен в `.gitignore`.

Для production рекомендуется использовать заранее созданный Kubernetes Secret через `secrets.existingSecret`, а не хранить credentials в values-файле.

## OpenAPI

Актуальная API specification находится в:

```text
api/openapi.yaml
```

## Архитектура Kubernetes

Диаграмма компонентов находится в [`docs/architecture-k8s.md`](docs/architecture-k8s.md).

## Проверка через Ingress

В Rancher Desktop используется Traefik. Helm values для Rancher Desktop задают:

```yaml
ingress:
  className: traefik
  host: avatars.localhost
```

Проверить Ingress:

```bash
kubectl get ingress -n avatar
```

Smoke test:

```bash
curl -i http://avatars.localhost/health/live
curl -i http://avatars.localhost/health/ready
curl -s http://avatars.localhost/metrics | head -30
```

Ожидаемый ответ `/health/live`:

```json
{"status":"ok"}
```

## Проверка graceful shutdown

```bash
kubectl delete pod -n avatar -l app.kubernetes.io/component=server --wait=false
kubectl get pods -n avatar -w
```

Приложение получает `SIGTERM`, переводит readiness в `draining`, завершает HTTP/metrics server и закрывает внешние подключения в рамках `30s terminationGracePeriodSeconds`.
