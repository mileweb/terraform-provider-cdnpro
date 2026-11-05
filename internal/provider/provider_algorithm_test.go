package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// QA finding #5 (NCAS-5719): the API refuses a certificate version whose key
// algorithm differs from the existing versions, and in v0.1.1 that surfaced
// only as a 400 during apply. A switch between RSA and EC must be planned as a
// replacement; a change within the family stays an in-place new version.
func TestAccCertificateAlgorithmChange(t *testing.T) {
	m := testAccSetup(t)
	rsaKey, rsaCert := testKeyMaterial(t, algorithmRSA)
	ecKey, ecCert := testKeyMaterial(t, algorithmEC)
	var firstID, secondID, thirdID string
	certificateCount := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if got := len(m.certificates); got != want {
				return fmt.Errorf("mock holds %d certificates, want %d", got, want)
			}
			return nil
		}
	}
	differs := func(a, b *string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if *a == *b {
				return fmt.Errorf("certificate %s was updated in place, want a replacement", *a)
			}
			return nil
		}
	}
	expect := func(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
		return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
			plancheck.ExpectResourceAction("cdnpro_certificate.test", action),
		}}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories(),
		Steps: []resource.TestStep{
			{
				Config: providerHCL(m) + certificateHCL("v1", "RSA2048", "GENERATE", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_certificate.test", &firstID),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "algorithm", "RSA"),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "key_length", "2048"),
				),
			},
			{
				// RSA2048 -> ECC256: replacement, not a new version.
				Config:           providerHCL(m) + certificateHCL("v1", "ECC256", "GENERATE", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionReplace),
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_certificate.test", &secondID),
					differs(&firstID, &secondID),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "algorithm", "EC"),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "key_length", "256"),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "1"),
					certificateCount(1),
				),
			},
			{
				// Uploaded EC material keeps the family: version 2 in place.
				Config:           providerHCL(m) + certificateHCL("v1", ecKey, ecCert, false),
				ConfigPlanChecks: expect(plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr("cdnpro_certificate.test", "id", &secondID),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "algorithm", "EC"),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "2"),
				),
			},
			{
				// Uploaded RSA material: replacement again.
				Config:           providerHCL(m) + certificateHCL("v1", rsaKey, rsaCert, false),
				ConfigPlanChecks: expect(plancheck.ResourceActionReplace),
				Check: resource.ComposeAggregateTestCheckFunc(
					rememberID("cdnpro_certificate.test", &thirdID),
					differs(&secondID, &thirdID),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "algorithm", "RSA"),
					resource.TestCheckResourceAttr("cdnpro_certificate.test", "latest_version", "1"),
					certificateCount(1),
				),
			},
		},
	})
}
