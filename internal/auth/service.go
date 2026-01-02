// Package auth provides authentication and authorization services for NebulaIO.
//
// The package supports multiple authentication methods:
//   - AWS Signature V4 for S3 API access
//   - JWT tokens for Admin API and Console access
//   - LDAP integration via the ldap subpackage
//   - OIDC/SSO via the oidc subpackage
//
// Authorization is handled through IAM-compatible policies that control access
// to buckets, objects, and administrative operations.
//
// Example usage:
//
//	config := auth.Config{
//	    JWTSecret:    "your-secret",
//	    TokenExpiry:  24 * time.Hour,
//	    RootUser:     "admin",
//	    RootPassword: "secure-password",
//	}
//	svc := auth.NewService(config, store, policyEvaluator)
//	user, err := svc.Authenticate(ctx, username, password)
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/policy"
	"github.com/rs/zerolog/log"
)

// Service configuration constants.
const (
	initialRetryDelay   = 500 * time.Millisecond // Initial delay for retry backoff
	backoffMultiplier   = 1.5                    // Exponential backoff multiplier
	maxRetryDelay       = 5 * time.Second        // Maximum retry delay
	maxRetries          = 10                     // Maximum number of retry attempts
	idByteSize          = 16                     // Size in bytes for generated IDs
	accessKeyIDByteSize = 10                     // Size in bytes for access key ID
	secretKeyByteSize   = 30                     // Size in bytes for secret access key
	arnSplitParts       = 2                      // Number of parts when splitting ARN by /
)

// Config holds auth service configuration.
type Config struct {
	JWTSecret          string
	RootUser           string
	RootPassword       string
	TokenExpiry        time.Duration
	RefreshTokenExpiry time.Duration
}

// Service handles authentication and authorization.
type Service struct {
	store  metadata.Store
	config Config
}

// NewService creates a new auth service.
func NewService(config Config, store metadata.Store) *Service {
	return &Service{
		config: config,
		store:  store,
	}
}

// TokenClaims represents JWT claims.
type TokenClaims struct {
	jwt.RegisteredClaims

	UserID   string            `json:"user_id"`
	Username string            `json:"username"`
	Role     metadata.UserRole `json:"role"`
}

// TokenPair represents access and refresh tokens.
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// EnsureRootUser creates the root admin user if it doesn't exist.
// It handles the Raft "not leader" case by retrying with backoff.
// Returns (created bool, err error) where created indicates if a new user was created.
func (s *Service) EnsureRootUser(ctx context.Context) (bool, error) {
	// Check if root user already exists
	_, err := s.store.GetUserByUsername(ctx, s.config.RootUser)
	if err == nil {
		return false, nil // User already exists
	}

	// Validate root password strength
	validateErr := ValidatePasswordStrength(s.config.RootPassword)
	if validateErr != nil {
		return false, fmt.Errorf("root password does not meet security requirements: %w", validateErr)
	}

	// Hash the password
	passwordHash, err := HashPassword(s.config.RootPassword)
	if err != nil {
		return false, fmt.Errorf("failed to hash password: %w", err)
	}

	rootUser := &metadata.User{
		ID:           generateID("user"),
		Username:     s.config.RootUser,
		PasswordHash: passwordHash,
		Role:         metadata.RoleSuperAdmin,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Try to create the user with retries for "not leader" errors
	retryDelay := initialRetryDelay

	for range maxRetries {
		err := s.store.CreateUser(ctx, rootUser)
		if err == nil {
			return true, nil // Successfully created
		}

		// Check if it's a "not leader" error - need to wait for leadership
		if strings.Contains(err.Error(), "not leader") {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(retryDelay):
				// Exponential backoff with a cap
				retryDelay = min(
					time.Duration(float64(retryDelay)*backoffMultiplier),
					maxRetryDelay,
				)

				continue
			}
		}

		// Check if user was created by another node (race condition)
		if strings.Contains(err.Error(), "already exists") {
			return false, nil
		}

		return false, fmt.Errorf("failed to create root user: %w", err)
	}

	return false, fmt.Errorf("failed to create root user after %d attempts: not leader", maxRetries)
}

