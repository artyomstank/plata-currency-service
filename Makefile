.PHONY: generate run-service run-gateway docker-up

generate:
	buf generate

run-service:
	go run ./cmd/service

run-gateway:
	go run ./cmd/gateway

docker-up:
	docker compose -f deploy/docker-compose.yml up --build
