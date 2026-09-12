package verify

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

type CaptchaStore struct {
	mu      sync.RWMutex
	entries map[string]captchaEntry
}

type captchaEntry struct {
	Code      string
	ExpiresAt time.Time
}

type EmailCodeStore struct {
	mu      sync.RWMutex
	entries map[string]emailEntry
}

type emailEntry struct {
	Code      string
	ExpiresAt time.Time
}

type SendLimiter struct {
	mu       sync.Mutex
	lastSent map[string]time.Time
}

func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{entries: make(map[string]captchaEntry)}
}

func NewEmailCodeStore() *EmailCodeStore {
	return &EmailCodeStore{entries: make(map[string]emailEntry)}
}

func NewSendLimiter() *SendLimiter {
	return &SendLimiter{lastSent: make(map[string]time.Time)}
}

func (s *CaptchaStore) New(code string, ttl time.Duration) string {
	if s == nil {
		return ""
	}
	id := newID()
	s.mu.Lock()
	s.entries[id] = captchaEntry{
		Code:      normalize(code),
		ExpiresAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()
	return id
}

func (s *CaptchaStore) Verify(id, code string) bool {
	if s == nil || id == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return false
	}
	if entry.ExpiresAt.Before(now) {
		delete(s.entries, id)
		return false
	}
	if entry.Code != normalize(code) {
		return false
	}
	delete(s.entries, id)
	return true
}

func (s *CaptchaStore) PruneExpired() {
	if s == nil {
		return
	}
	cutoff := time.Now()
	s.mu.Lock()
	for id, entry := range s.entries {
		if entry.ExpiresAt.Before(cutoff) {
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()
}

func (s *EmailCodeStore) Set(email, code string, ttl time.Duration) {
	if s == nil || email == "" {
		return
	}
	s.mu.Lock()
	s.entries[strings.ToLower(email)] = emailEntry{
		Code:      normalize(code),
		ExpiresAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()
}

func (s *EmailCodeStore) Verify(email, code string) bool {
	if s == nil || email == "" {
		return false
	}
	key := strings.ToLower(email)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return false
	}
	if entry.ExpiresAt.Before(now) {
		delete(s.entries, key)
		return false
	}
	if entry.Code != normalize(code) {
		return false
	}
	delete(s.entries, key)
	return true
}

func (s *EmailCodeStore) PruneExpired() {
	if s == nil {
		return
	}
	cutoff := time.Now()
	s.mu.Lock()
	for key, entry := range s.entries {
		if entry.ExpiresAt.Before(cutoff) {
			delete(s.entries, key)
		}
	}
	s.mu.Unlock()
}

func (s *SendLimiter) Allow(email string, minInterval time.Duration) (bool, time.Duration) {
	if s == nil || email == "" {
		return false, minInterval
	}
	key := strings.ToLower(email)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastSent[key]
	if ok {
		elapsed := now.Sub(last)
		if elapsed < minInterval {
			return false, minInterval - elapsed
		}
	}
	s.lastSent[key] = now
	return true, 0
}

func (s *SendLimiter) PruneOlderThan(maxAge time.Duration) {
	if s == nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	s.mu.Lock()
	for key, ts := range s.lastSent {
		if ts.Before(cutoff) {
			delete(s.lastSent, key)
		}
	}
	s.mu.Unlock()
}

func normalize(code string) string {
	return strings.TrimSpace(strings.ToUpper(code))
}

func newID() string {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
