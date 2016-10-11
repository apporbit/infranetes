// Copyright 2015 Apcera Inc. All rights reserved.

package ssh

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"

	gossh "golang.org/x/crypto/ssh"
)

// NewKeyPair generates a new SSH keypair. This will return a private & public key encoded as DER.
func NewKeyPair() (keyPair *KeyPair, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, ErrKeyGeneration
	}

	if err := priv.Validate(); err != nil {
		return nil, ErrValidation
	}

	privDer := x509.MarshalPKCS1PrivateKey(priv)
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Headers: nil, Bytes: privDer})
	pubSSH, err := gossh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, ErrPublicKey
	}

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  gossh.MarshalAuthorizedKey(pubSSH),
	}, nil
}

// KeyPair represents a Public and Private keypair.
type KeyPair struct {
	PrivateKey []byte
	PublicKey  []byte
}

// ReadFromFile reads a keypair from files.
func (kp *KeyPair) ReadFromFile(privateKeyPath string, publicKeyPath string) error {
	b, err := ioutil.ReadFile(privateKeyPath)
	if err != nil {
		return err
	}
	kp.PrivateKey = b

	b, err = ioutil.ReadFile(publicKeyPath)
	if err != nil {
		return err
	}
	kp.PublicKey = b

	return nil
}

// WriteToFile writes a keypair to files
func (kp *KeyPair) WriteToFile(privateKeyPath string, publicKeyPath string) error {
	files := []struct {
		File  string
		Type  string
		Value []byte
	}{
		{
			File:  privateKeyPath,
			Value: kp.PrivateKey,
		},
		{
			File:  publicKeyPath,
			Value: kp.PublicKey,
		},
	}

	for _, v := range files {
		f, err := os.Create(v.File)
		if err != nil {
			return ErrUnableToWriteFile
		}

		if _, err := f.Write(v.Value); err != nil {
			return ErrUnableToWriteFile
		}

		// windows does not support chmod
		switch runtime.GOOS {
		case "darwin", "linux":
			if err := f.Chmod(0600); err != nil {
				return err
			}
		}
	}

	return nil
}

// Fingerprint calculates the fingerprint of the public key
func (kp *KeyPair) Fingerprint() string {
	b, _ := base64.StdEncoding.DecodeString(string(kp.PublicKey))
	h := md5.New()

	io.WriteString(h, string(b))

	return fmt.Sprintf("%x", h.Sum(nil))
}