// Login authenticates a user and returns a token pair.
func (s *Service) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	if !user.Enabled {
		return nil, errors.New("user is disabled")
	}

	verifyErr := VerifyPassword(user.PasswordHash, password)
	if verifyErr != nil {
		return nil, errors.New("invalid credentials")
	}

	return s.generateTokenPair(user)
}

// RefreshToken generates a new token pair using a refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.ValidateToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	user, err := s.store.GetUser(ctx, claims.UserID)
	if err != nil {
		return nil, errors.New("user not found")
	}

	if !user.Enabled {
		return nil, errors.New("user is disabled")
	}

	return s.generateTokenPair(user)
}

// ValidateToken validates a JWT token and returns the claims.
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

	return nil, errors.New("invalid token")
}

// generateTokenPair generates an access and refresh token pair.
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

// CreateUser creates a new user with full validation.
func (s *Service) CreateUser(ctx context.Context, username, password, email string, role metadata.UserRole) (*metadata.User, error) {
	// Validate username format
	usernameErr := ValidateUsername(username)
	if usernameErr != nil {
		return nil, fmt.Errorf("invalid username: %w", usernameErr)
	}

	// Validate password strength
	err := ValidatePasswordStrength(password)
	if err != nil {
		return nil, fmt.Errorf("invalid password: %w", err)
	}

	// Validate email format if provided
	err = ValidateEmail(email)
	if err != nil {
		return nil, fmt.Errorf("invalid email: %w", err)
	}

	// Validate role
	if !isValidRole(role) {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	// Check if username already exists
	_, err = s.store.GetUserByUsername(ctx, username)
	if err == nil {
		return nil, errors.New("username already exists")
	}

	// Hash the password
	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &metadata.User{
		ID:           generateID("user"),
		Username:     username,
		PasswordHash: passwordHash,
		Email:        email,
		Role:         role,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err = s.store.CreateUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// isValidRole checks if the given role is a valid user role.
func isValidRole(role metadata.UserRole) bool {
	switch role {
	case metadata.RoleSuperAdmin, metadata.RoleAdmin, metadata.RoleUser, metadata.RoleReadOnly, metadata.RoleService:
		return true
	default:
		return false
	}
}

// UpdatePassword updates a user's password with validation.
func (s *Service) UpdatePassword(ctx context.Context, userID, newPassword string) error {
	// Validate password strength
	err := ValidatePasswordStrength(newPassword)
	if err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}

	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now()

	return s.store.UpdateUser(ctx, user)
}

// CreateAccessKey creates a new S3-compatible access key for a user.
func (s *Service) CreateAccessKey(ctx context.Context, userID, description string) (*metadata.AccessKey, string, error) {
	// Verify user exists
	_, err := s.store.GetUser(ctx, userID)
	if err != nil {
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

	err = s.store.CreateAccessKey(ctx, key)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create access key: %w", err)
	}

	// Return the secret only once - it won't be retrievable later
	return key, secretAccessKey, nil
}

// ValidateAccessKey validates an S3 access key and returns the associated user.
func (s *Service) ValidateAccessKey(ctx context.Context, accessKeyID string) (*metadata.AccessKey, *metadata.User, error) {
	key, err := s.store.GetAccessKey(ctx, accessKeyID)
	if err != nil {
		return nil, nil, errors.New("access key not found")
	}

	if !key.Enabled {
		return nil, nil, errors.New("access key is disabled")
	}

	user, err := s.store.GetUser(ctx, key.UserID)
	if err != nil {
		return nil, nil, errors.New("user not found")
	}

	if !user.Enabled {
		return nil, nil, errors.New("user is disabled")
	}

	return key, user, nil
}

// ValidateSignature validates an AWS Signature V4 signature.
func (s *Service) ValidateSignature(ctx context.Context, accessKeyID, stringToSign, signature string) error {
	key, err := s.store.GetAccessKey(ctx, accessKeyID)
	if err != nil {
		return errors.New("access key not found")
	}

	if !key.Enabled {
		return errors.New("access key is disabled")
	}

	// Calculate expected signature
	h := hmac.New(sha256.New, []byte(key.SecretAccessKey))
	h.Write([]byte(stringToSign))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return errors.New("signature mismatch")
	}

	return nil
}

// GetUserByID retrieves a user by their ID.
func (s *Service) GetUserByID(ctx context.Context, id string) (*metadata.User, error) {
	user, err := s.store.GetUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return user, nil
}

// ListUsers returns all users in the system.
func (s *Service) ListUsers(ctx context.Context) ([]*metadata.User, error) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	return users, nil
}

