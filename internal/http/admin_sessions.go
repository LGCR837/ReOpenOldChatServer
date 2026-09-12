package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type adminSessions struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newAdminSessions() *adminSessions {
	return &adminSessions{sessions: make(map[string]time.Time)}
}

func (s *adminSessions) New(ttl time.Duration) string {
	token := randomToken()
	expires := time.Now().Add(ttl)
	s.mu.Lock()
	s.sessions[token] = expires
	s.mu.Unlock()
	return token
}

func (s *adminSessions) Valid(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if exp.Before(now) {
		delete(s.sessions, token)
		return false
	}
	return true
}

func (s *adminSessions) Delete(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *adminSessions) PruneExpired() {
	now := time.Now()
	s.mu.Lock()
	for token, exp := range s.sessions {
		if exp.Before(now) {
			delete(s.sessions, token)
		}
	}
	s.mu.Unlock()
}

func randomToken() string {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
