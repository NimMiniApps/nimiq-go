package signer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/scrypt"
)

// Encrypted-at-rest key format.
//
// This is a deliberately boring composition of standard primitives — scrypt for
// key derivation, AES-256-GCM for authenticated encryption — rather than
// anything novel. The parameters below are the interactive-use defaults from
// the scrypt paper; raising N raises both attacker cost and unlock latency.
//
// It exists because the alternative found in practice is a raw hex private key
// in an environment variable, which is readable by anything that can list the
// process environment, is copied into container inspect output and orchestrator
// state, and tends to end up in logs.
//
// This protects a key at rest against someone who obtains the file. It does not
// protect a running process: once unlocked, the key is in ordinary memory. For
// a treasury, use a signer backed by a KMS or HSM instead — no transaction code
// changes, only the nimiq.Signer implementation.
const (
	scryptN      = 1 << 15 // ~32 MiB, a few hundred ms to derive
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 32
	saltLen      = 16
)

// encryptedKeyFile is the on-disk representation. It is JSON so that the
// parameters travel with the ciphertext: a file encrypted today must still be
// readable after the defaults above are raised.
type encryptedKeyFile struct {
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	N       int    `json:"n"`
	R       int    `json:"r"`
	P       int    `json:"p"`
	Salt    string `json:"salt"`
	Cipher  string `json:"cipher"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
	// Address is stored in the clear so a file can be identified without
	// unlocking it. It is not a secret, and it is verified against the
	// decrypted key on load.
	Address string `json:"address"`
}

// EncryptPrivateKey encrypts a 32-byte private key under a passphrase,
// returning the serialized key file.
func EncryptPrivateKey(seed []byte, passphrase []byte) ([]byte, error) {
	key, err := NewPrivateKey(seed)
	if err != nil {
		return nil, err
	}
	if len(passphrase) == 0 {
		return nil, fmt.Errorf("nimiq/signer: refusing to encrypt with an empty passphrase")
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("nimiq/signer: generating salt: %w", err)
	}
	derived, err := scrypt.Key(passphrase, salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: deriving key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nimiq/signer: generating nonce: %w", err)
	}

	address := key.Address().String()
	// Bind the address to the ciphertext so it cannot be edited to misattribute
	// a file without the tampering being detected on unlock.
	sealed := aead.Seal(nil, nonce, seed, []byte(address))

	return json.MarshalIndent(encryptedKeyFile{
		Version: 1,
		KDF:     "scrypt",
		N:       scryptN, R: scryptR, P: scryptP,
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Cipher:  "aes-256-gcm",
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(sealed),
		Address: address,
	}, "", "  ")
}

// DecryptPrivateKey recovers a signer from an encrypted key file.
//
// The error deliberately does not distinguish a wrong passphrase from a
// corrupted file, and never echoes the passphrase or any plaintext.
func DecryptPrivateKey(fileData, passphrase []byte) (*PrivateKey, error) {
	var f encryptedKeyFile
	if err := json.Unmarshal(fileData, &f); err != nil {
		return nil, fmt.Errorf("nimiq/signer: key file is not valid JSON")
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("nimiq/signer: unsupported key file version %d", f.Version)
	}
	if f.KDF != "scrypt" || f.Cipher != "aes-256-gcm" {
		return nil, fmt.Errorf("nimiq/signer: unsupported kdf %q or cipher %q", f.KDF, f.Cipher)
	}
	// Bound the work factor: a file claiming an enormous N would otherwise let
	// a hostile file exhaust memory on load.
	if f.N <= 0 || f.N > 1<<22 || f.R <= 0 || f.R > 64 || f.P <= 0 || f.P > 16 {
		return nil, fmt.Errorf("nimiq/signer: key file has implausible scrypt parameters")
	}
	salt, err := base64.StdEncoding.DecodeString(f.Salt)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: key file salt is malformed")
	}
	nonce, err := base64.StdEncoding.DecodeString(f.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: key file nonce is malformed")
	}
	sealed, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: key file data is malformed")
	}

	derived, err := scrypt.Key(passphrase, salt, f.N, f.R, f.P, scryptKeyLen)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: deriving key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nimiq/signer: key file nonce has the wrong length")
	}
	seed, err := aead.Open(nil, nonce, sealed, []byte(f.Address))
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: cannot decrypt key file: wrong passphrase or corrupted file")
	}

	key, err := NewPrivateKey(seed)
	if err != nil {
		return nil, err
	}
	// The address is authenticated as GCM additional data, so a mismatch here
	// means the file is internally inconsistent rather than tampered with.
	if subtle.ConstantTimeCompare([]byte(key.Address().String()), []byte(f.Address)) != 1 {
		return nil, fmt.Errorf("nimiq/signer: key file address does not match its key")
	}
	return key, nil
}

// LoadEncryptedKeyFile reads and decrypts a key file from disk.
//
// It refuses a file that is readable by group or other. A key encrypted at rest
// but left world-readable only raises the attacker's cost from "read it" to
// "read it and crack one passphrase".
func LoadEncryptedKeyFile(path string, passphrase []byte) (*PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("nimiq/signer: key file %s has permissions %o; it must not be readable by group or other", path, perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: %w", err)
	}
	return DecryptPrivateKey(data, passphrase)
}

// SaveEncryptedKeyFile writes an encrypted key file with owner-only
// permissions, refusing to overwrite an existing file.
func SaveEncryptedKeyFile(path string, seed, passphrase []byte) error {
	data, err := EncryptPrivateKey(seed, passphrase)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("nimiq/signer: %w", err)
	}
	// O_EXCL: silently replacing a key file destroys the only copy of a key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("nimiq/signer: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("nimiq/signer: %w", err)
	}
	return f.Close()
}
