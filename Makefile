# MediCart — developer entrypoints.
#
# Quick start:   make setup && make dev
# Everything else is discoverable with `make help`.

SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT := $(shell cd $(dir $(lastword $(MAKEFILE_LIST))) && pwd)
API  := $(ROOT)/services/api
WEB  := $(ROOT)/apps/web

# This machine's system Go is 1.19 which is too old; prefer the pinned 1.24
# toolchain installed under ~/.local/toolchains. Falls back to PATH `go`.
LOCAL_GO := $(HOME)/.local/toolchains/go1.24.8/bin/go
GO       := $(shell test -x $(LOCAL_GO) && echo $(LOCAL_GO) || echo go)

# A GOROOT exported by the old system Go would make the newer toolchain read
# the wrong defaults (symptom: empty GOPROXY). Let each toolchain find its own.
unexport GOROOT

# Load .env for targets that need DATABASE_URL etc.
ifneq (,$(wildcard $(ROOT)/.env))
include $(ROOT)/.env
export
endif

.PHONY: help
help: ## Show this help
	@echo "MediCart targets:"
	@grep -hE '^[a-zA-Z_0-9-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Go toolchain in use: $(GO)"

# ---------------------------------------------------------------- setup

.PHONY: setup
setup: env-file db-up deps migrate seed ## One-time setup: .env, DB, deps, migrations, seed data
	@echo ""
	@echo "✅ Setup complete. Next: make dev"

.PHONY: env-file
env-file: ## Create .env from .env.example if absent
	@if [ ! -f $(ROOT)/.env ]; then \
		cp $(ROOT)/.env.example $(ROOT)/.env; \
		echo "created .env from .env.example"; \
	else \
		echo ".env already exists — left untouched"; \
	fi

.PHONY: deps
deps: deps-api deps-web ## Install backend and frontend dependencies

.PHONY: deps-api
deps-api:
	cd $(API) && $(GO) mod download && $(GO) mod tidy

.PHONY: deps-web
deps-web:
	cd $(WEB) && npm install

# ---------------------------------------------------------------- database

.PHONY: db-up
db-up: ## Start PostgreSQL (Docker) and wait until it is ready
	docker compose up -d db
	@echo -n "waiting for postgres"
	@for i in $$(seq 1 40); do \
		if docker compose exec -T db pg_isready -U medicart -d medicart >/dev/null 2>&1; then \
			echo " ready"; exit 0; fi; \
		echo -n "."; sleep 1; \
	done; echo " TIMEOUT"; exit 1

.PHONY: db-down
db-down: ## Stop containers (keeps data)
	docker compose down

.PHONY: db-reset
db-reset: ## DESTRUCTIVE: delete the database volume, recreate, migrate, seed
	@read -p "This deletes all local MediCart data. Type 'yes' to continue: " c; \
	 [ "$$c" = "yes" ] || { echo "aborted"; exit 1; }
	docker compose down -v
	$(MAKE) db-up migrate seed

.PHONY: psql
psql: ## Open a psql shell on the dev database
	docker compose exec -it db psql -U medicart -d medicart

.PHONY: migrate
migrate: ## Apply all pending migrations
	cd $(API) && $(GO) run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	cd $(API) && $(GO) run ./cmd/migrate down 1

.PHONY: migrate-status
migrate-status: ## Show current schema version
	cd $(API) && $(GO) run ./cmd/migrate status

.PHONY: seed
seed: ## Load clearly-labelled demonstration catalogue data (idempotent)
	cd $(API) && $(GO) run ./cmd/seed

.PHONY: seed-admin
seed-admin: ## Create the first admin user from BOOTSTRAP_ADMIN_* in .env
	cd $(API) && $(GO) run ./cmd/seed -admin-only

# ---------------------------------------------------------------- dev

.PHONY: dev
dev: db-up ## Run API + web dev servers together (Ctrl-C stops both)
	@echo "API  → http://localhost:$${API_PORT:-8080}"
	@echo "WEB  → http://localhost:5173"
	@trap 'kill 0' EXIT INT TERM; \
	( cd $(API) && $(GO) run ./cmd/api 2>&1 | sed 's/^/[api] /' ) & \
	( cd $(WEB) && npm run dev 2>&1 | sed 's/^/[web] /' ) & \
	wait

.PHONY: dev-api
dev-api: ## Run only the Go API
	cd $(API) && $(GO) run ./cmd/api

.PHONY: dev-web
dev-web: ## Run only the Vite dev server
	cd $(WEB) && npm run dev

# ---------------------------------------------------------------- quality

.PHONY: fmt
fmt: ## Format Go and web sources
	cd $(API) && $(GO) fmt ./...
	cd $(WEB) && npm run format

.PHONY: fmt-check
fmt-check: ## Fail if Go sources are not gofmt-clean
	@cd $(API) && out=$$($(GO) fmt -l . ./cmd/... ./internal/...); \
	 if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi; \
	 echo "gofmt clean"

.PHONY: vet
vet: ## go vet
	cd $(API) && $(GO) vet ./...

.PHONY: lint-web
lint-web: ## ESLint
	cd $(WEB) && npm run lint

.PHONY: typecheck
typecheck: ## TypeScript type check
	cd $(WEB) && npm run typecheck

.PHONY: test
test: test-api test-web ## Run backend + frontend unit tests

.PHONY: test-api
test-api: db-up ## Go tests (integration tests use a throwaway schema)
	cd $(API) && $(GO) test ./... -count=1

.PHONY: test-api-cover
test-api-cover: db-up ## Go tests with a coverage summary
	cd $(API) && $(GO) test ./... -count=1 -coverprofile=coverage.out && \
	  $(GO) tool cover -func=coverage.out | tail -1

.PHONY: test-web
test-web: ## Vitest unit/component tests
	cd $(WEB) && npm run test -- --run

.PHONY: test-e2e
test-e2e: ## Playwright end-to-end suite (expects `make dev` running)
	cd $(WEB) && npx playwright test

.PHONY: test-e2e-install
test-e2e-install: ## Download Playwright browsers (one-off, ~300MB)
	cd $(WEB) && npx playwright install chromium

.PHONY: ci
ci: fmt-check vet test-api lint-web typecheck test-web build ## Everything CI runs

# ---------------------------------------------------------------- build

.PHONY: build
build: build-api build-web ## Production builds of both apps

.PHONY: build-api
build-api:
	cd $(API) && CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o bin/api ./cmd/api
	@ls -lh $(API)/bin/api

.PHONY: build-web
build-web:
	cd $(WEB) && npm run build

.PHONY: docker-build
docker-build: ## Build the API container image
	docker build -t medicart-api:local $(API)

.PHONY: clean
clean: ## Remove build artefacts (not data)
	rm -rf $(API)/bin $(WEB)/dist $(API)/coverage.out
