# llmem - AI Memory Service
# Build and management commands

BINARY := llmem
PID_FILE := /tmp/llmem.pid
PORT := 9980
VERSION_FILE := VERSION

.PHONY: all build test test-race clean run stop restart logs help version tag bump-patch bump-minor bump-major

all: build

## Build

build: ## Build the binary
	go build -o $(BINARY) .

build-race: ## Build with race detector
	go build -race -o $(BINARY) .

## Test

test: ## Run all tests
	go test ./...

test-v: ## Run tests with verbose output
	go test ./... -v

test-race: ## Run tests with race detector
	go test -race ./...

test-cover: ## Run tests with coverage
	go test ./... -cover -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Run

run: build ## Build and run in foreground
	./$(BINARY)

run-bg: build stop ## Build and run in background
	@echo "Starting $(BINARY) on port $(PORT)..."
	@nohup ./$(BINARY) > /tmp/llmem.log 2>&1 & echo $$! > $(PID_FILE)
	@sleep 1
	@if kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		echo "Started with PID $$(cat $(PID_FILE))"; \
	else \
		echo "Failed to start. Check /tmp/llmem.log"; \
		exit 1; \
	fi

stop: ## Stop the background service
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "Stopping PID $$PID..."; \
			kill $$PID; \
			sleep 1; \
			if kill -0 $$PID 2>/dev/null; then \
				kill -9 $$PID; \
			fi; \
		fi; \
		rm -f $(PID_FILE); \
	fi

restart: stop run-bg ## Rebuild and restart the service
	@echo "Service restarted"

## Utility

logs: ## Tail the service logs
	@tail -f /tmp/llmem.log

status: ## Check if service is running
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		echo "Running (PID $$(cat $(PID_FILE)))"; \
		curl -s http://localhost:$(PORT)/v1/health | head -1; \
	else \
		echo "Not running"; \
	fi

health: ## Quick health check
	@curl -s http://localhost:$(PORT)/v1/health | jq . 2>/dev/null || echo "Service not responding"

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out coverage.html
	rm -f /tmp/llmem.log $(PID_FILE)

## Dev

fmt: ## Format code
	go fmt ./...

lint: ## Run linter (requires golangci-lint)
	golangci-lint run

tidy: ## Tidy go modules
	go mod tidy

## Version (semver)

version: ## Print current version from VERSION file
	@cat $(VERSION_FILE) | tr -d ' \n'

tag: ## Create git tag from VERSION (e.g. v1.3.0) and push
	@v=$$(cat $(VERSION_FILE) | tr -d ' \n'); \
	if [ -z "$$v" ]; then echo "VERSION file empty"; exit 1; fi; \
	git tag -a "v$$v" -m "Release v$$v"; \
	git push origin "v$$v" 2>/dev/null || echo "Tag v$$v created (push manually if needed)"

bump-patch: ## Bump PATCH in VERSION (x.y.Z)
	@awk -F. '{$$3++; if ($$3>=100) {$$3=0; $$2++}; if ($$2>=100) {$$2=0; $$1++}; printf "%d.%d.%d\n", $$1, $$2, $$3}' $(VERSION_FILE) > $(VERSION_FILE).tmp && mv $(VERSION_FILE).tmp $(VERSION_FILE) && cat $(VERSION_FILE)

bump-minor: ## Bump MINOR in VERSION (x.Y.0)
	@awk -F. '{$$2++; $$3=0; if ($$2>=100) {$$2=0; $$1++}; printf "%d.%d.%d\n", $$1, $$2, $$3}' $(VERSION_FILE) > $(VERSION_FILE).tmp && mv $(VERSION_FILE).tmp $(VERSION_FILE) && cat $(VERSION_FILE)

bump-major: ## Bump MAJOR in VERSION (X.0.0)
	@awk -F. '{$$1++; $$2=0; $$3=0; printf "%d.%d.%d\n", $$1, $$2, $$3}' $(VERSION_FILE) > $(VERSION_FILE).tmp && mv $(VERSION_FILE).tmp $(VERSION_FILE) && cat $(VERSION_FILE)

## Help

help: ## Show this help
	@echo "llmem - AI Memory Service"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
