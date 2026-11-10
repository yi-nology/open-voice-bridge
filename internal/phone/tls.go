package phone

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	oeerrors "github.com/oekit/pkg/errors"
)

// certificate file names live in the config directory next to the token store.
const (
	certFileName = "phone_cert.pem"
	keyFileName  = "phone_key.pem"

	certValidFor = 825 * 24 * time.Hour // browsers cap trust lifetimes near this
)

// CertificateManager produces and caches a self-signed TLS certificate whose
// SANs cover the local hostname, localhost and every LAN address. It rebuilds
// when the LAN set changes so a phone can always complete the TLS handshake
// after a DHCP lease moves. Keys are written 0600 and never logged.
type CertificateManager struct {
	dir string
	now func() time.Time
}

func NewCertificateManager(dir string) *CertificateManager {
	return &CertificateManager{dir: dir, now: time.Now}
}

// Get returns a usable certificate, generating or regenerating as needed.
func (m *CertificateManager) Get() (tls.Certificate, error) {
	certPath := filepath.Join(m.dir, certFileName)
	keyPath := filepath.Join(m.dir, keyFileName)

	if existing, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(existing.Certificate[0]); err == nil {
			if m.stillValid(leaf) {
				return existing, nil
			}
		}
	}

	dnsNames, ips := localNames()
	pair, err := newSelfSigned(dnsNames, ips, m.now())
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return tls.Certificate{}, oeerrors.Wrap(err, "create cert dir")
	}
	if err := os.WriteFile(keyPath, pair.keyPEM, 0o600); err != nil {
		return tls.Certificate{}, oeerrors.Wrap(err, "write phone key")
	}
	if err := os.WriteFile(certPath, pair.certPEM, 0o644); err != nil {
		return tls.Certificate{}, oeerrors.Wrap(err, "write phone cert")
	}
	cert, err := tls.X509KeyPair(pair.certPEM, pair.keyPEM)
	if err != nil {
		return tls.Certificate{}, oeerrors.Wrap(err, "reload generated cert")
	}
	return cert, nil
}

// stillValid is true when the cert is unexpired and its SANs already cover the
// current local name/IP set.
func (m *CertificateManager) stillValid(leaf *x509.Certificate) bool {
	now := m.now()
	if now.After(leaf.NotAfter) || now.Add(24*time.Hour).Before(leaf.NotBefore) {
		return false
	}
	wantDNS, wantIPs := localNames()
	dns := lowerSet(leaf.DNSNames)
	for _, name := range wantDNS {
		if !containsFold(dns, name) {
			return false
		}
	}
	existing := make(map[string]bool, len(leaf.IPAddresses))
	for _, ip := range leaf.IPAddresses {
		existing[ip.String()] = true
	}
	for _, ip := range wantIPs {
		if !existing[ip.String()] {
			return false
		}
	}
	return true
}

type certPair struct {
	certPEM []byte
	keyPEM  []byte
}

func newSelfSigned(dnsNames []string, ips []net.IP, now time.Time) (certPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return certPair{}, oeerrors.Wrap(err, "generate phone key")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return certPair{}, oeerrors.Wrap(err, "phone cert serial")
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Open Voice Bridge Phone", Organization: []string{"yi-nology"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certValidFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return certPair{}, oeerrors.Wrap(err, "sign phone cert")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return certPair{}, oeerrors.Wrap(err, "marshal phone key")
	}
	return certPair{
		certPEM: pemBlock("CERTIFICATE", der),
		keyPEM:  pemBlock("EC PRIVATE KEY", keyDER),
	}, nil
}

// localNames gathers the hostname, localhost and all non-loopback/unspecified
// interface addresses so generated SANs stay current with the network.
func localNames() (dnsNames []string, ips []net.IP) {
	dnsSet := map[string]bool{"localhost": true}
	ipSet := map[string]bool{"127.0.0.1": true, "::1": true}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		dnsSet[hostname] = true
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			ipSet[ip.String()] = true
		}
	}
	dnsNames = sortedKeys(dnsSet)
	for _, value := range sortedKeys(ipSet) {
		if ip := net.ParseIP(value); ip != nil {
			ips = append(ips, ip)
		}
	}
	return dnsNames, ips
}
