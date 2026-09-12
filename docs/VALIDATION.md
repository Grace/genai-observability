# Validation

What has actually been run against this tree, what still requires AWS, and where coverage is thin.
Run these before merging changes to the normalization, evaluation, replay, mapping, or
infrastructure paths.

## Required checks

```bash
go build ./... && go vet ./... && go test ./...
terraform -chdir=infra fmt -check -recursive
terraform -chdir=infra init -backend=false
terraform -chdir=infra validate
shellcheck scripts/*.sh && bash -n scripts/*.sh
cd web && npm ci && npm run build
```

## Last recorded run

2026-09-12, macOS, Go 1.23, Terraform 1.7, Node 22. Every check exits zero.

| Check | Result |
| --- | --- |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test ./...` | pass (5 packages with tests) |
| `terraform fmt -check -recursive` | pass |
| `terraform init -backend=false` | pass |
| `terraform validate` | pass |
| `shellcheck scripts/*.sh` | pass |
| `bash -n scripts/*.sh` | pass |
| `npm ci && npm run build` | pass (`tsc -b` clean) |

All seven source fixtures normalize through the offline CLI:

```bash
for f in otel openllmetry braintrust bedrock pydanticai eino genkit; do
  go run ./cmd/normalize -file "examples/normalization/$f.json"
done
```

## Test coverage — honest accounting

`go test ./...` passing is a narrower signal than it looks. Tests live in five packages:

| Package | Covers |
| --- | --- |
| `internal/normalize` | alias normalization, conservative Braintrust score handling (unknown scalar semantics are not promoted to probability), token-accounting mismatch visibility |
| `internal/pricing` | versioned catalog lookup and cost calculation |
| `internal/replay` | baseline/candidate quality, cost, and latency deltas |
| `internal/telemetry` | W3C trace-context inject/extract round trip |
| `internal/mapping` | deterministic mapping policy decisions |

No tests yet: `internal/adapters`, `internal/canonical`, `internal/model`, `internal/lambdatel`,
`cmd/app`, `cmd/normalize`, and all eight `lambda/` handlers. The adapter package is the most
significant gap — seven source formats are exercised only through the CLI above and through
`internal/normalize`, not directly.

## Requires AWS credentials — not executed

No `terraform plan` or `apply` has ever been run against a real account. Terraform is validated
**as configuration**; that is not the same as proven in deployment. Treat the AWS architecture
accordingly.

- `terraform -chdir=infra plan` / `apply` — needs credentials, a region, and Bedrock model access.
  `infra/lambdas.tf` reads Lambda zips from `build/`, so `scripts/build-lambdas.sh` runs first.
- `scripts/deploy.sh` — full stack deployment.
- `scripts/smoke-test.sh`, `scripts/normalization-smoke-test.sh` — need a deployed ALB endpoint.
  They exercise the production-request → EventBridge → Step Functions → evaluator → persistence path.
- `scripts/replay.sh` — needs a populated corpus bucket and the replay state machine.
- `scripts/mapping-analysis.sh` — needs the semantic-mapping state machine.
- Live Bedrock inference and the Honeycomb export path.

## Runnable without an AWS account

The normalization core runs offline against the shipped fixtures:

```bash
go run ./cmd/normalize -file examples/normalization/braintrust.json
```

The web inspector posts to a running `/normalize` endpoint (`VITE_NORMALIZE_API`, default
`http://localhost:8080`), so it needs the Go app running locally but no AWS.

## CI

`.github/workflows/ci.yml` runs the Go build/vet/test set with a `go.mod` drift check, Terraform
formatting/init/validation, shell syntax plus shellcheck, and the React/TypeScript build on every
push and pull request.