// DeleteUser deletes a user by ID.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	// Get user first to verify it exists and check if it's the root user
	user, err := s.store.GetUser(ctx, id)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	// Prevent deletion of the root user
	if user.Username == s.config.RootUser {
		return errors.New("cannot delete the root admin user")
	}

	// Delete all access keys for this user first
	keys, err := s.store.ListAccessKeys(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to list user access keys: %w", err)
	}

	for _, key := range keys {
		err := s.store.DeleteAccessKey(ctx, key.AccessKeyID)
		if err != nil {
			return fmt.Errorf("failed to delete access key %s: %w", key.AccessKeyID, err)
		}
	}

	err = s.store.DeleteUser(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// EnableUser enables a user account.
func (s *Service) EnableUser(ctx context.Context, id string) error {
	user, err := s.store.GetUser(ctx, id)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	if user.Enabled {
		return nil // Already enabled
	}

	user.Enabled = true
	user.UpdatedAt = time.Now()

	err = s.store.UpdateUser(ctx, user)
	if err != nil {
		return fmt.Errorf("failed to enable user: %w", err)
	}

	return nil
}

// DisableUser disables a user account.
func (s *Service) DisableUser(ctx context.Context, id string) error {
	user, err := s.store.GetUser(ctx, id)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	// Prevent disabling the root user
	if user.Username == s.config.RootUser {
		return errors.New("cannot disable the root admin user")
	}

	if !user.Enabled {
		return nil // Already disabled
	}

	user.Enabled = false
	user.UpdatedAt = time.Now()

	err = s.store.UpdateUser(ctx, user)
	if err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	return nil
}

// CheckPermission checks if a user has permission for an action on a resource
// It uses a layered approach:
// 1. Role-based checks (super admin, admin have full access)
// 2. Read-only role restrictions
// 3. IAM policy evaluation for users with attached policies
// 4. Default allow for regular users (fallback).
func (s *Service) CheckPermission(ctx context.Context, user *metadata.User, action, resource string) error {
	// Layer 1: Super admins and admins have full access (highest privilege)
	if user.Role == metadata.RoleSuperAdmin || user.Role == metadata.RoleAdmin {
		return nil
	}

	// Layer 2: Read-only users can only perform read operations
	if user.Role == metadata.RoleReadOnly {
		if !strings.HasPrefix(action, "s3:Get") && !strings.HasPrefix(action, "s3:List") {
			return fmt.Errorf("permission denied: read-only user cannot perform %s", action)
		}

		return nil
	}

	// Layer 3: Evaluate user-attached IAM policies if any exist
	if len(user.Policies) > 0 {
		return s.evaluateUserPolicies(ctx, user, action, resource)
	}

	// Layer 4: Default allow for regular users without specific policies
	// This maintains backward compatibility
	return nil
}

// evaluateUserPolicies evaluates all policies attached to a user
// Following AWS IAM evaluation logic:
// 1. By default, all requests are denied (implicit deny)
// 2. An explicit allow overrides the implicit deny
// 3. An explicit deny overrides any allows.
func (s *Service) evaluateUserPolicies(ctx context.Context, user *metadata.User, action, resource string) error {
	hasExplicitAllow := false

	// Parse the resource to extract bucket and object key for evaluation context
	bucketName, objectKey := parseResourceARN(resource)

	// Evaluate each policy attached to the user
	for _, policyName := range user.Policies {
		// Retrieve the policy from the store
		policyMeta, err := s.store.GetPolicy(ctx, policyName)
		if err != nil {
			// If policy doesn't exist, log and continue
			// This allows graceful handling of deleted policies
			continue
		}

		// Parse the policy document
		pol, err := policy.ParsePolicy(policyMeta.Document)
		if err != nil {
			// Invalid policy document - skip it
			continue
		}

		// Build evaluation context
		evalCtx := policy.EvalContext{
			Principal:  user.ID,
			Action:     action,
			Resource:   resource,
			BucketName: bucketName,
			ObjectKey:  objectKey,
			Conditions: make(map[string]string),
		}

		// Evaluate the policy
		result, err := pol.Evaluate(evalCtx)
		if err != nil {
			// Error during evaluation - skip this policy
			continue
		}

		// Process evaluation result
		switch result {
		case policy.EvalDeny:
			// Explicit deny always wins - reject immediately
			return fmt.Errorf("permission denied by policy %s: action %s on %s", policyName, action, resource)
		case policy.EvalAllow:
			// Found an explicit allow
			hasExplicitAllow = true
		case policy.EvalDefault:
			// No match in this policy - continue checking others
			continue
		}
	}

	// After evaluating all policies:
	// - If we found an explicit allow and no explicit deny, allow the request
	// - Otherwise, deny (implicit deny)
	if hasExplicitAllow {
		return nil
	}

	return fmt.Errorf("permission denied: no policy allows action %s on %s", action, resource)
}

// parseResourceARN extracts bucket name and object key from a resource ARN
// Supports formats:
// - arn:aws:s3:::bucket
// - arn:aws:s3:::bucket/key
// - arn:aws:s3:::bucket/prefix/key.
func parseResourceARN(resourceARN string) (bucket, key string) {
	// Remove the ARN prefix if present
	resourceARN = strings.TrimPrefix(resourceARN, "arn:aws:s3:::")

	// Split bucket and key
	parts := strings.SplitN(resourceARN, "/", arnSplitParts)

	bucket = parts[0]
	if len(parts) > 1 {
		key = parts[1]
	}

	return bucket, key
}

// Helper functions

func generateID(prefix string) string {
	b := make([]byte, idByteSize)
	_, err := rand.Read(b)
	if err != nil {
		// Cryptographic failure is unrecoverable - log and panic to prevent weak IDs
		log.Error().Err(err).Str("prefix", prefix).Msg("crypto/rand.Read failed while generating ID - this indicates a serious system entropy problem; check that /dev/urandom is available and the system has sufficient entropy")
		panic(fmt.Sprintf("crypto/rand.Read failed for prefix %s: %v - ensure /dev/urandom is accessible", prefix, err))
	}

	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

func generateAccessKeyID() string {
	b := make([]byte, accessKeyIDByteSize)
	_, err := rand.Read(b)
	if err != nil {
		// Cryptographic failure is unrecoverable - log and panic to prevent weak access keys
		log.Error().Err(err).Msg("crypto/rand.Read failed while generating access key ID - this indicates a serious system entropy problem; check that /dev/urandom is available")
		panic(fmt.Sprintf("crypto/rand.Read failed for access key ID generation: %v - ensure /dev/urandom is accessible", err))
	}

	return "AKIA" + strings.ToUpper(hex.EncodeToString(b))[:idByteSize]
}

func generateSecretAccessKey() string {
	b := make([]byte, secretKeyByteSize)
	_, err := rand.Read(b)
	if err != nil {
		// Cryptographic failure is unrecoverable - log and panic to prevent weak secret keys
		log.Error().Err(err).Msg("crypto/rand.Read failed while generating secret access key - this indicates a serious system entropy problem; check that /dev/urandom is available")
		panic(fmt.Sprintf("crypto/rand.Read failed for secret access key generation: %v - ensure /dev/urandom is accessible", err))
	}

	return hex.EncodeToString(b)[:secretKeyByteSize+idByteSize]
}
