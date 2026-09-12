package httpapi

import (
	"bytes"
	"io"
	"net/http"

	"metrochat/internal/secure"
)

const encHeader = "X-Enc"
const sessionHeader = "X-Session"

type bufferWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func newBufferWriter() *bufferWriter {
	return &bufferWriter{header: make(http.Header)}
}

func (w *bufferWriter) Header() http.Header {
	return w.header
}

func (w *bufferWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *bufferWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (a *API) secureMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(encHeader) != "1" {
			next.ServeHTTP(w, r)
			return
		}

		sessionID := r.Header.Get(sessionHeader)
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "missing_session", "missing session")
			return
		}
		keys, ok := a.sessions.Get(sessionID)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_session", "invalid session")
			return
		}

		if r.ContentLength > 0 {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_body", "invalid body")
				return
			}
			_ = r.Body.Close()
			plain, err := secure.DecryptWithKeys(raw, keys.EncKey, keys.MacKey)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_encryption", "invalid payload")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(plain))
			r.ContentLength = int64(len(plain))
			r.Header.Set("Content-Type", "application/json")
		}

		rec := newBufferWriter()
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		enc, err := secure.EncryptWithKeys(rec.buf.Bytes(), keys.EncKey, keys.MacKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encrypt_failed", "internal error")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(encHeader, "1")
		w.Header().Set(sessionHeader, sessionID)
		w.WriteHeader(status)
		_, _ = w.Write(enc)
	})
}
