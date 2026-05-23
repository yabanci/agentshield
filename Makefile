.PHONY: run build test test-race coverage smoke vuln lint docker-build up down logs

run:
	OLLAMA_URL=http://localhost:11434 go run .

build:
	go build -o agentshield .

test:
	go test ./... -race -count=1

# True end-to-end coverage. Default `go test -cover ./...` only counts coverage
# inside each test's own package, so the orchestrator package (tested via
# agent/ integration tests) under-reports as ~10%. -coverpkg=./... attributes
# coverage to the package the code lives in, giving the honest number.
coverage:
	go test -coverpkg=./... -coverprofile=coverage.out ./... >/dev/null
	@echo "---"
	@go tool cover -func=coverage.out | tail -1
	@echo "Per-package breakdown: go tool cover -func=coverage.out"
	@echo "HTML report:          go tool cover -html=coverage.out"

lint:
	golangci-lint run --timeout 5m ./...

vuln:
	govulncheck ./...

# Live HTTP smoke against a locally running agentshield on PORT=8080.
# Verifies the demo-critical paths: health, dashboard, status, demo/compare,
# demo/kill+restore, demo/degrade+restore-quality, /react. Assumes auth is
# OFF (no AGENTSHIELD_AUTH_TOKEN set) — for auth-on smoke set AUTH_TOKEN env.
smoke:
	@BASE=$${BASE:-http://localhost:8080}; \
	AUTH_HEADER=$${AUTH_TOKEN:+-H "Authorization: Bearer $$AUTH_TOKEN"}; \
	set -e; \
	echo "→ $$BASE/health/live"; curl -fsS "$$BASE/health/live" | head -c 80; echo; \
	echo "→ $$BASE/  (dashboard)"; curl -fsSo /dev/null -w "  HTTP %{http_code}\n" "$$BASE/"; \
	echo "→ $$BASE/status";       curl -fsS "$$BASE/status" | head -c 120; echo; \
	echo "→ POST $$BASE/demo/compare"; curl -fsS -X POST "$$BASE/demo/compare" -H 'Content-Type: application/json' -d '{"prompt":"hi"}' | head -c 200; echo; \
	echo "→ POST $$BASE/demo/degrade";        curl -fsS -X POST $$AUTH_HEADER "$$BASE/demo/degrade"; echo; \
	echo "→ POST $$BASE/demo/restore-quality"; curl -fsS -X POST $$AUTH_HEADER "$$BASE/demo/restore-quality"; echo; \
	echo "→ smoke OK"

docker-build:
	docker compose build

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f agentshield
