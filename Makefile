# Pivox Makefile
# Tools are managed in ./tools/go.mod and run via `go tool -modfile`

TOOLS_MOD = ./tools/go.mod

# Database
DATABASE_URL ?= postgres://pivox:pivox@localhost:5432/pivox?sslmode=disable
DATABASE_NAME ?= pivox
MIGRATIONS_DIR = internal/db/migrations

# Proto
PROTO_DIR = api/proto
API_LINTER_CONFIG = api/proto/api-linter.yaml

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Development

.PHONY: build
build: ## Build the server binary
	go build -o bin/server ./cmd/server

.PHONY: run
run: ## Run the server
	go run ./cmd/server

.PHONY: test
test: ## Run tests
	go test ./...

.PHONY: tidy
tidy: ## Run go mod tidy for all modules
	go mod tidy
	cd tools && go mod tidy

##@ Linting

.PHONY: lint
lint: ## Run golangci-lint
	go tool -modfile=$(TOOLS_MOD) golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	go tool -modfile=$(TOOLS_MOD) golangci-lint run --fix ./...

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w .

##@ Proto / Code Generation

.PHONY: proto-lint
proto-lint: ## Lint proto files with buf
	go tool -modfile=$(TOOLS_MOD) buf lint

.PHONY: proto-format
proto-format: ## Format proto files with buf
	go tool -modfile=$(TOOLS_MOD) buf format -w

.PHONY: proto-breaking
proto-breaking: ## Check for breaking proto changes against main
	go tool -modfile=$(TOOLS_MOD) buf breaking --against '.git#branch=main'

.PHONY: proto-generate
proto-generate: ## Generate Go code from proto files
	go tool -modfile=$(TOOLS_MOD) buf generate

.PHONY: api-lint
api-lint: ## Lint proto files with Google API linter
	go tool -modfile=$(TOOLS_MOD) api-linter \
		--proto-path=$(PROTO_DIR) \
		--config=$(API_LINTER_CONFIG) \
		--set-exit-status \
		$(PROTO_DIR)/pivox/**/**/**/*.proto

##@ Database Migrations (requires migrate in PATH; go tool lacks -tags support: golang/go#71503)

.PHONY: migrate-up
migrate-up: ## Run all pending migrations
	migrate \
		-path $(MIGRATIONS_DIR) \
		-database "$(DATABASE_URL)" \
		up

.PHONY: migrate-down
migrate-down: ## Rollback the last migration
	migrate \
		-path $(MIGRATIONS_DIR) \
		-database "$(DATABASE_URL)" \
		down 1

.PHONY: migrate-create
migrate-create: ## Create a new migration (usage: make migrate-create NAME=create_users)
	just migrate-create $(NAME)

.PHONY: migrate-force
migrate-force: ## Force migration version (usage: make migrate-force VERSION=1)
	just migrate-force $(VERSION)

.PHONY: seed
seed: ## Seed the database
	just seed

.PHONY: db-clear
db-clear: ## Clear all data from the database (truncate all tables)
	just db-clear

.PHONY: db-drop
db-drop: ## Drop the database
	just db-drop

.PHONY: db-create
db-create: ## Create the database
	just db-create

##@ Proxy / Tunnel

.PHONY: nginx
nginx: ## Start nginx reverse proxy on :8081
	nginx -c $(CURDIR)/configs/nginx.conf -e stderr

.PHONY: nginx-stop
nginx-stop: ## Stop nginx reverse proxy
	nginx -c $(CURDIR)/configs/nginx.conf -s stop

.PHONY: ngrok
ngrok: ## Start ngrok tunnel pointing to nginx proxy
	ngrok start --config configs/ngrok.yml proxy

##@ Firebase

.PHONY: emulators
emulators: ## Start Firebase emulators (auth + functions)
	just emulators

##@ Docker

.PHONY: up
up: ## Start docker-compose services
	docker compose up -d

.PHONY: down
down: ## Stop docker-compose services
	docker compose down
