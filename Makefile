.PHONY: help proto build up down test migrate clean logs status health

help:
	@echo "Music Streaming Platform - Makefile Commands"
	@echo "============================================="
	@echo "make proto      - Generate protobuf code"
	@echo "make build      - Build all Docker images"
	@echo "make up         - Start all services"
	@echo "make down       - Stop all services"
	@echo "make test       - Run all tests"
	@echo "make test-unit  - Run unit tests"
	@echo "make test-integration - Run integration tests"
	@echo "make migrate-up - Run database migrations"
	@echo "make migrate-down - Rollback migrations"
	@echo "make clean      - Clean everything"
	@echo "make logs       - View all logs"
	@echo "make status     - Check service status"
	@echo "make health     - Check health endpoints"

proto:
	@echo "Generating protobuf code..."
	@cd proto && protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		user.proto music.proto payment.proto
	@echo "Protobuf generation complete"

build:
	@echo "Building Docker images..."
	@docker-compose build --parallel
	@echo "Build complete"

up:
	@echo "Starting all services..."
	@docker-compose up -d
	@echo "Waiting for services to be ready..."
	@sleep 15
	@make status
	@echo ""
	@echo "Services started successfully!"
	@echo "Access the platform at:"
	@echo "  Frontend:    http://localhost:3000"
	@echo "  API Gateway: http://localhost:8080"
	@echo "  Grafana:     http://localhost:3001 (admin/admin)"
	@echo "  Jaeger:      http://localhost:16686"
	@echo "  Prometheus:  http://localhost:9090"

down:
	@echo "Stopping all services..."
	@docker-compose down
	@echo "Services stopped"

test-unit:
	@echo "Running unit tests..."
	@go test ./... -v -cover -short

test-integration:
	@echo "Running integration tests..."
	@go test -tags=integration ./... -v -cover

test: test-unit test-integration
	@echo "All tests complete"

migrate-up:
	@echo "Running migrations..."
	@docker-compose run --rm user-service ./migrate up
	@docker-compose run --rm music-service ./migrate up
	@docker-compose run --rm payment-service ./migrate up
	@echo "Migrations complete"

migrate-down:
	@echo "Rolling back migrations..."
	@docker-compose run --rm user-service ./migrate down
	@docker-compose run --rm music-service ./migrate down
	@docker-compose run --rm payment-service ./migrate down
	@echo "Rollback complete"

clean:
	@echo "Cleaning everything..."
	@docker-compose down -v
	@docker system prune -f
	@echo "Clean complete"

logs:
	@docker-compose logs -f

status:
	@docker-compose ps

health:
	@echo "Checking service health..."
	@curl -s http://localhost:8080/health | jq . || echo "API Gateway not ready"
	@echo ""
	@docker-compose ps

restart:
	@echo "Restarting all services..."
	@docker-compose restart
	@echo "Services restarted"

.PHONY: all
all: proto build up migrate-up
	@echo "Platform fully deployed!"