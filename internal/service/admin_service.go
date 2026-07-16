package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type AdminService interface {
	ValidatePassword(password string) bool
	CreateSession() string
	ValidateSession(token string) bool
}

type adminSession struct {
	token     string
	expiresAt time.Time
}

type adminService struct {
	password string
	mu       sync.RWMutex
	sessions map[string]adminSession
}

func NewAdminService(password string) AdminService {
	return &adminService{
		password: password,
		sessions: make(map[string]adminSession),
	}
}

func (s *adminService) ValidatePassword(password string) bool {
	return s.password == password
}

func (s *adminService) CreateSession() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)

	s.mu.Lock()
	s.sessions[token] = adminSession{
		token:     token,
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	s.mu.Unlock()

	go s.cleanup()

	return token
}

func (s *adminService) ValidateSession(token string) bool {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return false
	}

	if time.Now().After(sess.expiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}

	return true
}

func (s *adminService) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, token)
		}
	}
}
