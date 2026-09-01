// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package managerapp

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

// CertificateReadinessChecker validates the serving key pair, validity window,
// exact Service DNS SANs, and the configured CA chain. The controller-runtime
// webhook server uses its certwatcher by default, so a same-CA key rotation is
// picked up without a manager restart; the checker observes the current files.
func CertificateReadinessChecker(certPath, keyPath, caPath string, dnsNames []string) healthz.Checker {
	return func(_ *http.Request) error {
		if err := validateCertificateFiles(certPath, keyPath, caPath, dnsNames); err != nil {
			return err
		}
		return nil
	}
}

func validateCertificateFiles(certPath, keyPath, caPath string, dnsNames []string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return errors.New("webhook serving certificate is unavailable")
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return errors.New("webhook serving key is unavailable")
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return errors.New("webhook serving key pair is invalid")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return errors.New("webhook serving certificate is invalid")
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return errors.New("webhook serving certificate is outside its validity window")
	}
	for _, expected := range dnsNames {
		if !containsExact(leaf.DNSNames, expected) {
			return fmt.Errorf("webhook serving certificate is missing required DNS SAN")
		}
	}

	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		certificate, parseErr := x509.ParseCertificate(der)
		if parseErr != nil {
			return errors.New("webhook certificate chain is invalid")
		}
		intermediates.AddCert(certificate)
	}
	roots, err := loadCertificateAuthorityPool(caPath)
	if err != nil {
		return err
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime:   now,
	}); err != nil {
		return errors.New("webhook serving certificate is not trusted by the configured CA")
	}
	return nil
}

func loadCertificateAuthorityPool(caPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, errors.New("webhook CA bundle is unavailable")
	}
	roots := x509.NewCertPool()
	added := false
	for rest := caPEM; len(rest) > 0; {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remaining
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return nil, errors.New("webhook CA bundle is invalid")
		}
		roots.AddCert(certificate)
		added = true
	}
	if !added {
		return nil, errors.New("webhook CA bundle is empty")
	}
	return roots, nil
}

func containsExact(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
