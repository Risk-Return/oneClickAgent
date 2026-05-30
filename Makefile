# Top-level convenience targets for development.
.PHONY: all gateway device agent web

gateway:
	cd gateway && go build -o bin/gateway ./cmd/gateway

device:
	cd device && pip install -e .

agent:
	cd agent && docker build -t iagent/agent:dev .

web:
	cd web && npm install && npm run build

dev-up:
	docker compose -f deploy/cloud/docker-compose.yml up -d
