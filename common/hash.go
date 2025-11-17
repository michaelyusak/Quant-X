package common

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
)

func HmacHash(str, secret string) string {
	key := []byte(secret)
	h := hmac.New(sha512.New, key)
	h.Write([]byte(str))
	return hex.EncodeToString(h.Sum(nil))
}
