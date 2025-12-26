package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"golang.org/x/crypto/bcrypt"
)

// Config holds auth service configuration
type Config struct {
	JWTSecret          string
	TokenExpiry        time.Duration
	RefreshTokenExpiry time.Duration
	RootUser           string
	RootPassword       string
}

// Service handles authentication and authorization
type Service struct {
	config Config
	store  metadata.Store
}

// NewService creates a new auth service
func NewService(config Config, store metadata.Store) *Service {
	return &Service{
		config: config,
		store:  store,
	}
}

// TokenClaims represents JWT claims
type TokenClaims struct {
	UserID   string            `json:"user_id"`
	Username string            `json:"username"`
	Role     metadata.UserRole `json:"role"`
	jwt.RegisteredClaims
}

// TokenPair represents access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// EnsureRootUser creates the root admin user if it doesn't exist
func (s *Service) EnsureRootUser(ctx context.Context) error {
	// Check if root user exists
	_, err := s.store.GetUserByUsername(ctx, s.config.RootUser)
	if err == nil {
		return nil // User exists
	}

	// Create root user
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(s.config.RootPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	rootUser := &metadata.User{
		ID:           generateID("user"),
		Username:     s.config.RootUser,
		PasswordHash: string(passwordHash),
		Role:         metadata.RoleSuperAdmin,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.store.CreateUser(ctx, rootUser); err != nil {
		return fmt.Errorf("failed to create root user: %w", err)
	}

	return nil
}

// Login authenticates a user and returns a token pair
func (s *Service) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if !user.Enabled {
		return nil, fmt.Errorf("user is disabled")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	return s.generateTokenPair(user)
}

// RefreshToken generates a new token pair using a refresh token
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.ValidateToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	user, err := s.store.GetUser(ctx, claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if !user.Enabled {
		return nil, fmt.Errorf("user is disabled")
	}

	return s.generateTokenPair(user)
}

// ValidateToken validates a JWT token and returns the claims
func (s *Service) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*TokenClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// generateTokenPair generates an access and refresh token pair
func (s *Service) generateTokenPair(user *metadata.User) (*TokenPair, error) {
	now := time.Now()
	accessExpiry := now.Add(s.config.TokenExpiry)
	refreshExpiry := now.Add(s.config.RefreshTokenExpiry)

	// Access token
	accessClaims := TokenClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(accessExpiry),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   user.ID,
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token
	refreshClaims := TokenClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiry),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   user.ID,
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(s.config.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    accessExpiry,
		TokenType:    "Bearer",
	}, nil
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, username, password, email string, role metadata.UserRole) (*metadata.User, error) {
	// Check if username already exists
	if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
		return nil, fmt.Errorf("username already exists")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &metadata.User{
		ID:           generateID("user"),
		Username:     username,
		PasswordHash: string(passwordHash),
		Email:        email,
		Role:         role,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// UpdatePassword updates a user's password
func (s *Service) UpdatePassword(ctx context.Context, userID, newPassword string) error {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.PasswordHash = string(passwordHash)
	user.UpdatedAt = time.Now()

	return s.store.UpdateUser(ctx, user)
}

// CreateAccessKey creates a new S3-compatible access key for a user
func (s *Service) CreateAccessKey(ctx context.Context, userID, description string) (*metadata.AccessKey, string, error) {
	// Verify user exists
	if _, err := s.store.GetUser(ctx, userID); err != nil {
		return nil, "", fmt.Errorf("user not found: %w", err)
	}

	accessKeyID := generateAccessKeyID()
	secretAccessKey := generateSecretAccessKey()

	key := &metadata.AccessKey{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey, // In production, this should be encrypted
		UserID:          userID,
		Description:     description,
		Enabled:         true,
		CreatedAt:       time.Now(),
	}

	if err := s.store.CreateAccessKey(ctx, key); err != nil {
		return nil, "", fmt.Errorf("failed to create access key: %w", err)
	}

	// Return the secret only once - it won't be retrievable later
	return key, secretAccessKey, nil
}

// ValidateAccessKey validates an S3 access key and returns the associated user
func (s *Service) ValidateAccessKey(ctx context.Context, accessKeyID string) (*metadata.AccessKey, *metadata.User, error) {
	key, err := s.store.GetAccessKey(ctx, accessKeyID)
	if err != nil {
		return nil, nil, fmt.Errorf("access key not found")
	}

	if !key.Enabled {
		return nil, nil, fmt.Errorf("access key is disabled")
	}

	user, err := s.store.GetUser(ctx, key.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("user not found")
	}

	if !user.Enabled {
		return nil, nil, fmt.Errorf("user is disabled")
	}

	return key, user, nil
}

// ValidateSignature validates an AWS Signature V4 signature
func (s *Service) ValidateSignature(ctx context.Context, accessKeyID, stringToSign, signature string) error {
	key, err := s.store.GetAccessKey(ctx, accessKeyID)
	if err != nil {
		return fmt.Errorf("access key not found")
	}

	if !key.Enabled {
		return fmt.Errorf("access key is disabled")
	}

	// Calculate expected signature
	h := hmac.New(sha256.New, []byte(key.SecretAccessKey))
	h.Write([]byte(stringToSign))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// CheckPermission checks if a user has permission for an action on a resource
func (s *Service) CheckPermission(ctx context.Context, user *metadata.User, action, resource string) error {
	// Super admins and admins have full access
	if user.Role == metadata.RoleSuperAdmin || user.Role == metadata.RoleAdmin {
		return nil
	}

	// Read-only users can only perform read operations
	if user.Role == metadata.RoleReadOnly {
		if !strings.HasPrefix(action, "s3:Get") && !strings.HasPrefix(action, "s3:List") {
			return fmt.Errorf("permission denied: read-only user")
		}
	}

	// TODO: Implement full IAM policy evaluation
	// For now, allow regular users to access their own resources

	return nil
}

// Helper functions

func generateID(prefix string) string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

func generateAccessKeyID() string {
	b := make([]byte, 10)
	rand.Read(b)
	return "AKIA" + strings.ToUpper(hex.EncodeToString(b))[:16]
}

func generateSecretAccessKey() string {
	b := make([]byte, 30)
	rand.Read(b)
	return hex.EncodeToString(b)[:40]
}
