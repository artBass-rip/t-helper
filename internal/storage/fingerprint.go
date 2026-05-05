package storage

import (
	"crypto/sha256"
	"encoding/hex"
)

func Fingerprint(provider string, parts ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(provider))
	for _, part := range parts {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	sum := hash.Sum(nil)
	return "db:" + hex.EncodeToString(sum)
}

func UnsafeFingerprintForTest(provider, locator string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + locator))
	return "db:" + hex.EncodeToString(sum[:])
}
