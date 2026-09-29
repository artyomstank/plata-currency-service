ENV_FILE := deploy/.env.local
COMPOSE := docker compose --env-file $(ENV_FILE) -f deploy/docker-compose.yml
LOCAL_DATABASE_DSN = postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@localhost:$${POSTGRES_PORT}/$${POSTGRES_DB}?sslmode=disable

.PHONY: generate migrate run-service run-gateway test test-unit docker-up docker-down docker-ps docker-logs

generate:
	buf generate

migrate:
	set -a; . $(ENV_FILE); set +a; \
		DATABASE_DSN="$(LOCAL_DATABASE_DSN)" go run ./cmd/migrate

run-service:
	set -a; . $(ENV_FILE); set +a; \
		DATABASE_DSN="$(LOCAL_DATABASE_DSN)" go run ./cmd/service

run-gateway:
	go run ./cmd/gateway

test:
	go test ./...

test-unit:
	go test ./internal/config ./internal/provider ./internal/storage ./internal/usecase ./internal/worker

docker-up:
	$(COMPOSE) up --build --force-recreate -d

docker-down:
	$(COMPOSE) down

docker-ps:
	$(COMPOSE) ps

docker-logs:
	$(COMPOSE) logs -f --tail=200
