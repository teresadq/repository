package signing

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// Alpine holds the RSA key used for APK v2 index signatures.
type Alpine struct {
	private     *rsa.PrivateKey
	publicPEM   []byte
	keyFilename string
}

const alpineKeyFilename = "kkrepo-go.rsa.pub"

// LoadOrCreateAlpine loads data/alpine-signing-key.pem or generates a key.
func LoadOrCreateAlpine(dataDir string) (*Alpine, error) {
	path := filepath.Join(dataDir, "alpine-signing-key.pem")
	var private *rsa.PrivateKey
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("invalid Alpine signing key PEM")
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("Alpine signing key must be RSA")
		}
		private = key
	} else if os.IsNotExist(err) {
		generated, err := rsa.GenerateKey(rand.Reader, 3072)
		if err != nil {
			return nil, err
		}
		encoded, err := x509.MarshalPKCS8PrivateKey(generated)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
			return nil, err
		}
		private = generated
	} else {
		return nil, err
	}
	publicEncoded, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return nil, err
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicEncoded})
	return &Alpine{private: private, publicPEM: publicPEM, keyFilename: alpineKeyFilename}, nil
}

// Sign signs the unsigned APKINDEX archive and returns the tar entry name and
// RSA/SHA-1 signature (the APK "RSA" signature type).
func (a *Alpine) Sign(unsigned []byte) (string, []byte, error) {
	sum := sha1.Sum(unsigned)
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.private, crypto.SHA1, sum[:])
	if err != nil {
		return "", nil, err
	}
	return ".SIGN.RSA." + a.keyFilename, signature, nil
}

// PublicPEM returns the PEM public key for clients.
func (a *Alpine) PublicPEM() []byte { return a.publicPEM }

// KeyFilename returns the public key filename used in signature entries.
func (a *Alpine) KeyFilename() string { return a.keyFilename }
