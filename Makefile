.PHONY: fmt test test-normalizer build-lambdas deploy smoke smoke-normalizer replay destroy
fmt:
	gofmt -w ./cmd ./internal ./lambda

test:
	go test ./...

test-normalizer:
	go test ./internal/normalize ./internal/adapters ./internal/canonical

build-lambdas:
	./scripts/build-lambdas.sh

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
