package runtime

import (
	"crypto/rand"
	"encoding/hex"
)

func NewInstanceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "runtime_" + hex.EncodeToString(b[:]), nil
}
