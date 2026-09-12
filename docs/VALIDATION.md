# Validation status

## Completed in the artifact-generation environment

- `gofmt` over all Go source;
- focused Go tests for the dependency-free normalization packages:
  - OTel alias normalization;
  - conservative Braintrust score handling (unknown scalar semantics are not promoted to probability);
  - token-accounting mismatch visibility;
- shell scripts checked with `bash -n`;
- JSON examples parsed successfully;
- Graphviz architecture PNG/SVG regenerated from source;
- repository checked for the old `Interlingua` project/module name;
- generated package excludes Terraform state, `terraform.tfvars`, `.terraform`, build artifacts, secrets, and `.git` metadata.

## Not executable in this environment

`go test ./...` cannot complete here because outbound DNS/network access to `proxy.golang.org` is blocked, so AWS/OpenTelemetry dependencies cannot be downloaded and a `go.sum` cannot be generated in this runtime. The dependency-free normalization tests do pass.

Terraform CLI is not installed in this runtime, so `terraform validate` could not be executed here.

A live AWS deployment cannot be performed without your AWS account credentials, selected Bedrock model IDs/inference profiles, and Honeycomb secret.

## Run immediately after cloning

```bash
go mod tidy
go test ./...
terraform -chdir=infra fmt -recursive
terraform -chdir=infra init -backend=false
terraform -chdir=infra validate
```

The included GitHub Actions workflow performs these checks on push/PR in a normal networked CI environment.
