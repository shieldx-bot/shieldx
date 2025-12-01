package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"shieldx/shared/shieldx-common/pkg/auth"
	"shieldx/shared/shieldx-common/pkg/metrics"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type Mailer interface {
	Send(to, subject, body string) error
}

type NoopMailer struct{}

func (NoopMailer) Send(to, subject, body string) error {
	if to == "" {
		return nil
	}
	log.Printf("Mailer disabled: skipping send to %s", to)
	return nil
}

var allowedRoles = map[string]struct{}{
	"admin":   {},
	"analyst": {},
	"user":    {},
}

var defaultRolePermissions = map[string][]string{
	"admin": {
		"api:read",
		"api:write",
		"dashboard:read",
		"users:read",
		"users:write",
	},
	"analyst": {
		"api:read",
		"dashboard:read",
	},
	"user": {
		"dashboard:read",
	},
}

func main() {
	// Load configuration from environment
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	port := getEnv("PORT", "8080")

	// Generate keys for demo (in production, load from secure storage)
	privateKey, publicKey, err := auth.LoadKeysFromEnv()
	if err != nil {
		log.Fatal("Failed to load keys:", err)
	}

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})

	// Test Redis connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Redis not available, using in-memory store: %v", err)
	}

	// Initialize JWT Manager
	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		PrivateKeyPEM:   privateKey,
		PublicKeyPEM:    publicKey,
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "shieldx-auth-service",
		RevokedTokenStore: auth.NewRedisRevokedStore(auth.RedisConfig{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       0,
		}),
	})
	if err != nil {
		log.Fatal("Failed to create JWT manager:", err)
	}

	// Initialize Session Manager
	sessionManager, err := auth.NewSessionManager(auth.SessionConfig{
		RedisAddr:     redisAddr,
		RedisPassword: redisPassword,
		RedisDB:       0,
		SessionTTL:    24 * time.Hour,
	})
	if err != nil {
		log.Printf("Warning: Session manager not available: %v", err)
	}

	// Initialize RBAC Engine
	rbacEngine, err := auth.NewRBACEngine(auth.RBACConfig{
		Roles: auth.GetDefaultRoles(),
	})
	if err != nil {
		log.Fatal("Failed to create RBAC engine:", err)
	}

	storePath := strings.TrimSpace(getEnv("AUTH_USER_STORE_PATH", ""))
	var userStore UserStore
	if storePath != "" {
		persistentStore, err := NewPersistentUserStore(storePath)
		if err != nil {
			log.Fatalf("Failed to initialize persistent user store: %v", err)
		}
		userStore = persistentStore
		log.Printf("💾 Persistent user store enabled (%s)", storePath)
	} else {
		userStore = NewMemoryUserStore()
		log.Printf("💾 Using in-memory user store")
	}

	mailer := NoopMailer{}
	bootstrapEmail := getEnv("AUTH_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	bootstrapPassword := getEnv("AUTH_BOOTSTRAP_ADMIN_PASSWORD", "admin123!")
	if bootstrapPassword != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(bootstrapPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			log.Printf("Warning: unable to hash bootstrap admin password: %v", hashErr)
		} else {
			if _, createErr := userStore.CreateUser("admin", bootstrapEmail, hash, []string{"admin"}); createErr != nil {
				if !errors.Is(createErr, ErrUserExists) && !errors.Is(createErr, ErrEmailExists) {
					log.Printf("Warning: failed to create bootstrap admin: %v", createErr)
				}
			} else {
				log.Printf("👤 Bootstrap admin account ready (email: %s)", bootstrapEmail)
			}
		}
	}

	// Initialize OAuth2 Provider
	oauth2Provider, err := auth.NewOAuth2Provider(auth.OAuth2Config{
		Issuer:         "https://auth.shieldx.cloud",
		RedisClient:    redisClient,
		JWTManager:     jwtManager,
		SessionManager: sessionManager,
		Clients: []*auth.OAuth2Client{
			{
				ClientID:     "shieldx-web-app",
				ClientSecret: "demo-secret-change-in-production",
				RedirectURIs: []string{
					"http://localhost:3000/callback",
					"https://console.shieldx.cloud/callback",
				},
				GrantTypes: []string{"authorization_code", "refresh_token"},
				Scopes:     []string{"openid", "profile", "email", "api"},
				Name:       "ShieldX Web Console",
			},
			{
				ClientID:     "shieldx-mobile-app",
				ClientSecret: "mobile-secret-change-in-production",
				RedirectURIs: []string{
					"shieldx://callback",
				},
				GrantTypes: []string{"authorization_code", "refresh_token"},
				Scopes:     []string{"openid", "profile", "email", "api"},
				Name:       "ShieldX Mobile App",
			},
		},
	})
	if err != nil {
		log.Fatal("Failed to create OAuth2 provider:", err)
	}

	// Create auth middleware
	authMiddleware := auth.NewAuthMiddleware(auth.MiddlewareConfig{
		JWTManager:     jwtManager,
		SessionManager: sessionManager,
		RBACEngine:     rbacEngine,
		BypassPaths: []string{
			"/health",
			"/metrics",
			"/auth/login",
			"/auth/register",
			"/oauth2/authorize",
			"/oauth2/token",
		},
	})

	// Metrics registry and HTTP metrics middleware
	reg := metrics.NewRegistry()
	httpMetrics := metrics.NewHTTPMetrics(reg, "auth_service")

	// Setup routes
	mux := http.NewServeMux()
	mux.Handle("/metrics", reg)

	// Public endpoints
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/auth/login", loginHandler(jwtManager, sessionManager, userStore))
	mux.HandleFunc("/auth/register", registerHandler(jwtManager, userStore, mailer))
	mux.HandleFunc("/auth/refresh", refreshHandler(jwtManager))

	// OAuth2 endpoints
	mux.HandleFunc("/oauth2/authorize", oauth2Provider.HandleAuthorize)
	mux.HandleFunc("/oauth2/token", oauth2Provider.HandleToken)

	// Protected endpoints
	mux.Handle("/auth/logout", authMiddleware.Authenticate(http.HandlerFunc(logoutHandler(jwtManager, sessionManager))))
	mux.Handle("/api/profile", authMiddleware.Authenticate(http.HandlerFunc(profileHandler)))
	mux.Handle("/api/users", authMiddleware.Authenticate(
		authMiddleware.RequireRole("admin")(usersHandler(userStore)),
	))
	mux.Handle("/api/data", authMiddleware.Authenticate(
		authMiddleware.RequirePermission("api", "read")(http.HandlerFunc(dataHandler)),
	))

	// Admin endpoints
	mux.Handle("/admin/roles", authMiddleware.Authenticate(
		authMiddleware.RequireRole("admin")(http.HandlerFunc(rolesHandler(rbacEngine))),
	))

	log.Printf("🔐 ShieldX Auth Service starting on port %s", port)
	log.Printf("📝 JWT Manager initialized (RS256)")
	log.Printf("💾 Session Manager: %s", redisAddr)
	log.Printf("🔒 RBAC Engine loaded with %d default roles", len(auth.GetDefaultRoles()))
	log.Printf("🌐 OAuth2 Provider initialized with %d clients", 2)

	h := httpMetrics.Middleware(mux)
	if err := http.ListenAndServe(":"+port, h); err != nil {
		log.Fatal("Server failed:", err)
	}
}

