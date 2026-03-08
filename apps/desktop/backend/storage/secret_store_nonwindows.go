//go:build !windows

package storage

import "fmt"

func protectData(plaintext []byte) ([]byte, error) {
	return nil, fmt.Errorf("dpapi secret protection is only available on windows")
}

func unprotectData(ciphertext []byte) ([]byte, error) {
	return nil, fmt.Errorf("dpapi secret unprotection is only available on windows")
}
