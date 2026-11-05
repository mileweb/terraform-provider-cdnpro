# Changelog

## 1.0.0 (2026-11-05)

First release published to the Terraform Registry as `mileweb/cdnpro`
(NCAS-5719). The provider is functionally identical to 0.1.2, which was
published as `HuberyChen1/cdnpro` while the `mileweb` namespace was being set
up; only the provider address changes.

- **Provider source is now `mileweb/cdnpro`.** Configurations and states
  created with the interim `HuberyChen1/cdnpro` address are moved with

  ```bash
  terraform state replace-provider HuberyChen1/cdnpro mileweb/cdnpro
  terraform init -upgrade
  ```

  after updating `source` in the `required_providers` block.

## 0.1.2 (2026-11-05)

Published as `HuberyChen1/cdnpro`. Fix from QA of 0.1.1 (NCAS-5719, finding #5):

- **Switching the key algorithm replaces the certificate.** The API refuses a
  certificate version whose key family (RSA or EC) differs from the existing
  versions, and 0.1.1 found that out only at apply time: the plan showed an
  in-place update and the apply failed with `400 InvalidCertVersion`, after
  other resources in the same run may already have been changed. The provider
  now derives the family of the configured material (the `RSA2048` / `ECC256`
  keywords, the private key PEM, or the public key of the certificate PEM) and,
  when it differs from the `algorithm` of the existing versions, plans a
  replacement (`-/+`, with `algorithm` forcing it) and warns that a certificate
  in use needs `create_before_destroy`. Material the provider cannot parse keeps
  the in-place update and the verdict of the API. The mock API now enforces the
  rule as well.

## 0.1.1 (2026-11-02)

Published as `HuberyChen1/cdnpro`. Fixes from QA of 0.1.0 (NCAS-5719):

- **Import no longer rewrites write-only values.** After `terraform import` of
  a `cdnpro_certificate`, `cdnpro_secret` or `cdnpro_webhook`, the first apply
  used to upload a new certificate version, rotate the secret and overwrite the
  webhook credentials, because the SHA-256 hashes of the write-only attributes
  did not exist yet. Import now marks the resource as adopted: the configured
  values are taken to be the ones on the server, the first apply only records
  their hashes (the plan shows `last_update_time`, or the certificate's
  `latest_version`, as known after apply, together with a warning saying that
  nothing is sent), and later changes are detected as before.
- **`identification_info` survives import.** The API never returns the subject
  of a generated certificate, so after an import every plan showed it as an
  addition and re-issued the certificate. It is now recorded from the
  configuration without creating a version.
- **Purge and prefetch failures show the API's error list.** The API publishes
  `status = finished` and `successRate` a moment before it attaches `errors`;
  a poll landing in between reported the failure without details. The poller
  now re-reads such a task once after a short delay, and the diagnostic always
  names the task and the `GET /cdn/purges/{id}` / `GET /cdn/prefetches/{id}`
  endpoint to inspect it.

## 0.1.0 (2026-10-22)

Phase 3 (operational actions and data sources) of the CDN Pro Terraform provider (NCAS-5719):

- `cdnpro_purge` and `cdnpro_prefetch` as Terraform Actions (Terraform >= 1.14):
  invokable ad hoc or from `lifecycle.action_trigger`, polled to completion,
  failing on a partial `successRate`.
- Data sources `cdnpro_hostname`, `cdnpro_shields`, `cdnpro_isps` and
  `cdnpro_client_regions` with deterministic ordering.
- Provider `polling` block gains `purge_interval` / `purge_timeout`.
- Mock API endpoints and acceptance tests for all of the above.

Phase 2 (supporting configuration) of the CDN Pro Terraform provider (NCAS-5719):

- `cdnpro_secret`: write-only `secret` tracked by hash and rotated in place.
- `cdnpro_webhook`: write-only `credentials.secret_key`; removing the
  `credentials` block replaces the webhook because the API cannot clear it.
- `cdnpro_log_config`: `/cdn/report/logConfigs` with the API's integer ID
  carried as a string and server defaults mirrored in the schema.
- Mock API and acceptance coverage for all three.

Phase 1 (core) of the CDN Pro Terraform provider (NCAS-5719):

- Provider configuration with HMAC-SHA1 request signing, environment variable
  fallbacks and per-endpoint-group polling overrides.
- `cdnpro_property`: property + folded versions, in-place patch vs new version
  depending on the frozen flag, `Last-Modified` / `If-Unmodified-Since`.
- `cdnpro_certificate`: certificate + folded versions, write-only PEM material
  with AES-128-CBC private key encryption and hash-based change detection.
- `cdnpro_edge_hostname`: async provisioning polled on `history[0]`, 404 as the
  deletion success signal, opt-in `force_delete`.
- `cdnpro_deployment`: implicit validation before `deploy_property`, no-op
  adoption, drift detection per action, inverse `remove_*` on destroy,
  `OutdatedSecret` retry, `DuplicateDeployment` guidance.
- In-memory mock of the CDN Pro API and acceptance tests driven through the
  Terraform CLI.
