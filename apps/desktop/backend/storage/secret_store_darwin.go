//go:build darwin

package storage

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

const keychainService = "com.kdrive.secrets"

// protectData stores the plaintext in the macOS Keychain and returns a
// key-reference that can be used to retrieve it later. The returned bytes are
// a base64-encoded Keychain account label (not the secret itself).
func protectData(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return []byte{}, nil
	}

	// Use a content-addressable account name so the same plaintext always
	// maps to the same Keychain item. The account name is a base64 encoding
	// of the plaintext — the actual secret is stored by the Keychain.
	account := base64.StdEncoding.EncodeToString(plaintext)
	encoded := base64.StdEncoding.EncodeToString(plaintext)

	// Delete any previous entry (ignore errors — it may not exist).
	_ = exec.Command("security", "delete-generic-password",
		"-s", keychainService,
		"-a", account,
	).Run()

	cmd := exec.Command("security", "add-generic-password",
		"-s", keychainService,
		"-a", account,
		"-w", encoded,
		"-U", // update if exists
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("keychain add-generic-password failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return []byte(account), nil
}

// unprotectData retrieves the secret from the macOS Keychain using the
// key-reference returned by protectData.
func unprotectData(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return []byte{}, nil
	}

	account := string(ciphertext)

	cmd := exec.Command("security", "find-generic-password",
		"-s", keychainService,
		"-a", account,
		"-w", // output password only
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("keychain find-generic-password failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, fmt.Errorf("keychain secret decode failed: %w", err)
	}

	return decoded, nil
}
