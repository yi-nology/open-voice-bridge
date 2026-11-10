package phone

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertificateManagerGeneratesAndReuses(t *testing.T) {
	dir := t.TempDir()
	manager := NewCertificateManager(dir)

	cert, err := manager.Get()
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("empty certificate chain")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if !containsFold(lowerSet(leaf.DNSNames), "localhost") {
		t.Fatalf("SAN DNS names = %v, want localhost", leaf.DNSNames)
	}
	var hasLoopback bool
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			hasLoopback = true
		}
	}
	if !hasLoopback {
		t.Fatalf("SAN IPs = %v, want 127.0.0.1", leaf.IPAddresses)
	}
	// SEC-04: the leaf must not be a CA — trusting it must not turn this
	// host's key into a general-purpose signer.
	if leaf.IsCA {
		t.Fatal("self-signed leaf must not be a CA")
	}
	if leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("key usage = %v, want digital signature only", leaf.KeyUsage)
	}

	keyInfo, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("key perms = %v, want 0600", keyInfo.Mode().Perm())
	}

	second, err := manager.Get()
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	secondLeaf, err := x509.ParseCertificate(second.Certificate[0])
	if err != nil {
		t.Fatalf("parse second leaf: %v", err)
	}
	if secondLeaf.SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Fatal("certificate should be reused, not regenerated")
	}
}

func TestCertificateManagerRebuildsExpired(t *testing.T) {
	dir := t.TempDir()
	base := time.Unix(1_600_000_000, 0)
	manager := NewCertificateManager(dir)
	manager.now = func() time.Time { return base }

	first, err := manager.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	firstLeaf, _ := x509.ParseCertificate(first.Certificate[0])

	// Well before the renewal margin the cached cert keeps being reused.
	manager.now = func() time.Time { return base.Add(certValidFor - certRenewMargin - 24*time.Hour) }
	kept, err := manager.Get()
	if err != nil {
		t.Fatalf("Get before renewal margin: %v", err)
	}
	keptLeaf, _ := x509.ParseCertificate(kept.Certificate[0])
	if keptLeaf.SerialNumber.Cmp(firstLeaf.SerialNumber) != 0 {
		t.Fatal("certificate before the renewal margin should be reused")
	}

	// Near expiry (inside the renewal margin) must rebuild even though the
	// cert is not expired yet: a long-running process must not stay pinned
	// to a cert that is about to die.
	manager.now = func() time.Time { return base.Add(certValidFor - certRenewMargin + 2*time.Hour) }
	second, err := manager.Get()
	if err != nil {
		t.Fatalf("Get near expiry: %v", err)
	}
	secondLeaf, _ := x509.ParseCertificate(second.Certificate[0])
	if secondLeaf.SerialNumber.Cmp(firstLeaf.SerialNumber) == 0 {
		t.Fatal("near-expiry certificate should be regenerated")
	}

	// Fully expired: the second cert is rebuilt too (its NotAfter is
	// secondIssueTime - 1h + certValidFor).
	manager.now = func() time.Time {
		return base.Add(2*certValidFor - certRenewMargin + 8*time.Hour)
	}
	third, err := manager.Get()
	if err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	thirdLeaf, _ := x509.ParseCertificate(third.Certificate[0])
	if thirdLeaf.SerialNumber.Cmp(secondLeaf.SerialNumber) == 0 {
		t.Fatal("expired certificate should be regenerated")
	}
}
