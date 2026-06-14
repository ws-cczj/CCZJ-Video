package util

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

const defaultKey = "this_is_key_cczj"

func Encrypt(plaintext string) (string, error) {
	return EncryptWithKey(plaintext, defaultKey)
}

func Decrypt(ciphertext string) (string, error) {
	return DecryptWithKey(ciphertext, defaultKey)
}

func EncryptWithKey(plaintext, key string) (string, error) {
	block, err := aes.NewCipher(padKey([]byte(key)))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	plainBytes := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	cipherBytes := make([]byte, aes.BlockSize+len(plainBytes))
	iv := cipherBytes[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(cipherBytes[aes.BlockSize:], plainBytes)
	return base64.StdEncoding.EncodeToString(cipherBytes), nil
}

func DecryptWithKey(ciphertext, key string) (string, error) {
	cipherBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	block, err := aes.NewCipher(padKey([]byte(key)))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	if len(cipherBytes) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	iv := cipherBytes[:aes.BlockSize]
	cipherBytes = cipherBytes[aes.BlockSize:]
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(cipherBytes, cipherBytes)
	plainBytes, err := pkcs7Unpad(cipherBytes)
	if err != nil {
		return "", fmt.Errorf("unpad: %w", err)
	}
	return string(plainBytes), nil
}

func padKey(key []byte) []byte {
	const keyLen = 32
	if len(key) >= keyLen {
		return key[:keyLen]
	}
	padded := make([]byte, keyLen)
	copy(padded, key)
	return padded
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - len(data)%blockSize
	pad := bytes.Repeat([]byte{byte(padLen)}, padLen)
	return append(data, pad...)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen > len(data) || padLen > aes.BlockSize {
		return nil, fmt.Errorf("invalid padding")
	}
	return data[:len(data)-padLen], nil
}
