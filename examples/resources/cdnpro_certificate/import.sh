# Import by certificate ID. Write-only material cannot be read back, so the
# configured private_key / certificate / chain_cert (and identification_info)
# are adopted as the current version: the first apply only records their hashes
# and uploads nothing.
terraform import cdnpro_certificate.prod 5f1e2d3c4b5a69788796a5b4
