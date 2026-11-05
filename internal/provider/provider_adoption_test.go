package provider

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Import adoption (NCAS-5719 QA findings #2 and #3): after terraform import
// the write-only values have no stored hashes. The first plan must not treat
// the configured values as changed (which uploaded a new certificate version,
// rotated the secret and rewrote the webhook credentials in v0.1.0), while a
// genuine change made after the import must still be detected.
//
// Each test creates the resource, forgets it with a `removed` block (the
// equivalent of `terraform state rm`), imports it back into the same address
// and then plans against the unchanged configuration.

// forgetHCL drops a resource from state without destroying it.
func forgetHCL(address string) string {
	return fmt.Sprintf(`
removed {
  from = %s
  lifecycle {
    destroy = false
  }
}
`, address)
}

// rememberID captures the resource's ID for the later import step.
func rememberID(address string, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not in state", address)
		}
		*into = rs.Primary.ID
		return nil
	}
}

func TestAccSecretImportAdoption(t *testing.T) {
	m := testAccSetup(t)
	var id string
	var lastUpdate time.Time
	stored := func() (string, time.Time) {
		m.mu.Lock()
		defer m.mu.Unlock()
		sec := m.secrets[id]
		return sec.secret, sec.lastUpdate
	}
	config := providerHCL(m) + secretHCL("origin-auth", "shared token", "s3cr3t-value-1")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_secret.auth", &id),
					func(*terraform.State) error { _, lastUpdate = stored(); return nil },
				),
			},
			{
				Config: providerHCL(m) + forgetHCL("cdnpro_secret.auth"),
			},
			{
				Config:             config,
				ResourceName:       "cdnpro_secret.auth",
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return id, nil },
				ImportStatePersist: true,
			},
			{
				// Same configuration after import: the plan only carries the
				// recording apply (last_update_time unknown) and no PATCH is sent.
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cdnpro_secret.auth", plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue("cdnpro_secret.auth", tfjsonpath.New("last_update_time")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cdnpro_secret.auth", "name", "origin-auth"),
					resource.TestCheckNoResourceAttr("cdnpro_secret.auth", "secret"),
					func(*terraform.State) error {
						secret, at := stored()
						if secret != "s3cr3t-value-1" || !at.Equal(lastUpdate) {
							return fmt.Errorf("secret written after import: value=%q lastUpdate %s -> %s", secret, lastUpdate, at)
						}
						return nil
					},
				),
			},
			{
				// A rotation after the import is still detected and patched.
				Config: providerHCL(m) + secretHCL("origin-auth", "shared token", "s3cr3t-value-2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cdnpro_secret.auth", plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					if secret, _ := stored(); secret != "s3cr3t-value-2" {
						return fmt.Errorf("mock stores secret %q, want the rotated value", secret)
					}
					return nil
				},
			},
		},
	})
}

func TestAccWebhookImportAdoption(t *testing.T) {
	m := testAccSetup(t)
	var id string
	var lastUpdate time.Time
	stored := func() (string, time.Time) {
		m.mu.Lock()
		defer m.mu.Unlock()
		wh := m.webhooks[id]
		key, _ := wh.credentials["secretKey"].(string)
		return key, wh.lastUpdate
	}
	config := providerHCL(m) + webhookHCL("deploy-notify", "https://hooks.example.com/cdn", true, "key-one")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_webhook.notify", &id),
					func(*terraform.State) error { _, lastUpdate = stored(); return nil },
				),
			},
			{
				Config: providerHCL(m) + forgetHCL("cdnpro_webhook.notify"),
			},
			{
				Config:             config,
				ResourceName:       "cdnpro_webhook.notify",
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return id, nil },
				ImportStatePersist: true,
			},
			{
				// Same configuration after import: the plan only carries the
				// recording apply (last_update_time unknown); credentials untouched.
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cdnpro_webhook.notify", plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue("cdnpro_webhook.notify", tfjsonpath.New("last_update_time")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cdnpro_webhook.notify", "credentials.user", "cdn-notifier"),
					resource.TestCheckNoResourceAttr("cdnpro_webhook.notify", "credentials.secret_key"),
					func(*terraform.State) error {
						key, at := stored()
						if key != "key-one" || !at.Equal(lastUpdate) {
							return fmt.Errorf("webhook written after import: key=%q lastUpdate %s -> %s", key, lastUpdate, at)
						}
						return nil
					},
				),
			},
			{
				// A key rotation after the import is still detected and patched.
				Config: providerHCL(m) + webhookHCL("deploy-notify", "https://hooks.example.com/cdn", true, "key-two"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cdnpro_webhook.notify", plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					if key, _ := stored(); key != "key-two" {
						return fmt.Errorf("mock stores key %q, want the rotated value", key)
					}
					return nil
				},
			},
		},
	})
}