// Handler functions
func healthHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"service":   "shieldx-auth",
		"timestamp": time.Now().Unix(),
	})
}

func loginHandler(jwtManager *auth.JWTManager, sessionManager *auth.SessionManager, users UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(req.Username)
		if username == "" || req.Password == "" {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		user, err := users.Authenticate(username, req.Password)
		if err != nil {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Create session in Redis when available
		var session *auth.SessionData
		if sessionManager != nil {
			session, err = sessionManager.CreateSession(
				r.Context(),
				user.ID,
				"tenant-default",
				user.Email,
				r.RemoteAddr,
				r.UserAgent(),
			)
			if err != nil {
				log.Printf("Failed to create session: %v", err)
			}
		}

		roles := user.Roles
		if len(roles) == 0 {
			roles = []string{"user"}
		}

		tokenPair, err := jwtManager.GenerateTokenPair(
			r.Context(),
			user.ID,
			"tenant-default",
			user.Email,
			roles,
			permissionsForRoles(roles),
		)
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"access_token":  tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"token_type":    "Bearer",
			"expires_in":    900, // 15 minutes
		}

		if session != nil {
			response["session_id"] = session.SessionID
		}

		response["user"] = map[string]interface{}{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"roles":      roles,
			"last_login": user.LastLoginAt,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func registerHandler(jwtManager *auth.JWTManager, users UserStore, mailer Mailer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string   `json:"username"`
			Email    string   `json:"email"`
			Password string   `json:"password"`
			Roles    []string `json:"roles"`
			OrgName  string   `json:"org_name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		email := strings.TrimSpace(req.Email)
		if email == "" {
			http.Error(w, "Email is required", http.StatusBadRequest)
			return
		}

		if _, err := mail.ParseAddress(email); err != nil {
			http.Error(w, "Invalid email address", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(req.Username)
		if username == "" {
			if inferred := inferUsername(email); inferred != "" {
				username = inferred
			}
		}
		if username == "" {
			http.Error(w, "Username is required", http.StatusBadRequest)
			return
		}

		if err := validatePassword(req.Password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		roles := sanitizeRoles(req.Roles)
		if len(roles) == 0 {
			roles = []string{"user"}
		}

		if containsRole(roles, "admin") && users.HasRole("admin") {
			http.Error(w, "Admin account already exists", http.StatusForbidden)
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Failed to process credentials", http.StatusInternalServerError)
			return
		}

		storedUser, err := users.CreateUser(username, email, hash, roles)
		if err != nil {
			switch {
			case errors.Is(err, ErrUserExists), errors.Is(err, ErrEmailExists):
				http.Error(w, "User already exists", http.StatusConflict)
			default:
				log.Printf("Failed to create user: %v", err)
				http.Error(w, "Failed to create user", http.StatusInternalServerError)
			}
			return
		}

		tokenPair, err := jwtManager.GenerateTokenPair(
			r.Context(),
			storedUser.ID,
			"tenant-default",
			storedUser.Email,
			roles,
			permissionsForRoles(roles),
		)
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}

		if mailer != nil {
			if err := mailer.Send(
				storedUser.Email,
				"Welcome to ShieldX",
				fmt.Sprintf("Hi %s, your ShieldX account is ready.", storedUser.Username),
			); err != nil {
				log.Printf("Mailer send failed: %v", err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Registration successful",
			"user": map[string]interface{}{
				"id":         storedUser.ID,
				"username":   storedUser.Username,
				"email":      storedUser.Email,
				"roles":      storedUser.Roles,
				"created_at": storedUser.CreatedAt,
			},
			"access_token":  tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"token_type":    "Bearer",
			"expires_in":    900,
		})
	}
}

func refreshHandler(jwtManager *auth.JWTManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RefreshToken string `json:"refresh_token"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		tokenPair, err := jwtManager.RefreshToken(r.Context(), req.RefreshToken)
		if err != nil {
			http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"token_type":    "Bearer",
			"expires_in":    900,
		})
	}
}

func logoutHandler(jwtManager *auth.JWTManager, sessionManager *auth.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		claims := auth.GetClaimsFromContext(r.Context())
		if claims == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		ctx := r.Context()
		response := map[string]interface{}{
			"message": "Logged out",
			"revoked": false,
		}

		if claims.ID != "" && claims.ExpiresAt != nil {
			if err := jwtManager.RevokeToken(ctx, claims.ID, claims.ExpiresAt.Time); err != nil {
				log.Printf("Failed to revoke access token: %v", err)
			} else {
				response["revoked"] = true
			}
		}

		if claims.SessionID != "" {
			if err := jwtManager.RevokeSession(ctx, claims.SessionID); err != nil {
				log.Printf("Failed to revoke session: %v", err)
			}
			if sessionManager != nil {
				if err := sessionManager.DeleteSession(ctx, claims.SessionID); err != nil {
					log.Printf("Failed to delete session: %v", err)
				}
			}
			response["session_revoked"] = true
		}

		if refresh := strings.TrimSpace(req.RefreshToken); refresh != "" {
			refreshClaims, err := jwtManager.ValidateToken(ctx, refresh)
			if err != nil {
				log.Printf("Refresh token validation failed: %v", err)
			} else if refreshClaims.TokenType == "refresh" {
				var expiry time.Time
				if refreshClaims.ExpiresAt != nil {
					expiry = refreshClaims.ExpiresAt.Time
				} else {
					expiry = time.Now().Add(7 * 24 * time.Hour)
				}
				if err := jwtManager.RevokeToken(ctx, refreshClaims.ID, expiry); err != nil {
					log.Printf("Failed to revoke refresh token: %v", err)
				} else {
					response["refresh_revoked"] = true
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func profileHandler(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":     claims.UserID,
		"tenant_id":   claims.TenantID,
		"email":       claims.Email,
		"roles":       claims.Roles,
		"permissions": claims.Permissions,
		"session_id":  claims.SessionID,
	})
}

func usersHandler(store UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users := store.ListUsers()
		sort.Slice(users, func(i, j int) bool {
			return users[i].CreatedAt.Before(users[j].CreatedAt)
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"total": len(users),
			"users": users,
		})
	}
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": []string{"item1", "item2", "item3"},
	})
}

func rolesHandler(rbacEngine *auth.RBACEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roles := auth.GetDefaultRoles()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"roles": roles,
		})
	}
}

func inferUsername(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func validatePassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters long")
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return fmt.Errorf("password must include upper, lower, digit, and special characters")
	}

	return nil
}

func sanitizeRoles(input []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(input))
	for _, role := range input {
		canon := strings.ToLower(strings.TrimSpace(role))
		if canon == "" {
			continue
		}
		if _, ok := allowedRoles[canon]; !ok {
			continue
		}
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		result = append(result, canon)
	}
	return result
}

func containsRole(roles []string, role string) bool {
	needle := strings.ToLower(strings.TrimSpace(role))
	for _, r := range roles {
		if strings.EqualFold(r, needle) {
			return true
		}
	}
	return false
}

func permissionsForRoles(roles []string) []string {
	set := make(map[string]struct{})
	for _, role := range roles {
		if perms, ok := defaultRolePermissions[strings.ToLower(role)]; ok {
			for _, p := range perms {
				if p == "" {
					continue
				}
				set[p] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(set))
	for perm := range set {
		result = append(result, perm)
	}
	sort.Strings(result)
	return result
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
