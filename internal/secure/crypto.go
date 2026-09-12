package secure

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

const passphrase = "oldchat.v1.local"

type Envelope struct {
	IV   string `json:"iv"`
	Data string `json:"data"`
	MAC  string `json:"mac"`
}

func Encrypt(plain []byte) ([]byte, error) {
	key := deriveKey(passphrase)
	macKey := deriveKey(passphrase + "_mac")
	return EncryptWithKeys(plain, key, macKey)
}

func EncryptWithKeys(plain, encKey, macKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	mac := hmacSHA256(macKey, append(iv, ciphertext...))
	env := Envelope{
		IV:   base64.StdEncoding.EncodeToString(iv),
		Data: base64.StdEncoding.EncodeToString(ciphertext),
		MAC:  base64.StdEncoding.EncodeToString(mac),
	}
	return json.Marshal(env)
}

func Decrypt(payload []byte) ([]byte, error) {
	key := deriveKey(passphrase)
	macKey := deriveKey(passphrase + "_mac")
	return DecryptWithKeys(payload, key, macKey)
}

func DecryptWithKeys(payload, encKey, macKey []byte) ([]byte, error) {
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}
	if env.IV == "" || env.Data == "" || env.MAC == "" {
		return nil, errors.New("missing fields")
	}
	iv, err := base64.StdEncoding.DecodeString(env.IV)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, err
	}
	macRaw, err := base64.StdEncoding.DecodeString(env.MAC)
	if err != nil {
		return nil, err
	}
	expected := hmacSHA256(macKey, append(iv, ciphertext...))
	if !hmac.Equal(macRaw, expected) {
		return nil, errors.New("bad mac")
	}

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("invalid length")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	return pkcs7Unpad(plain, aes.BlockSize)
}

func deriveKey(input string) []byte {
	sum := sha256.Sum256([]byte(input))
	return sum[:]
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - (len(data) % blockSize)
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid padding")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, errors.New("invalid padding")
	}
	for i := 0; i < pad; i++ {
		if data[len(data)-1-i] != byte(pad) {
			return nil, errors.New("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

func DeriveSessionKeys(secret []byte) (encKey, macKey []byte) {
	encKey = deriveKeyBytes(secret, []byte("enc"))
	macKey = deriveKeyBytes(secret, []byte("mac"))
	return encKey, macKey
}

func deriveKeyBytes(secret, suffix []byte) []byte {
	buf := make([]byte, 0, len(secret)+len(suffix))
	buf = append(buf, secret...)
	buf = append(buf, suffix...)
	sum := sha256.Sum256(buf)
	return sum[:]
}
