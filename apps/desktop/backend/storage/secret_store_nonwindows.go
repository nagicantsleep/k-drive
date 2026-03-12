//go:build !windows && !darwin

package storage

import "fmt"

func protectData(plaintext []byte) ([]byte, error) {
	return nil, fmt.Errorf("secure secret storage is not available on this platform")
}

func unprotectData(ciphertext []byte) ([]byte, error) {
	return nil, fmt.Errorf("secure secret storage is not available on this platform")
}
