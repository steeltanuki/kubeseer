// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/internal/managerapp"
)

func assertPackagingWebhookTLSScenarios(t *testing.T) {
	t.Helper()
	chartDir := packagingChartDir(t)

	certManagerText := renderPackagingChart(t, chartDir)
	for _, fragment := range []string{
		"apiVersion: cert-manager.io/v1",
		"kind: Issuer",
		"selfSigned: {}",
		"kind: Certificate",
		"isCA: true",
		"rotationPolicy: Always",
		`cert-manager.io/inject-ca-from: "kubeseer-system/kubeseer-webhook-ca"`,
		"dnsNames:\n    - kubeseer-webhook\n    - kubeseer-webhook.kubeseer-system\n    - kubeseer-webhook.kubeseer-system.svc\n    - kubeseer-webhook.kubeseer-system.svc.cluster.local",
		"failurePolicy: Fail",
		"path: /validate-kubeseer-io-v1alpha1-kubeseer",
		"path: /validate-kubeseer-io-v1alpha1-kubeseeraccesspolicy",
	} {
		if !strings.Contains(certManagerText, fragment) {
			t.Fatalf("cert-manager render lacks %q", fragment)
		}
	}

	externalText := renderPackagingChart(t, chartDir,
		"--set", "certificate.mode=externalSecret",
		"--set", "certificate.externalSecret.secretName=administrator-webhook-tls",
		"--set-string", "certificate.externalSecret.caBundle=PUBLIC-CA",
	)
	if strings.Contains(externalText, "cert-manager.io/") || strings.Contains(externalText, "kind: Secret") {
		t.Fatal("external Secret mode rendered cert-manager resources or a TLS Secret")
	}
	for _, fragment := range []string{
		"secretName: \"administrator-webhook-tls\"",
		"ca.crt: |-",
		"PUBLIC-CA",
		`caBundle: "UFVCTElDLUNB"`,
		"failurePolicy: Fail",
		"path: /validate-kubeseer-io-v1alpha1-kubeseer",
		"path: /validate-kubeseer-io-v1alpha1-kubeseeraccesspolicy",
	} {
		if !strings.Contains(externalText, fragment) {
			t.Fatalf("external Secret render lacks %q", fragment)
		}
	}

	assertPackagingWebhookTLSInvalidValues(t, chartDir)
	assertPackagingWebhookCertificateReadiness(t)

	t.Log("MODULE_INTEGRATION=packaging-webhook-tls STATUS=passed")
}

func assertPackagingWebhookTLSInvalidValues(t *testing.T, chartDir string) {
	t.Helper()
	for _, values := range [][]string{
		{"--set", "certificate.mode=unsupported"},
		{"--set", "certificate.mode=externalSecret"},
		{"--set", "certificate.mode=externalSecret", "--set", "certificate.externalSecret.secretName=administrator-webhook-tls"},
	} {
		commandArgs := append([]string{"template", "kubeseer", chartDir, "--namespace", "kubeseer-system", "--kube-version", "1.35.6"}, values...)
		if output, err := exec.Command("helm", commandArgs...).CombinedOutput(); err == nil {
			t.Fatalf("invalid certificate values unexpectedly rendered successfully: %v\n%s", values, output)
		}
	}
}

func assertPackagingWebhookCertificateReadiness(t *testing.T) {
	t.Helper()
	fixture := newWebhookCertificateFixture(t)
	check := managerapp.CertificateReadinessChecker(fixture.certPath, fixture.keyPath, fixture.caPath, fixture.dnsNames)
	if err := check(nil); err != nil {
		t.Fatalf("valid webhook certificate is not ready: %v", err)
	}

	// A same-CA serving rotation is safe: the checker reads the current files,
	// matching the controller-runtime watcher update boundary.
	fixture.writeCA(t, fixture.caCertificate)
	fixture.writeServingCertificate(t, fixture.caCertificate, fixture.caKey)
	if err := check(nil); err != nil {
		t.Fatalf("same-CA serving rotation is not ready: %v", err)
	}

	fixture.writeCA(t, fixture.otherCACertificate)
	if err := check(nil); err == nil {
		t.Fatal("serving certificate remained ready with an unrelated CA")
	}
	fixture.writeCA(t, fixture.caCertificate)
	fixture.writeServingCertificate(t, fixture.newCACertificate, fixture.newCAKey)
	if err := check(nil); err == nil {
		t.Fatal("new-CA serving certificate became ready without an overlap bundle")
	}
	fixture.writeCA(t, fixture.caCertificate, fixture.newCACertificate)
	if err := check(nil); err != nil {
		t.Fatalf("new-CA serving certificate is not ready during overlap: %v", err)
	}
	fixture.writeCA(t, fixture.newCACertificate)
	if err := check(nil); err != nil {
		t.Fatalf("new-CA serving certificate is not ready after rollover: %v", err)
	}

	fixture.writeCA(t, fixture.caCertificate)
	fixture.writeServingCertificate(t, fixture.caCertificate, fixture.caKey)
	if err := check(nil); err != nil {
		t.Fatalf("restored serving certificate is not ready: %v", err)
	}
	fixture.writeServingCertificateWithDNSNames(t, fixture.caCertificate, fixture.caKey, []string{"wrong.example"})
	if err := check(nil); err == nil {
		t.Fatal("certificate with mismatched DNS SAN became ready")
	}
	if _, err := tls.X509KeyPair(fixture.certPEM, fixture.keyPEM); err != nil {
		t.Fatalf("fixture certificate/key pair is invalid: %v", err)
	}
}

