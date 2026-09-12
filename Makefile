.PHONY: fmt test test-core validate diagram diagram-check build-lambdas web deploy smoke smoke-normalizer replay destroy

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
	./scripts/check-diagram.sh
	cd web && npm install && npm run build

# The committed PNG/SVG are rendered artifacts. Regenerate them whenever
# docs/full-architecture.dot changes; diagram-check fails if you forget.
diagram:
	dot -Tsvg docs/full-architecture.dot -o docs/full-architecture.svg
	dot -Tpng -Gdpi=150 docs/full-architecture.dot -o docs/full-architecture.png

diagram-check:
	./scripts/check-diagram.sh

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
