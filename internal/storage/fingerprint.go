package storage

import (
	"crypto/sha256"
	"encoding/hex"
)

func Fingerprint(provider, locator string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + locator))
	return "db:" + hex.EncodeToString(sum[:])
}
