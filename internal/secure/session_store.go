package secure

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type SessionKeys struct {
	EncKey    []byte
	MacKey    []byte
	CreatedAt time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionKeys
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]SessionKeys)}
}

func (s *SessionStore) Create(encKey, macKey []byte) string {
	id := newSessionID()
	s.mu.Lock()
	s.sessions[id] = SessionKeys{
		EncKey:    append([]byte(nil), encKey...),
		MacKey:    append([]byte(nil), macKey...),
		CreatedAt: time.Now(),
	}
	s.mu.Unlock()
	return id
}

func (s *SessionStore) Get(id string) (SessionKeys, bool) {
	s.mu.RLock()
	val, ok := s.sessions[id]
	s.mu.RUnlock()
	return val, ok
}

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func newSessionID() string {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (s *SessionStore) PruneOlderThan(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	s.mu.Lock()
	for id, sess := range s.sessions {
		if sess.CreatedAt.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}
