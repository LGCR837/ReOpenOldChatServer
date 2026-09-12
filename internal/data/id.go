package data

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

func NewID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
