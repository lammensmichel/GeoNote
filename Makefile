.PHONY: help dev dev-down build test lint migrate migrate-status migrate-baseline seed

help: ## Afficher l'aide
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---- Docker ----

dev: ## Lancer l'environnement de dev (DB + API)
	cd docker && docker compose up --build -d

dev-down: ## Arreter l'environnement de dev
	cd docker && docker compose down

dev-logs: ## Voir les logs
	cd docker && docker compose logs -f api

dev-reset: ## Reset complet (supprime les donnees)
	cd docker && docker compose down -v && docker compose up --build -d

# ---- Backend Go ----

build: ## Compiler le backend
	cd backend && go build -o bin/geonote ./cmd/server

run: ## Lancer le backend en local
	cd backend && go run ./cmd/server

test: ## Lancer les tests backend
	cd backend && go test -v -race ./...

test-cover: ## Tests avec couverture
	cd backend && go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

lint: ## Linter le backend
	cd backend && go vet ./...

# ---- Mobile Flutter ----

mobile-get: ## Installer les dependances Flutter
	cd mobile && flutter pub get

mobile-run: ## Lancer l'app mobile
	cd mobile && flutter run

mobile-test: ## Tests Flutter
	cd mobile && flutter test

mobile-analyze: ## Analyser le code Flutter
	cd mobile && flutter analyze

# ---- Database ----

# Base locale Docker par defaut (memes DB_PORT / DB_PASSWORD que docker-compose).
# En production : DATABASE_URL='<URL directe Neon>' make migrate (confirmation demandee).
LOCAL_DATABASE_URL ?= postgres://geonote:$${DB_PASSWORD:-geonote}@localhost:$${DB_PORT:-5432}/geonote?sslmode=disable

migrate: ## Appliquer les migrations manquantes (DATABASE_URL, sinon base Docker locale)
	DATABASE_URL="$${DATABASE_URL:-$(LOCAL_DATABASE_URL)}" ./scripts/migrate.sh

migrate-status: ## Voir les migrations appliquees et a faire
	DATABASE_URL="$${DATABASE_URL:-$(LOCAL_DATABASE_URL)}" ./scripts/migrate.sh --status

migrate-baseline: ## Base deja creee : marquer jusqu'a UPTO (ex. make migrate-baseline UPTO=007)
	@test -n "$(UPTO)" || { echo "Indiquez la derniere migration presente : make migrate-baseline UPTO=007"; exit 2; }
	DATABASE_URL="$${DATABASE_URL:-$(LOCAL_DATABASE_URL)}" ./scripts/migrate.sh --baseline=$(UPTO)

seed: ## Charger les donnees de test (base locale uniquement)
	DATABASE_URL="$${DATABASE_URL:-$(LOCAL_DATABASE_URL)}" ./scripts/migrate.sh --seed

psql: ## Ouvrir un shell PostgreSQL
	cd docker && docker compose exec db psql -U geonote -d geonote