func TestAccCertificateImportAdoption(t *testing.T) {
	m := testAccSetup(t)
	var id string
	versionCount := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if got := len(m.certificates[id].versions); got != want {
				return fmt.Errorf("mock holds %d certificate versions, want %d", got, want)
			}
			return nil
		}
	}
	config := providerHCL(m) + certificateHCL("v1", "RSA2048", "GENERATE", true)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_certificate.test", &id),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "1"),
				),
			},
			{
				Config: providerHCL(m) + forgetHCL("cdnpro_certificate.test"),
			},
			{
				Config:             config,
				ResourceName:       "cdnpro_certificate.test",
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return id, nil },
				ImportStatePersist: true,
			},
			{
				// Same configuration after import: identification_info (never
				// returned by the API) is recorded from the configuration, which
				// is an update of state only. No version is uploaded and
				// latest_version keeps its value already in the plan.
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cdnpro_certificate.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue("cdnpro_certificate.test", tfjsonpath.New("latest_version"), knownvalue.Int64Exact(1)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "1"),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "identification_info.country", "US"),
					resource.TestCheckNoResourceAttr("cdnpro_certificate.test", "private_key"),
					versionCount(1),
				),
			},
			{
				// New material after the import is still detected: version 2.
				Config: providerHCL(m) + certificateHCL("v1", testPrivateKeyPEM, testCertificatePEM, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "2"),
					versionCount(2),
				),
			},
		},
	})
}

// An uploaded certificate has no identification_info to record, so the
// recording apply is forced through latest_version instead; it must keep its
// value and no version may be uploaded.
func TestAccCertificateImportAdoptionUploaded(t *testing.T) {
	m := testAccSetup(t)
	var id string
	versionCount := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if got := len(m.certificates[id].versions); got != want {
				return fmt.Errorf("mock holds %d certificate versions, want %d", got, want)
			}
			return nil
		}
	}
	config := providerHCL(m) + certificateHCL("v1", testPrivateKeyPEM, testCertificatePEM, false)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_certificate.test", &id),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "1"),
				),
			},
			{
				Config: providerHCL(m) + forgetHCL("cdnpro_certificate.test"),
			},
			{
				Config:             config,
				ResourceName:       "cdnpro_certificate.test",
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return id, nil },
				ImportStatePersist: true,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cdnpro_certificate.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue("cdnpro_certificate.test", tfjsonpath.New("latest_version")),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "1"),
					versionCount(1),
				),
			},
			{
				// Switching to generated material after the import is detected.
				Config: providerHCL(m) + certificateHCL("v1", "RSA2048", "GENERATE", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "2"),
					versionCount(2),
				),
			},
		},
	})
}

// QA finding #4: a prefetch that fails must show the per-URL errors the API
// attaches to the task, even though the API publishes them a moment after the
// terminal status. Terraform wraps long diagnostics, hence \s+ between words.
func TestAccPrefetchActionPartialFailure(t *testing.T) {
	m := testAccSetup(t)
	config := providerHCL(m) + `
action "cdnpro_prefetch" "warm" {
  config {
    file_list = [{ url = "https://www.example.com/partial/missing.bin" }]
  }
}

resource "terraform_data" "trigger" {
  input = "v1"
  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.cdnpro_prefetch.warm]
    }
  }
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{{
			Config: config,
			ExpectError: regexp.MustCompile(`Prefetch partially failed[\s\S]*0\.0%[\s\S]*` +
				`\[UnexpectedStatusCode-404\]\s+prefetch\s+https://www\.example\.com/partial/missing\.bin\s+failed[\s\S]*` +
				`GET\s+/cdn/prefetches/pf\d+`),
		}},
	})
}
