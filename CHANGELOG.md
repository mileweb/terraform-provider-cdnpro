# Changelog

## Unreleased

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
