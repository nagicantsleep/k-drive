//go:build windows

package storage

import (
	"fmt"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32              = syscall.NewLazyDLL("Crypt32.dll")
	kernel32             = syscall.NewLazyDLL("Kernel32.dll")
	procCryptProtectData = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
	procLocalFree        = kernel32.NewProc("LocalFree")
)

func protectData(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return []byte{}, nil
	}

	inBlob := dataBlob{cbData: uint32(len(plaintext)), pbData: &plaintext[0]}
	var outBlob dataBlob

	ret, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if ret == 0 {
		if callErr == syscall.Errno(0) {
			callErr = fmt.Errorf("CryptProtectData failed")
		}
		return nil, callErr
	}

	defer procLocalFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	ciphertext := make([]byte, outBlob.cbData)
	copy(ciphertext, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	return ciphertext, nil
}

func unprotectData(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return []byte{}, nil
	}

	inBlob := dataBlob{cbData: uint32(len(ciphertext)), pbData: &ciphertext[0]}
	var outBlob dataBlob

	ret, _, callErr := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if ret == 0 {
		if callErr == syscall.Errno(0) {
			callErr = fmt.Errorf("CryptUnprotectData failed")
		}
		return nil, callErr
	}

	defer procLocalFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	plaintext := make([]byte, outBlob.cbData)
	copy(plaintext, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	return plaintext, nil
}
