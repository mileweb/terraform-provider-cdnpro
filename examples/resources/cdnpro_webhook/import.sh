# The webhook ID is only returned when it is created (Location header); it is not
# in the GET response, so take it from wherever it was recorded (e.g. the portal).
# credentials.secret_key is write-only, so the configured one is adopted as the
# current key: the first apply only records its hash and sends nothing.
terraform import cdnpro_webhook.deploy_notify 6716f3a2b1c0d9e8f7a6b5c4
