.PHONY: run build test docker-build up down logs

run:
	OLLAMA_URL=http://localhost:11434 go run .

build:
	go build -o agentshield .

test:
	go test ./... -race -count=1

docker-build:
	docker compose build

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f agentshield
