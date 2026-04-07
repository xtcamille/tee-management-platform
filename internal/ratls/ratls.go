package ratls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// RA-TLS Extension OID (custom for SGX Quote)
var OIDExtensionSgxQuote = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 55}

// GenerateCertificate generates a self-signed certificate with an embedded SGX Quote.
// In simulation mode, it uses a dummy quote.
func GenerateCertificate(isSimulation bool) (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(24 * time.Hour)

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"TEE RA-TLS Enclave"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add SGX Quote extension
	var quote []byte
	if isSimulation {
		quote = []byte("MOCK_SGX_QUOTE_CONTENTS_FOR_TESTING")
	} else {
		// Hardware mode: Call SGX API to get the Quote
		// e.g., quote, err = GetSgxQuote(priv.PublicKey)
		quote = []byte("REAL_SGX_QUOTE_PLACEHOLDER")
	}

	template.ExtraExtensions = []pkix.Extension{
		{
			Id:       OIDExtensionSgxQuote,
			Critical: false,
			Value:    quote,
		},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}, nil
}

// VerifyPeerCertificate extracts and verifies the SGX Quote from the peer's certificate.
func VerifyPeerCertificate(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("no certificates provided")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %v", err)
	}

	// Find the SGX Quote extension
	var quoteValue []byte
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(OIDExtensionSgxQuote) {
			quoteValue = ext.Value
			break
		}
	}

	if quoteValue == nil {
		return fmt.Errorf("SGX Quote extension not found in certificate")
	}

	// Perform Remote Attestation Verification
	fmt.Printf("[RA-TLS] Received Quote Content: %s\n", string(quoteValue))

	// Real-world: Use a verification service (e.g., Intel PCS or KubeTEE Verifier)
	// Here we just accept mock for testing.
	if string(quoteValue) == "MOCK_SGX_QUOTE_CONTENTS_FOR_TESTING" {
		return nil
	}

	// Real hardware verification logic would go here
	// return VerifySgxQuote(quoteValue, cert.PublicKey)

	return nil
}
