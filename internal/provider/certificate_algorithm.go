package provider

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// Key families the CDN Pro API distinguishes. The `algorithm` of a version is
// the Java PublicKey algorithm name ("RSA", "EC" or "ECDSA"), and every version
// of a certificate has to share one family: the API rejects a new version of
// the other family with 400 InvalidCertVersion. The provider therefore plans
// such a change as a replacement (NCAS-5719 QA finding #5).
const (
	algorithmRSA = "RSA"
	algorithmEC  = "EC"
)

// normalizeAlgorithm folds an algorithm name as reported by the API into one
// of the families, or "" when it is unknown.
func normalizeAlgorithm(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, algorithmRSA):
		return algorithmRSA
	case strings.HasPrefix(s, algorithmEC):
		return algorithmEC
	}
	return ""
}

// configuredAlgorithm derives the key family of the configured material: from
// the RSA2048 / ECC256 keywords, from the private key PEM or, when the key
// cannot be parsed (encrypted, or an encoding Go does not read), from the
// public key of the certificate PEM. "" means it could not be determined, in
// which case the plan stays an in-place update and the API has the last word.
func configuredAlgorithm(privateKey, certificate string) string {
	if a := privateKeyAlgorithm(privateKey); a != "" {
		return a
	}
	return certificateAlgorithm(certificate)
}

func privateKeyAlgorithm(s string) string {
	// Keywords that ask the API to generate the key (client.IsPrivateKeyKeyword).
	switch s {
	case "RSA2048":
		return algorithmRSA
	case "ECC256":
		return algorithmEC
	}
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return ""
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return algorithmRSA
	case "EC PRIVATE KEY":
		return algorithmEC
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return ""
		}
		switch key.(type) {
		case *rsa.PrivateKey:
			return algorithmRSA
		case *ecdsa.PrivateKey:
			return algorithmEC
		}
	}
	return ""
}

func certificateAlgorithm(s string) string {
	block, _ := pem.Decode([]byte(s))
	if block == nil || block.Type != "CERTIFICATE" {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return ""
	}
	switch cert.PublicKeyAlgorithm {
	case x509.RSA:
		return algorithmRSA
	case x509.ECDSA:
		return algorithmEC
	}
	return ""
}

// addAlgorithmChangeWarning accompanies the replacement planned by ModifyPlan.
func addAlgorithmChangeWarning(diags *diag.Diagnostics, id, have, want string) {
	diags.AddAttributeWarning(path.Root("private_key"),
		"Changing the key algorithm replaces the certificate",
		fmt.Sprintf("The versions of certificate %s use %s keys and the configured material is %s. "+
			"The CDN Pro API requires every version of a certificate to keep the key algorithm (a new version of the other "+
			"kind is rejected with InvalidCertVersion), so the certificate is destroyed and created again under a new ID. "+
			"If it is referenced by a property or a deployment, add `lifecycle { create_before_destroy = true }` to the "+
			"certificate so that the replacement exists before those resources move to it and the old certificate is deleted last.",
			id, have, want))
}
