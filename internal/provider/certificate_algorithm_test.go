package provider

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// testKeyMaterial returns a freshly generated PKCS#8 private key and a
// self-signed certificate for it, both PEM encoded, in the given key family.
func testKeyMaterial(t *testing.T, family string) (keyPEM, certPEM string) {
	t.Helper()
	var (
		priv crypto.Signer
		err  error
	)
	switch family {
	case algorithmRSA:
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
	case algorithmEC:
		priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		t.Fatalf("unknown key family %q", family)
	}
	if err != nil {
		t.Fatalf("generate %s key: %v", family, err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal %s key: %v", family, err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "acc.example.com"},
		DNSNames:     []string{"acc.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(240 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		t.Fatalf("self-sign %s certificate: %v", family, err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return keyPEM, certPEM
}

func TestConfiguredAlgorithm(t *testing.T) {
	rsaKey, rsaCert := testKeyMaterial(t, algorithmRSA)
	ecKey, ecCert := testKeyMaterial(t, algorithmEC)

	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaPriv)}))
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sec1DER, err := x509.MarshalECPrivateKey(ecPriv)
	if err != nil {
		t.Fatal(err)
	}
	sec1 := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1DER}))
	encrypted := "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----\n"

	cases := []struct {
		name      string
		key, cert string
		want      string
	}{
		{"keyword RSA2048", "RSA2048", "GENERATE", algorithmRSA},
		{"keyword ECC256", "ECC256", "GENERATE", algorithmEC},
		{"PKCS8 RSA key", rsaKey, rsaCert, algorithmRSA},
		{"PKCS8 EC key", ecKey, ecCert, algorithmEC},
		{"PKCS1 RSA key alone", pkcs1, "", algorithmRSA},
		{"SEC1 EC key alone", sec1, "", algorithmEC},
		{"unreadable key, certificate decides", encrypted, ecCert, algorithmEC},
		{"unreadable key, RSA certificate", encrypted, rsaCert, algorithmRSA},
		{"key wins over certificate", rsaKey, ecCert, algorithmRSA},
		{"truncated test fixtures", testPrivateKeyPEM, testCertificatePEM, ""},
		{"garbage", "not a key", "not a certificate", ""},
		{"empty", "", "", ""},
	}
	for _, tc := range cases {
		if got := configuredAlgorithm(tc.key, tc.cert); got != tc.want {
			t.Errorf("%s: configuredAlgorithm() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeAlgorithm(t *testing.T) {
	cases := map[string]string{
		"RSA": algorithmRSA, "rsa": algorithmRSA, " RSA ": algorithmRSA,
		"EC": algorithmEC, "ECDSA": algorithmEC, "ecdsa": algorithmEC,
		"": "", "DSA": "", "Ed25519": "",
	}
	for in, want := range cases {
		if got := normalizeAlgorithm(in); got != want {
			t.Errorf("normalizeAlgorithm(%q) = %q, want %q", in, got, want)
		}
	}
}
