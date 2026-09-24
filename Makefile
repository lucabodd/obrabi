SHELL := /bin/bash
GO ?= go
COMPOSE ?= docker compose
SERVICES := gateway auth projects stats feedback
WHO ?= nando
USER_ID ?= 1

.DEFAULT_GOAL := help
.PHONY: help build test test-go test-web lint fmt env up down logs ps password users reset-password seed-demo backup dev dev-seed

help: ## Mostra questo aiuto
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "} {printf "  \033[1m%-16s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- sviluppo

build: ## Compila i cinque servizi in ./bin
	@mkdir -p bin
	@for s in $(SERVICES); do $(GO) build -o bin/$$s ./cmd/$$s || exit 1; done

test: test-go test-web ## Esegue tutti i test

test-go: ## Test Go (integrazione con OBRABI_TEST_DATABASE_URL)
	$(GO) test ./...

test-web: ## Test del frontend (node --test)
	node --test web/tests/*.test.mjs

lint: ## go vet + controllo gofmt
	$(GO) vet ./...
	@test -z "$$(gofmt -l cmd internal web)" || (gofmt -l cmd internal web; exit 1)

fmt: ## Formatta il codice Go
	gofmt -w cmd internal web

dev: ## Servizi in locale, PostgreSQL in Docker
	./scripts/dev.sh

dev-seed: build ## Dati demo con `make dev` attivo: make dev-seed USER_ID=1
	@set -a; source .env; set +a; \
	DATABASE_URL="postgres://obrabi_projects:$$PROJECTS_DB_PASSWORD@127.0.0.1:$${OBRABI_DEV_DB_PORT:-5432}/obrabi?sslmode=disable" \
	OBRABI_LOG_FORMAT=text bin/projects seed-demo $(USER_ID)

# ---------------------------------------------------------------- deploy

env: ## Crea .env con segreti casuali
	./scripts/gen-env.sh

up: ## Compila e avvia lo stack
	$(COMPOSE) up -d --build

down: ## Ferma lo stack (i dati restano nel volume)
	$(COMPOSE) down

logs: ## Log di tutti i servizi
	$(COMPOSE) logs -f --tail=100

ps: ## Stato dei servizi
	$(COMPOSE) ps

password: ## Mostra la password iniziale stampata al primo avvio
	@$(COMPOSE) logs auth 2>/dev/null | grep -A4 "Usuari creat" | sed 's/^[^|]*| //'

users: ## Elenco utenti
	$(COMPOSE) exec auth /app/auth user list

reset-password: ## Nuova password casuale: make reset-password WHO=nando
	$(COMPOSE) exec auth /app/auth user reset-password $(WHO)

seed-demo: ## Dati di esempio in un account vuoto: make seed-demo USER_ID=1
	$(COMPOSE) exec projects /app/projects seed-demo $(USER_ID)

backup: ## Backup immediato in ./backups
	$(COMPOSE) exec backup sh -c 'f=/backups/obrabi-$$(date +%Y%m%d-%H%M%S)-manual.dump && pg_dump --format=custom --file=$$f && echo $$f'
