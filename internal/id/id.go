package id

import (
	"crypto/rand"
	"encoding/hex"
)

func New() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func MustNew() string {
	value, err := New()
	if err != nil {
		panic(err)
	}
	return value
}
