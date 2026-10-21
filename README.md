# Terraform Provider for CDN Pro

`terraform-provider-cdnpro` lets customers manage CDN Pro configuration as code
through the CDN Pro API: properties, certificates, edge hostnames and the
deployments that put them live.

Design reference: Confluence PRD *Terraform Provider for CDN Pro API* (Jira
NCAS-5719). This repository implements **Phase 1 (core)** of that PRD.

| Resource | Wraps | Status |
| --- | --- | --- |
| `cdnpro_property` | `/cdn/properties` + versions (folded) | Phase 1, done |
| `cdnpro_certificate` | `/cdn/certificates` + versions (folded) | Phase 1, done |
| `cdnpro_edge_hostname` | `/cdn/edgeHostnames` | Phase 1, done |
| `cdnpro_deployment` | `/cdn/deploymentTasks` + implicit `/cdn/validations` | Phase 1, done |
| `cdnpro_secret`, `cdnpro_webhook`, `cdnpro_log_config` | plain CRUD | Phase 2, not started |
| `cdnpro_purge`, `cdnpro_prefetch` (actions), data sources | | Phase 3, not started |

## Requirements

- Terraform >= 1.11 (write-only attributes are used for certificate material).
- Go >= 1.24 to build.

## Usage

```hcl
terraform {
  required_providers {
    cdnpro = {
      source = "mileweb/cdnpro"
    }
  }
}

provider "cdnpro" {
  server   = "https://ngapi.cdnetworks.com" # or CDNPRO_SERVER
  api_user = var.api_user                   # or CDNPRO_API_USER
  api_key  = var.api_key                    # or CDNPRO_API_KEY

  # Optional: tune the fixed-interval polling of async operations.
  polling {
    deployment_interval = "20s"
    deployment_timeout  = "10m"
  }
}
```

See [`examples/`](examples) for a complete property + certificate + edge
hostname + staged deployment configuration, and [`docs/`](docs) for the
attribute reference.

### How the API is hidden

- **Versions are internal.** Changing any configuration attribute of a
  `cdnpro_property` or `cdnpro_certificate` is an in-place update from
  Terraform's point of view. Internally the provider patches the latest
  property version when it has never been deployed, and otherwise creates a new
  version. Certificates always get a new version. Creating a version never
  deploys it.
- **Deployments validate first.** `cdnpro_deployment` runs the property
  validation the API requires before `deploy_property`, retries once on
  `OutdatedSecret`, and gives up with a clear message on
  `DuplicateDeployment`.
- **Adoption without import.** A `cdnpro_deployment` whose actions already match
  the live `versionInProduction` / `versionInStaging` makes no API call on
  create.
- **Optimistic concurrency.** The provider caches `Last-Modified` from every
  read and sends `If-Unmodified-Since` on every PATCH/PUT. A 412 surfaces as
  "Resource modified outside Terraform".
- **Write-only material.** `private_key`, `certificate` and `chain_cert` are
  never stored in state; SHA-256 hashes in private state drive change
  detection. The private key is AES-128-CBC encrypted with key material derived
  from the request `Date` header, exactly as the API requires.
- **Edge hostnames are polled** on `history[0]`; a successful deletion is
  observed as a `404`.

### Auth

Every request is signed as HTTP Basic with the password
`base64(HMAC-SHA1(Date, api_key))`. The key is never logged. `internal/client`
has unit tests pinned to OpenSSL-generated vectors.

## Development

```bash
make build        # go build
make test         # unit tests
make testacc      # acceptance tests against the in-memory mock API (needs the terraform CLI)
make docs         # regenerate docs/ with tfplugindocs
make lint         # gofmt + go vet
```

The acceptance tests spin up an in-memory fake of the CDN Pro API
(`internal/provider/mockapi_test.go`) and drive the provider through the real
Terraform CLI with `terraform-plugin-testing`. They skip when no `terraform`
binary is found; point `TF_ACC_TERRAFORM_PATH` at one to run them.

If `proxy.golang.org` is unreachable:

```bash
export GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn
```

### Local override for manual testing

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "mileweb/cdnpro" = "/path/to/terraform-provider-cdnpro"   # directory containing the built binary
  }
  direct {}
}
```

### Layout

```
main.go                       provider entry point
internal/client/              API client: auth, encryption, polling tables, per-resource calls
internal/provider/            provider configuration and the four resources
internal/provider/*_test.go   mock API + acceptance tests
examples/                     HCL examples (also feed tfplugindocs)
docs/                         generated registry docs
```

## Known deviations from the PRD (confirmed against the API source)

- Certificates **do** support `Last-Modified` / `If-Unmodified-Since`; the
  provider uses them.
- `name` is required by the API on deployment tasks and validations; the
  provider generates `terraform-<target>-<timestamp>` when `name` is not set.
- `DuplicateDeployment` is HTTP 409, not 400. Handling is unchanged.
- `auto_renew` is returned by the API as `Off` (capitalised); the provider
  accepts either case and does not report the difference as drift.
- `peer_failure_timeout` cannot be "object or the string `off`" in HCL; it is an
  object with a `disabled` flag that sends the literal `"off"`.
- `redirect_http_to_https` is a string attribute (`true`, `false`, `302`,
  `307`, `308`); HCL booleans convert automatically.
