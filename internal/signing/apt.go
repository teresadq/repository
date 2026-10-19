// Package signing manages local signing keys for APT and Alpine repositories.
package signing

import (
	"bytes"
	"crypto"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/clearsign"
	"golang.org/x/crypto/openpgp/packet"
)

// APT holds an OpenPGP entity used for Release/InRelease signatures.
type APT struct {
	entity      *openpgp.Entity
	publicArmor []byte
}

// LoadOrCreateAPT loads data/apt-signing-key.asc or generates a new RSA key.
func LoadOrCreateAPT(dataDir string) (*APT, error) {
	path := filepath.Join(dataDir, "apt-signing-key.asc")
	var entity *openpgp.Entity
	if raw, err := os.ReadFile(path); err == nil {
		entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(raw))
		if err != nil || len(entities) == 0 {
			return nil, fmt.Errorf("parse APT signing key: %w", err)
		}
		entity = entities[0]
	} else if os.IsNotExist(err) {
		entity, err = openpgp.NewEntity("kkrepo-go", "APT signing key", "kkrepo@localhost",
			&packet.Config{RSABits: 3072, DefaultHash: crypto.SHA256})
		if err != nil {
			return nil, err
		}
		var buffer bytes.Buffer
		armorWriter, err := armor.Encode(&buffer, "PGP PRIVATE KEY BLOCK", nil)
		if err != nil {
			return nil, err
		}
		if err := entity.SerializePrivate(armorWriter, nil); err != nil {
			return nil, err
		}
		if err := armorWriter.Close(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	var public bytes.Buffer
	armorWriter, err := armor.Encode(&public, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		return nil, err
	}
	if err := entity.Serialize(armorWriter); err != nil {
		return nil, err
	}
	if err := armorWriter.Close(); err != nil {
		return nil, err
	}
	return &APT{entity: entity, publicArmor: public.Bytes()}, nil
}

// SignRelease returns a clearsigned InRelease and armored detached Release.gpg.
func (a *APT) SignRelease(release []byte) ([]byte, []byte, error) {
	config := &packet.Config{DefaultHash: crypto.SHA256}
	var inRelease bytes.Buffer
	clearWriter, err := clearsign.Encode(&inRelease, a.entity.PrivateKey, config)
	if err != nil {
		return nil, nil, err
	}
	if _, err := clearWriter.Write(release); err != nil {
		return nil, nil, err
	}
	if err := clearWriter.Close(); err != nil {
		return nil, nil, err
	}
	var detached bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&detached, a.entity, bytes.NewReader(release), config); err != nil {
		return nil, nil, err
	}
	return inRelease.Bytes(), detached.Bytes(), nil
}

// PublicArmor returns the armored public key.
func (a *APT) PublicArmor() []byte { return a.publicArmor }
