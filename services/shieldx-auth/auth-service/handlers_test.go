package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"shieldx/shared/shieldx-common/pkg/auth"
)

func TestRegisterLoginLogoutFlow(t *testing.T) {
	store := NewMemoryUserStore()
	jwtManager := newTestJWTManager(t)
	mailer := NoopMailer{}

	register := registerHandler(jwtManager, store, mailer)

	payload := map[string]any{
		"email":    "alice@example.com",
		"username": "alice",
		"password": "Sup3rSecret!",
		"roles":    []string{"user"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	register.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	var registerResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("parse register response: %v", err)
	}

	login := loginHandler(jwtManager, nil, store)
	loginBody, _ := json.Marshal(map[string]string{
		"username": "alice",
		"password": "Sup3rSecret!",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	loginRes := httptest.NewRecorder()
	login.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginRes.Code)
	}

	var loginResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(loginRes.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	if loginResp.AccessToken == "" || loginResp.RefreshToken == "" {
		t.Fatalf("tokens not returned: %+v", loginResp)
	}

	claims, err := jwtManager.ValidateToken(context.Background(), loginResp.AccessToken)
	if err != nil {
		t.Fatalf("validate token before logout: %v", err)
	}

	logout := logoutHandler(jwtManager, nil)
	logoutReqBody, _ := json.Marshal(map[string]string{
		"refresh_token": loginResp.RefreshToken,
	})
	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", bytes.NewReader(logoutReqBody))
	ctx := context.WithValue(logoutReq.Context(), auth.ClaimsContextKey, claims)
	logoutReq = logoutReq.WithContext(ctx)
	logoutRes := httptest.NewRecorder()
	logout.ServeHTTP(logoutRes, logoutReq)

	if logoutRes.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d", logoutRes.Code)
	}

	if _, err := jwtManager.ValidateToken(context.Background(), loginResp.AccessToken); !errors.Is(err, auth.ErrTokenRevoked) {
		t.Fatalf("expected revoked access token, got %v", err)
	}

	if _, err := jwtManager.ValidateToken(context.Background(), loginResp.RefreshToken); !errors.Is(err, auth.ErrTokenRevoked) {
		t.Fatalf("expected revoked refresh token, got %v", err)
	}
}

func newTestJWTManager(t *testing.T) *auth.JWTManager {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})

	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	manager, err := auth.NewJWTManager(auth.JWTConfig{
		PrivateKeyPEM: string(privPEM),
		PublicKeyPEM:  string(pubPEM),
	})
	if err != nil {
		t.Fatalf("create jwt manager: %v", err)
	}

	return manager
}
