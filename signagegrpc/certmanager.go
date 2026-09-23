package signagegrpc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CertManager owns the on-disk TLS material and bearer tokens. Files live
// under filesDir/tls/. On first call all material is generated; subsequent
// instances reuse existing files until Rotate is called.
type CertManager struct {
	mu        sync.RWMutex
	filesDir  string
	tlsDir    string
	serverCer []byte
	serverKey []byte
	clientCer []byte
	clientKey []byte
	token     string
	adminTok  string
}

// NewCertManager opens the cert store at filesDir/tls, creating it if missing.
func NewCertManager(filesDir string) (*CertManager, error) {
	cm := &CertManager{
		filesDir: filesDir,
		tlsDir:   filepath.Join(filesDir, "tls"),
	}
	if err := os.MkdirAll(cm.tlsDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir tls dir: %w", err)
	}
	if err := cm.loadOrInit(); err != nil {
		return nil, err
	}
	return cm, nil
}

func (cm *CertManager) loadOrInit() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	files := map[string]*[]byte{
		"cert.pem":        &cm.serverCer,
		"key.pem":         &cm.serverKey,
		"client-cert.pem": &cm.clientCer,
		"client-key.pem":  &cm.clientKey,
	}
	missing := false
	for name, sink := range files {
		b, err := os.ReadFile(filepath.Join(cm.tlsDir, name))
		if err != nil {
			missing = true
			break
		}
		*sink = b
	}
	tokenPath := filepath.Join(cm.tlsDir, "token.txt")
	adminPath := filepath.Join(cm.tlsDir, "admin-token.txt")
	if !missing {
		if b, err := os.ReadFile(tokenPath); err == nil {
			cm.token = strings.TrimSpace(string(b))
		} else {
			missing = true
		}
		if b, err := os.ReadFile(adminPath); err == nil {
			cm.adminTok = strings.TrimSpace(string(b))
		} else {
			missing = true
		}
	}
	if !missing {
		return nil
	}
	return cm.regenerateLocked()
}

func (cm *CertManager) regenerateLocked() error {
	srvCer, srvKey, err := genSelfSigned("go-signage server")
	if err != nil {
		return err
	}
	cliCer, cliKey, err := genSelfSigned("go-signage client")
	if err != nil {
		return err
	}
	cm.serverCer, cm.serverKey = srvCer, srvKey
	cm.clientCer, cm.clientKey = cliCer, cliKey
	cm.token = randomToken()
	cm.adminTok = randomToken()

	for name, data := range map[string][]byte{
		"cert.pem":        srvCer,
		"key.pem":         srvKey,
		"client-cert.pem": cliCer,
		"client-key.pem":  cliKey,
		"token.txt":       []byte(cm.token),
		"admin-token.txt": []byte(cm.adminTok),
	} {
		if err := os.WriteFile(filepath.Join(cm.tlsDir, name), data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func (cm *CertManager) ServerCertPEM() []byte {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return append([]byte(nil), cm.serverCer...)
}
func (cm *CertManager) ServerKeyPEM() []byte {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return append([]byte(nil), cm.serverKey...)
}
func (cm *CertManager) ClientCertPEM() []byte {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return append([]byte(nil), cm.clientCer...)
}
func (cm *CertManager) ClientKeyPEM() []byte {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return append([]byte(nil), cm.clientKey...)
}

func (cm *CertManager) Token() string      { cm.mu.RLock(); defer cm.mu.RUnlock(); return cm.token }
func (cm *CertManager) AdminToken() string { cm.mu.RLock(); defer cm.mu.RUnlock(); return cm.adminTok }

// Fingerprint returns the colon-separated SHA-256 fingerprint of the DER
// encoding of the server certificate.
func (cm *CertManager) Fingerprint() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return fingerprintPEM(cm.serverCer)
}

// ClientFingerprint returns the SHA-256 fingerprint of the issued client cert.
func (cm *CertManager) ClientFingerprint() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return fingerprintPEM(cm.clientCer)
}

// Rotate replaces every credential and rewrites the on-disk material.
func (cm *CertManager) Rotate() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.regenerateLocked()
}

// ReissueClientCert replaces only the client material, keeping server
// identity and bearer tokens stable.
func (cm *CertManager) ReissueClientCert() error {
	cliCer, cliKey, err := genSelfSigned("go-signage client")
	if err != nil {
		return err
	}
	cm.mu.Lock()
	cm.clientCer, cm.clientKey = cliCer, cliKey
	cm.mu.Unlock()
	if err := os.WriteFile(filepath.Join(cm.tlsDir, "client-cert.pem"), cliCer, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cm.tlsDir, "client-key.pem"), cliKey, 0o600)
}

// ClientCAPool returns an x509.CertPool that contains exactly the issued
// client certificate — used by the server's mTLS TrustManager.
func (cm *CertManager) ClientCAPool() (*x509.CertPool, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cm.clientCer) {
		return nil, errors.New("signagegrpc: failed to parse client cert PEM")
	}
	return pool, nil
}

// --- helpers ----------------------------------------------------------------

func genSelfSigned(commonName string) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	cerPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return cerPem, keyPem, nil
}

func randomToken() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func fingerprintPEM(certPEM []byte) string {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	out := make([]string, len(sum))
	for i, b := range sum {
		out[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return strings.Join(out, ":")
}