type webhookCertificateFixture struct {
	certPath             string
	keyPath              string
	caPath               string
	dnsNames             []string
	caCertificate        *x509.Certificate
	caKey                *rsa.PrivateKey
	newCACertificate     *x509.Certificate
	newCAKey             *rsa.PrivateKey
	otherCACertificate   *x509.Certificate
	servingCertificate   *x509.Certificate
	servingKey           *rsa.PrivateKey
	newCACertificateLeaf *x509.Certificate
	newCAServingKey      *rsa.PrivateKey
	certPEM              []byte
	keyPEM               []byte
}

func newWebhookCertificateFixture(t *testing.T) *webhookCertificateFixture {
	t.Helper()
	rootDir := t.TempDir()
	fixture := &webhookCertificateFixture{
		certPath: filepath.Join(rootDir, "tls.crt"),
		keyPath:  filepath.Join(rootDir, "tls.key"),
		caPath:   filepath.Join(rootDir, "ca.crt"),
		dnsNames: []string{
			"kubeseer-webhook",
			"kubeseer-webhook.kubeseer-system",
			"kubeseer-webhook.kubeseer-system.svc",
			"kubeseer-webhook.kubeseer-system.svc.cluster.local",
		},
	}
	fixture.caCertificate, fixture.caKey = mustCreateCA(t, "primary-ca")
	fixture.newCACertificate, fixture.newCAKey = mustCreateCA(t, "replacement-ca")
	fixture.otherCACertificate, _ = mustCreateCA(t, "unrelated-ca")
	fixture.servingCertificate, fixture.servingKey = mustCreateServingCertificate(t, fixture.caCertificate, fixture.caKey, fixture.dnsNames)
	fixture.newCACertificateLeaf, fixture.newCAServingKey = mustCreateServingCertificate(t, fixture.newCACertificate, fixture.newCAKey, fixture.dnsNames)
	fixture.writeCA(t, fixture.caCertificate)
	fixture.writeServingCertificate(t, fixture.caCertificate, fixture.caKey)
	return fixture
}

func (f *webhookCertificateFixture) writeCA(t *testing.T, certificates ...*x509.Certificate) {
	t.Helper()
	var data []byte
	for _, certificate := range certificates {
		data = append(data, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})...)
	}
	if err := os.WriteFile(f.caPath, data, 0o600); err != nil {
		t.Fatalf("write CA bundle: %v", err)
	}
}

func (f *webhookCertificateFixture) writeServingCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey) {
	f.writeServingCertificateWithDNSNames(t, ca, caKey, f.dnsNames)
}

func (f *webhookCertificateFixture) writeServingCertificateWithDNSNames(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, dnsNames []string) {
	t.Helper()
	certificate, key := mustCreateServingCertificate(t, ca, caKey, dnsNames)
	f.certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	f.keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(f.certPath, f.certPEM, 0o600); err != nil {
		t.Fatalf("write serving certificate: %v", err)
	}
	if err := os.WriteFile(f.keyPath, f.keyPEM, 0o600); err != nil {
		t.Fatalf("write serving key: %v", err)
	}
}

func mustCreateCA(t *testing.T, commonName string) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatalf("generate CA serial: %v", err)
	}
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now,
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return certificate, key
}

func mustCreateServingCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, dnsNames []string) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate serving key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatalf("generate serving serial: %v", err)
	}
	now := time.Now().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     append([]string(nil), dnsNames...),
		NotBefore:    now,
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create serving certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse serving certificate: %v", err)
	}
	return certificate, key
}

func packagingChartDir(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	return filepath.Join(filepath.Dir(sourceFile), "..", "..", "charts", "kubeseer")
}

func renderPackagingChart(t *testing.T, chartDir string, values ...string) string {
	t.Helper()
	args := append([]string{"template", "kubeseer", chartDir, "--namespace", "kubeseer-system", "--kube-version", "1.35.6"}, values...)
	output, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("render chart: %v\n%s", err, output)
	}
	return string(output)
}
