package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"

	"metrochat/internal/secure"
)

type handshakeRequest struct {
	ClientPub string `json:"client_pub"`
}

type handshakeResponse struct {
	SessionID string `json:"session_id"`
	ServerPub string `json:"server_pub"`
}

func (a *API) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req handshakeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	clientPub, err := parseClientPublicKey(req.ClientPub)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_key", "invalid public key")
		return
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_failed", "internal error")
		return
	}

	secret, err := deriveSharedSecret(priv, clientPub)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_key", "invalid public key")
		return
	}

	encKey, macKey := secure.DeriveSessionKeys(secret)
	sessionID := a.sessions.Create(encKey, macKey)

	serverPubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_failed", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, handshakeResponse{
		SessionID: sessionID,
		ServerPub: base64.StdEncoding.EncodeToString(serverPubBytes),
	})
}

func parseClientPublicKey(raw string) (*ecdsa.PublicKey, error) {
	if raw == "" {
		return nil, errors.New("missing key")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	pubAny, err := x509.ParsePKIXPublicKey(decoded)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("wrong type")
	}
	if pub.Curve != elliptic.P256() {
		return nil, errors.New("wrong curve")
	}
	return pub, nil
}

func deriveSharedSecret(priv *ecdsa.PrivateKey, pub *ecdsa.PublicKey) ([]byte, error) {
	if priv == nil || pub == nil {
		return nil, errors.New("missing key")
	}
	if priv.Curve != pub.Curve {
		return nil, errors.New("curve mismatch")
	}
	x, _ := priv.Curve.ScalarMult(pub.X, pub.Y, priv.D.Bytes())
	if x == nil {
		return nil, errors.New("invalid secret")
	}
	size := (priv.Curve.Params().BitSize + 7) / 8
	secret := make([]byte, size)
	raw := x.Bytes()
	copy(secret[size-len(raw):], raw)
	return secret, nil
}
