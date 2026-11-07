package kernel

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

func NewUUID() string {
	return uuid.NewString()
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return b
}

// RandomPassword returns a URL-safe secret suitable for Trojan / legacy SS.
func RandomPassword() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(24))
}

// SS2022KeyLen returns the PSK length a Shadowsocks 2022 method requires, or 0
// for legacy AEAD methods that accept any password.
func SS2022KeyLen(method string) int {
	switch method {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32
	default:
		return 0
	}
}

func ShadowsocksPassword(method string) string {
	if n := SS2022KeyLen(method); n > 0 {
		return base64.StdEncoding.EncodeToString(randomBytes(n))
	}
	return RandomPassword()
}

// RealityKeyPair returns an X25519 key pair in the raw-URL base64 form used by
// Xray (`xray x25519`) and sing-box.
func RealityKeyPair() (privateKey, publicKey string, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.Bytes()),
		base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// RealityPublicKey derives the public key from a stored private key so a
// user-supplied private key still produces correct share links.
func RealityPublicKey(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("Reality 私钥格式错误")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("Reality 私钥格式错误")
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// WireGuardKeyPair returns a Curve25519 key pair in WireGuard's standard
// base64 form.
func WireGuardKeyPair() (privateKey, publicKey string, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()),
		base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// WireGuardPublicKey derives the public key of a WireGuard private key.
func WireGuardPublicKey(privateKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("WireGuard 私钥格式错误")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("WireGuard 私钥格式错误")
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// VLESS Encryption (Xray 25.9+): ML-KEM-768 + X25519 key exchange with
// X25519 authentication, as "xray vlessenc" generates it. The server keeps
// the private key (decryption), clients get the public one (encryption).
const vlessEncPrefix = "mlkem768x25519plus.native."

// VLESSEncKeys returns a matching decryption / encryption pair.
func VLESSEncKeys() (decryption, encryption string, err error) {
	priv, pub, err := RealityKeyPair()
	if err != nil {
		return "", "", err
	}
	return vlessEncPrefix + "600s." + priv, vlessEncPrefix + "0rtt." + pub, nil
}

// vlessEncryptionFor derives the client setting from a server decryption
// string, or "" when it is not one the panel generated.
func vlessEncryptionFor(decryption string) string {
	rest, ok := strings.CutPrefix(decryption, vlessEncPrefix)
	if !ok {
		return ""
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return ""
	}
	pub, err := RealityPublicKey(parts[1])
	if err != nil {
		return ""
	}
	return vlessEncPrefix + "0rtt." + pub
}

func RealityShortID() string {
	return hex.EncodeToString(randomBytes(8))
}

// SelfSignedCert issues a long-lived ECDSA certificate for serverName. Clients
// must skip verification (share links carry allowInsecure).
func SelfSignedCert(serverName string) (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: serverName},
		DNSNames:              []string{serverName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM, nil
}

func certMatchesName(certPEM, name string) bool {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	return cert.VerifyHostname(name) == nil && time.Now().Before(cert.NotAfter)
}

// randomPort returns a port in [lo, hi).
func randomPort(lo, hi int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(hi-lo)))
	if err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return lo + int(n.Int64())
}
