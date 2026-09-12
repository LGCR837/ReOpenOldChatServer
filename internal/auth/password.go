package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"strconv"

	"golang.org/x/crypto/argon2"
)

type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

var DefaultParams = Params{
	Memory:  64 * 1024,
	Time:    2,
	Threads: 2,
	SaltLen: 16,
	KeyLen:  32,
}

var rehashOnLogin = getEnvBool("ARGON2_REHASH_ON_LOGIN", false)

func init() {
	DefaultParams = loadParamsFromEnv(DefaultParams)
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, DefaultParams.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, DefaultParams.Time, DefaultParams.Memory, DefaultParams.Threads, DefaultParams.KeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		DefaultParams.Memory, DefaultParams.Time, DefaultParams.Threads, b64Salt, b64Hash)

	return encoded, nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, hash, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	otherHash := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

func NeedsRehash(encoded string) bool {
	p, _, _, err := decodeHash(encoded)
	if err != nil {
		return false
	}
	if p.Memory != DefaultParams.Memory {
		return true
	}
	if p.Time != DefaultParams.Time {
		return true
	}
	if p.Threads != DefaultParams.Threads {
		return true
	}
	if p.KeyLen != DefaultParams.KeyLen {
		return true
	}
	return false
}

func RehashEnabled() bool {
	return rehashOnLogin
}

func decodeHash(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return Params{}, nil, nil, errors.New("invalid hash format")
	}
	if parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("invalid hash type")
	}
	if parts[2] != "v=19" {
		return Params{}, nil, nil, errors.New("invalid hash version")
	}

	var p Params
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads)
	if err != nil {
		return Params{}, nil, nil, errors.New("invalid hash params")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, errors.New("invalid salt")
	}
	p.SaltLen = uint32(len(salt))

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, errors.New("invalid hash")
	}
	p.KeyLen = uint32(len(hash))

	return p, salt, hash, nil
}

func loadParamsFromEnv(p Params) Params {
	if v := getEnvInt("ARGON2_MEMORY_MB"); v > 0 {
		p.Memory = uint32(v) * 1024
	}
	if v := getEnvInt("ARGON2_MEMORY_KB"); v > 0 {
		p.Memory = uint32(v)
	}
	if v := getEnvInt("ARGON2_TIME"); v > 0 {
		p.Time = uint32(v)
	}
	if v := getEnvInt("ARGON2_THREADS"); v > 0 {
		if v > 255 {
			v = 255
		}
		p.Threads = uint8(v)
	}
	if v := getEnvInt("ARGON2_SALT_LEN"); v > 0 {
		p.SaltLen = uint32(v)
	}
	if v := getEnvInt("ARGON2_KEY_LEN"); v > 0 {
		p.KeyLen = uint32(v)
	}
	return p
}

func getEnvInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return 0
	}
	return val
}

func getEnvBool(key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def
	}
	if raw == "1" || raw == "true" || raw == "yes" || raw == "on" {
		return true
	}
	if raw == "0" || raw == "false" || raw == "no" || raw == "off" {
		return false
	}
	return def
}
