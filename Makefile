.PHONY: build test vet lint run dev-telemetry stop-telemetry logs-telemetry help

# ─── Build ───────────────────────────────────────────────────────────────────

build: ## Build the gateway binary
	go build -o bin/gateway ./cmd/gateway

# ─── Test ────────────────────────────────────────────────────────────────────

test: ## Run all tests with race detector
	go test ./... -race

vet: ## Run go vet
	go vet ./...

# ─── Run ─────────────────────────────────────────────────────────────────────

run: ## Run the gateway with config.yaml
	./bin/gateway -config config.yaml

# ─── Telemetry ───────────────────────────────────────────────────────────────

dev-telemetry: ## Start Aspire Dashboard for local OTel development
	docker compose -f docker-compose.aspire.yml up -d
	@echo ""
	@echo "  Aspire Dashboard: http://localhost:18888"
	@echo "  OTLP HTTP endpoint: http://localhost:4318"
	@echo ""
	@echo "  Enable in config.yaml:"
	@echo "    telemetry:"
	@echo "      enabled: true"
	@echo ""

stop-telemetry: ## Stop Aspire Dashboard
	docker compose -f docker-compose.aspire.yml down

logs-telemetry: ## Follow Aspire Dashboard logs
	docker compose -f docker-compose.aspire.yml logs -f

# ─── Help ────────────────────────────────────────────────────────────────────

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
