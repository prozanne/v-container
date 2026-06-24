// Package sshx is the SSH/SFTP transport layer used to talk to guests: key
// generation, connection (with retry/wait), command execution, interactive
// shells and SFTP file access. It deals only with the SSH protocol and the
// local terminal — it has no knowledge of VMs or QEMU.
package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// Identity bundles a usable SSH signer with its authorized_keys public line.
type Identity struct {
	Signer        ssh.Signer
	PublicKeyLine string // exactly what goes into ~/.ssh/authorized_keys
}

// GenerateKey creates a fresh ed25519 keypair and returns the OpenSSH-format
// PEM private key and the single-line authorized_keys public key.
func GenerateKey(comment string) (privPEM []byte, pubLine []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("derive public key: %w", err)
	}
	return pem.EncodeToMemory(block), ssh.MarshalAuthorizedKey(sshPub), nil
}

// WriteKeyPair generates a keypair and writes the private key to privPath (0600)
// and the public key to privPath+".pub" (0644), returning the public line.
func WriteKeyPair(privPath, comment string) (pubLine []byte, err error) {
	priv, pub, err := GenerateKey(comment)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(privPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(privPath, priv, 0o600); err != nil {
		return nil, fmt.Errorf("write private key %s: %w", privPath, err)
	}
	if err := os.WriteFile(privPath+".pub", pub, 0o644); err != nil {
		return nil, fmt.Errorf("write public key %s.pub: %w", privPath, err)
	}
	return pub, nil
}

// LoadIdentity reads a private key file and returns a ready-to-use Identity.
func LoadIdentity(privPath string) (*Identity, error) {
	data, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", privPath, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse private key %s: %w", privPath, err)
	}
	return &Identity{
		Signer:        signer,
		PublicKeyLine: string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
	}, nil
}
