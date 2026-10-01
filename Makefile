ENV_FILE := deploy/.env.local
COMPOSE := docker compose --env-file $(ENV_FILE) -f deploy/docker-compose.yml
LOCAL_DATABASE_DSN = postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@localhost:$${POSTGRES_PORT}/$${POSTGRES_DB}?sslmode=disable

.PHONY: migrate run-service test test-unit docker-up docker-down docker-ps docker-logs

migrate:
	set -a; . $(ENV_FILE); set +a; \
		DATABASE_DSN="$(LOCAL_DATABASE_DSN)" go run ./cmd/migrate

run-service:
	set -a; . $(ENV_FILE); set +a; \
		DATABASE_DSN="$(LOCAL_DATABASE_DSN)" HTTP_ADDR="$${HTTP_ADDR:-:$${HTTP_PORT:-8080}}" go run ./cmd/service

test:
	go test ./...

test-unit:
	go test ./internal/... ./pkg/...

docker-up:
	$(COMPOSE) up --build --force-recreate --remove-orphans -d

docker-down:
	$(COMPOSE) down --remove-orphans

docker-ps:
	$(COMPOSE) ps

docker-logs:
	$(COMPOSE) logs -f --tail=200
