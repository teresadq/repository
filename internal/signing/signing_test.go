package signing

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"testing"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/clearsign"
)

func TestAPTSignRelease(t *testing.T) {
	key, err := LoadOrCreateAPT(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(key.PublicArmor(), []byte("BEGIN PGP PUBLIC KEY BLOCK")) {
		t.Fatal("public key armor missing")
	}
	release := []byte("Origin: kkrepo-go\nSuite: stable\nSHA256:\n abc 12 main/binary-amd64/Packages\n")
	inRelease, detached, err := key.SignRelease(release)
	if err != nil {
		t.Fatal(err)
	}
	block, rest := clearsign.Decode(inRelease)
	if block == nil || len(rest) != 0 {
		t.Fatal("invalid InRelease clearsign block")
	}
	if !bytes.Contains(block.Plaintext, release) {
		t.Fatalf("clearsigned text = %s", block.Plaintext)
	}
	if _, err := openpgp.CheckDetachedSignature(openpgp.EntityList{key.entity},
		bytes.NewReader(block.Bytes), block.ArmoredSignature.Body); err != nil {
		t.Fatalf("InRelease signature: %v", err)
	}
	armored, err := armor.Decode(bytes.NewReader(detached))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openpgp.CheckDetachedSignature(openpgp.EntityList{key.entity},
		bytes.NewReader(release), armored.Body); err != nil {
		t.Fatalf("Release.gpg signature: %v", err)
	}
}

func TestAlpineSign(t *testing.T) {
	key, err := LoadOrCreateAlpine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unsigned := []byte("unsigned APKINDEX gzip bytes")
	name, signature, err := key.Sign(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	if name != ".SIGN.RSA.kkrepo-go.rsa.pub" {
		t.Fatalf("entry name = %s", name)
	}
	sum := sha1.Sum(unsigned)
	if err := rsa.VerifyPKCS1v15(&key.private.PublicKey, crypto.SHA1, sum[:], signature); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !bytes.Contains(key.PublicPEM(), []byte("BEGIN PUBLIC KEY")) {
		t.Fatal("public PEM missing")
	}
}
