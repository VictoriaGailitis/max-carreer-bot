.PHONY: bootstrap up up-frontend down test smoke smoke-compose check-client

bootstrap:
	bash scripts/dev/bootstrap-local.sh

up:
	docker compose up --build -d

up-frontend:
	docker compose -f compose.yaml -f deploy/compose.frontend.yaml up --build -d

down:
	docker compose down

test:
	docker build --target test -t max-carreer-bot-test .

smoke:
	bash scripts/checks/smoke-api.sh

smoke-compose:
	bash scripts/checks/smoke-compose.sh

check-client:
	tsc --project api/client/tsconfig.json
