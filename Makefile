.PHONY: fmt test test-core validate build-lambdas web deploy smoke smoke-normalizer replay destroy

fmt:
	gofmt -w ./cmd ./internal ./lambda
	terraform -chdir=infra fmt -recursive

test:
	go test ./...
	go vet ./...

test-core:
	go test ./internal/normalize ./internal/adapters ./internal/canonical ./internal/mapping ./internal/pricing ./internal/replay

validate:
	go test ./...
	go vet ./...
	terraform -chdir=infra fmt -check -recursive
	terraform -chdir=infra init -backend=false
	terraform -chdir=infra validate
	bash -n scripts/*.sh
	cd web && npm install && npm run build

build-lambdas:
	./scripts/build-lambdas.sh

web:
	cd web && npm install && npm run dev

deploy:
	./scripts/deploy.sh

smoke:
	./scripts/smoke-test.sh

smoke-normalizer:
	./scripts/normalization-smoke-test.sh

replay:
	./scripts/replay.sh

destroy:
	terraform -chdir=infra destroy
