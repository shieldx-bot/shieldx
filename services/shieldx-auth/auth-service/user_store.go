package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserExists         = errors.New("user already exists")
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type StoredUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash []byte    `json:"-"`
	Roles        []string  `json:"roles"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login,omitempty"`
}

type MemoryUserStore struct {
	mu              sync.RWMutex
	usersByUsername map[string]*StoredUser
	usersByEmail    map[string]*StoredUser
}

func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		usersByUsername: make(map[string]*StoredUser),
		usersByEmail:    make(map[string]*StoredUser),
	}
}

func (s *MemoryUserStore) CreateUser(username, email string, passwordHash []byte, roles []string) (*StoredUser, error) {
	usernameKey := strings.ToLower(strings.TrimSpace(username))
	emailKey := strings.ToLower(strings.TrimSpace(email))

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usersByUsername[usernameKey]; exists {
		return nil, ErrUserExists
	}

	if _, exists := s.usersByEmail[emailKey]; exists {
		return nil, ErrEmailExists
	}

	now := time.Now().UTC()
	user := &StoredUser{
		ID:           fmt.Sprintf("user-%d", now.UnixNano()),
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Roles:        append([]string(nil), roles...),
		CreatedAt:    now,
		LastLoginAt:  time.Time{},
	}

	s.usersByUsername[usernameKey] = user
	s.usersByEmail[emailKey] = user

	return copyUser(user), nil
}

func (s *MemoryUserStore) Authenticate(username, password string) (*StoredUser, error) {
	usernameKey := strings.ToLower(strings.TrimSpace(username))

	s.mu.RLock()
	user, ok := s.usersByUsername[usernameKey]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	s.mu.Lock()
	user.LastLoginAt = time.Now().UTC()
	s.mu.Unlock()

	return copyUser(user), nil
}

func (s *MemoryUserStore) GetByUsername(username string) (*StoredUser, error) {
	usernameKey := strings.ToLower(strings.TrimSpace(username))

	s.mu.RLock()
	user, ok := s.usersByUsername[usernameKey]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrInvalidCredentials
	}

	return copyUser(user), nil
}

func (s *MemoryUserStore) ListUsers() []StoredUser {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]StoredUser, 0, len(s.usersByUsername))
	seen := make(map[string]struct{})
	for _, user := range s.usersByUsername {
		if _, ok := seen[user.ID]; ok {
			continue
		}
		seen[user.ID] = struct{}{}
		sanitized := copyUser(user)
		sanitized.PasswordHash = nil
		result = append(result, *sanitized)
	}
	return result
}

func (s *MemoryUserStore) HasRole(role string) bool {
	needle := strings.ToLower(strings.TrimSpace(role))
	if needle == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.usersByUsername {
		for _, r := range user.Roles {
			if strings.EqualFold(r, needle) {
				return true
			}
		}
	}

	return false
}

func copyUser(src *StoredUser) *StoredUser {
	if src == nil {
		return nil
	}
	clone := *src
	clone.Roles = append([]string(nil), src.Roles...)
	if src.PasswordHash != nil {
		clone.PasswordHash = append([]byte(nil), src.PasswordHash...)
	}
	return &clone
}
