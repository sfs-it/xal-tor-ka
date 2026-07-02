# Xal-Tor-Ka — operativita locale (go run) + stack Docker (compose).

BIN := xaltorka
COMPOSE := docker compose
# Single source of truth: version/version.go. Derived here for `make version`.
VERSION := $(shell sed -n 's/.*Version = "\(.*\)".*/\1/p' version/version.go)

.PHONY: help bootstrap run build fmt vet tidy test clean \
        up down logs rebuild ps setup admin version

help: ## Elenca i target disponibili
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

bootstrap: ## Crea secrets.json/users.json dai .example se mancanti
	@test -f secrets.json || cp secrets.example.json secrets.json
	@test -f users.json   || cp users.example.json users.json

run: bootstrap ## Avvia il server in locale (go run)
	go run . -config .

build: ## Compila il binario statico (immagine minima) — versione da version/version.go
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN) .

version: ## Mostra la versione corrente
	@echo $(VERSION)

fmt: ## Formatta il codice (gofmt)
	gofmt -w .

vet: ## Analisi statica (go vet)
	go vet ./...

tidy: ## Allinea go.mod/go.sum
	go mod tidy

test: ## Esegue i test con il race detector
	go test -race ./...

clean: ## Rimuove binario e dati runtime
	rm -f $(BIN)
	rm -rf data

# --- Docker (compose) -------------------------------------------------------
up: bootstrap ## Avvia lo stack in background (build se serve)
	$(COMPOSE) up -d --build

down: ## Ferma e rimuove i container
	$(COMPOSE) down

logs: ## Segue i log dello stack
	$(COMPOSE) logs -f

rebuild: ## Ricostruisce le immagini e riavvia
	$(COMPOSE) up -d --build --force-recreate

ps: ## Stato dei servizi
	$(COMPOSE) ps

setup: ## Crea il profilo admin di setup nel container (EMAIL=...)
	$(COMPOSE) run --rm xaltorka setup --email "$(EMAIL)" --config /etc/xaltorka

admin: ## Crea/promuove un utente admin (EMAIL=... PASSWORD=...) e riavvia
	$(COMPOSE) run --rm xaltorka user --email "$(EMAIL)" --password "$(PASSWORD)" --admin --config /etc/xaltorka
	$(COMPOSE) restart xaltorka
