package signer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testSeed = bytes.Repeat([]byte{0x42}, 32)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	pass := []byte("correct horse battery staple")
	file, err := EncryptPrivateKey(testSeed, pass)
	if err != nil {
		t.Fatal(err)
	}
	key, err := DecryptPrivateKey(file, pass)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := NewPrivateKey(testSeed)
	if key.Address() != want.Address() {
		t.Errorf("address = %s, want %s", key.Address(), want.Address())
	}
}

// TestCiphertextContainsNoPlaintext is the basic property: the seed must not
// appear anywhere in the file, in raw or hex form.
func TestCiphertextContainsNoPlaintext(t *testing.T) {
	file, err := EncryptPrivateKey(testSeed, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file, testSeed) {
		t.Error("the raw seed appears in the encrypted file")
	}
	if bytes.Contains(bytes.ToLower(file), []byte("4242424242424242")) {
		t.Error("the hex-encoded seed appears in the encrypted file")
	}
}

func TestWrongPassphraseFails(t *testing.T) {
	file, err := EncryptPrivateKey(testSeed, []byte("right"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecryptPrivateKey(file, []byte("wrong"))
	if err == nil {
		t.Fatal("a wrong passphrase must not decrypt")
	}
	// The message must not distinguish a wrong passphrase from corruption, and
	// must not echo the passphrase.
	if strings.Contains(err.Error(), "wrong") && strings.Contains(err.Error(), "right") {
		t.Errorf("error leaks passphrase material: %v", err)
	}
}

// TestTamperingIsDetected: GCM authenticates the ciphertext, and the address is
// bound as additional data, so editing either is caught.
func TestTamperingIsDetected(t *testing.T) {
	pass := []byte("pw")
	file, err := EncryptPrivateKey(testSeed, pass)
	if err != nil {
		t.Fatal(err)
	}

	var f encryptedKeyFile
	if err := json.Unmarshal(file, &f); err != nil {
		t.Fatal(err)
	}

	t.Run("edited address", func(t *testing.T) {
		bad := f
		bad.Address = "NQ34 248H 248H 248H 248H 248H 248H 248H 248H"
		data, _ := json.Marshal(bad)
		if _, err := DecryptPrivateKey(data, pass); err == nil {
			t.Error("an edited address must be detected")
		}
	})

	t.Run("flipped ciphertext byte", func(t *testing.T) {
		raw := []byte(f.Data)
		raw[0] ^= 0x01
		bad := f
		bad.Data = string(raw)
		data, _ := json.Marshal(bad)
		if _, err := DecryptPrivateKey(data, pass); err == nil {
			t.Error("modified ciphertext must be detected")
		}
	})
}

func TestRejectsEmptyPassphrase(t *testing.T) {
	if _, err := EncryptPrivateKey(testSeed, nil); err == nil {
		t.Fatal("an empty passphrase must be refused")
	}
}

// TestRejectsImplausibleScryptParameters stops a hostile file from making the
// loader allocate gigabytes during key derivation.
func TestRejectsImplausibleScryptParameters(t *testing.T) {
	file, _ := EncryptPrivateKey(testSeed, []byte("pw"))
	var f encryptedKeyFile
	_ = json.Unmarshal(file, &f)
	f.N = 1 << 30
	data, _ := json.Marshal(f)
	if _, err := DecryptPrivateKey(data, []byte("pw")); err == nil {
		t.Fatal("an absurd work factor must be rejected before deriving")
	}
}

func TestSaveAndLoadKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys", "payout.json")
	pass := []byte("pw")

	if err := SaveEncryptedKeyFile(path, testSeed, pass); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}

	key, err := LoadEncryptedKeyFile(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := NewPrivateKey(testSeed)
	if key.Address() != want.Address() {
		t.Error("loaded the wrong key")
	}
}

// TestSaveRefusesOverwrite: silently replacing a key file destroys the only
// copy of a key.
func TestSaveRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.json")
	if err := SaveEncryptedKeyFile(path, testSeed, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := SaveEncryptedKeyFile(path, testSeed, []byte("pw")); err == nil {
		t.Fatal("overwriting an existing key file must be refused")
	}
}

// TestLoadRefusesLoosePermissions: encryption at rest is undermined by a
// world-readable file.
func TestLoadRefusesLoosePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.json")
	if err := SaveEncryptedKeyFile(path, testSeed, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadEncryptedKeyFile(path, []byte("pw"))
	if err == nil {
		t.Fatal("a group/other-readable key file must be refused")
	}
	if !strings.Contains(err.Error(), "permissions") {
		t.Errorf("error should explain the permissions problem, got: %v", err)
	}
}

// TestStringDoesNotLeakKey guards the accidental-logging case.
func TestStringDoesNotLeakKey(t *testing.T) {
	key, err := NewPrivateKey(testSeed)
	if err != nil {
		t.Fatal(err)
	}
	s := key.String()
	if strings.Contains(strings.ToLower(s), "4242") {
		t.Errorf("String() leaks key material: %s", s)
	}
	if !strings.Contains(s, key.Address().String()) {
		t.Error("String() should identify the key by address")
	}
}
