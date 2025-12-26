package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
)

// Handler handles Admin API requests
type Handler struct {
	auth   *auth.Service
	bucket *bucket.Service
	object *object.Service
	store  metadata.Store
}

// NewHandler creates a new Admin API handler
func NewHandler(authService *auth.Service, bucketService *bucket.Service, objectService *object.Service, store metadata.Store) *Handler {
	return &Handler{
		auth:   authService,
		bucket: bucketService,
		object: objectService,
		store:  store,
	}
}

// RegisterRoutes registers Admin API routes
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Auth endpoints (public)
	r.Post("/auth/login", h.Login)
	r.Post("/auth/refresh", h.RefreshToken)

	// Protected endpoints
	r.Group(func(r chi.Router) {
		r.Use(h.authMiddleware)

		// Auth
		r.Post("/auth/logout", h.Logout)

		// Users
		r.Get("/users", h.ListUsers)
		r.Post("/users", h.CreateUser)
		r.Get("/users/{id}", h.GetUser)
		r.Put("/users/{id}", h.UpdateUser)
		r.Delete("/users/{id}", h.DeleteUser)
		r.Put("/users/{id}/password", h.UpdatePassword)

		// Access Keys
		r.Get("/users/{id}/keys", h.ListAccessKeys)
		r.Post("/users/{id}/keys", h.CreateAccessKey)
		r.Delete("/keys/{accessKeyId}", h.DeleteAccessKey)

		// Policies
		r.Get("/policies", h.ListPolicies)
		r.Post("/policies", h.CreatePolicy)
		r.Get("/policies/{name}", h.GetPolicy)
		r.Put("/policies/{name}", h.UpdatePolicy)
		r.Delete("/policies/{name}", h.DeletePolicy)

		// Buckets
		r.Get("/buckets", h.ListBuckets)
		r.Post("/buckets", h.CreateBucket)
		r.Get("/buckets/{name}", h.GetBucket)
		r.Delete("/buckets/{name}", h.DeleteBucket)

		// Cluster
		r.Get("/cluster/status", h.GetClusterStatus)
		r.Get("/cluster/nodes", h.ListNodes)

		// Storage
		r.Get("/storage/info", h.GetStorageInfo)
	})
}

// Auth middleware
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			writeError(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		claims, err := h.auth.ValidateToken(parts[1])
		if err != nil {
			writeError(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Add user info to context (simplified)
		r.Header.Set("X-User-ID", claims.UserID)
		r.Header.Set("X-User-Role", string(claims.Role))

		next.ServeHTTP(w, r)
	})
}

// Auth handlers

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tokens, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tokens, err := h.auth.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	// For JWT, logout is handled client-side by discarding the token
	writeJSON(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

// User handlers

type CreateUserRequest struct {
	Username string            `json:"username"`
	Password string            `json:"password"`
	Email    string            `json:"email"`
	Role     metadata.UserRole `json:"role"`
}

type UserResponse struct {
	ID          string            `json:"id"`
	Username    string            `json:"username"`
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name,omitempty"`
	Role        metadata.UserRole `json:"role"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

func userToResponse(user *metadata.User) *UserResponse {
	return &UserResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Enabled:     user.Enabled,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var response []*UserResponse
	for _, user := range users {
		response = append(response, userToResponse(user))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Role == "" {
		req.Role = metadata.RoleUser
	}

	user, err := h.auth.CreateUser(r.Context(), req.Username, req.Password, req.Email, req.Role)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, userToResponse(user))
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, "User not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type UpdateUserRequest struct {
	Email       string            `json:"email,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Role        metadata.UserRole `json:"role,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, "User not found", http.StatusNotFound)
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email != "" {
		user.Email = req.Email
	}
	if req.DisplayName != "" {
		user.DisplayName = req.DisplayName
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Enabled != nil {
		user.Enabled = *req.Enabled
	}
	user.UpdatedAt = time.Now()

	if err := h.store.UpdateUser(r.Context(), user); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type UpdatePasswordRequest struct {
	Password string `json:"password"`
}

func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.auth.UpdatePassword(r.Context(), id, req.Password); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated"})
}

// Access Key handlers

type CreateAccessKeyRequest struct {
	Description string `json:"description"`
}

type AccessKeyResponse struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key,omitempty"` // Only returned on creation
	UserID          string    `json:"user_id"`
	Description     string    `json:"description"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
}

func (h *Handler) ListAccessKeys(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	keys, err := h.store.ListAccessKeys(r.Context(), userID)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var response []*AccessKeyResponse
	for _, key := range keys {
		response = append(response, &AccessKeyResponse{
			AccessKeyID: key.AccessKeyID,
			UserID:      key.UserID,
			Description: key.Description,
			Enabled:     key.Enabled,
			CreatedAt:   key.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) CreateAccessKey(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	var req CreateAccessKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	key, secret, err := h.auth.CreateAccessKey(r.Context(), userID, req.Description)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := &AccessKeyResponse{
		AccessKeyID:     key.AccessKeyID,
		SecretAccessKey: secret, // Only returned once!
		UserID:          key.UserID,
		Description:     key.Description,
		Enabled:         key.Enabled,
		CreatedAt:       key.CreatedAt,
	}

	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) DeleteAccessKey(w http.ResponseWriter, r *http.Request) {
	accessKeyID := chi.URLParam(r, "accessKeyId")

	if err := h.store.DeleteAccessKey(r.Context(), accessKeyID); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Policy handlers

type CreatePolicyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Document    string `json:"document"` // JSON policy document
}

func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.store.ListPolicies(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, policies)
}

func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	policy := &metadata.Policy{
		Name:        req.Name,
		Description: req.Description,
		Document:    req.Document,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.store.CreatePolicy(r.Context(), policy); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, policy)
}

func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	policy, err := h.store.GetPolicy(r.Context(), name)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	policy, err := h.store.GetPolicy(r.Context(), name)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	policy.Description = req.Description
	policy.Document = req.Document
	policy.UpdatedAt = time.Now()

	if err := h.store.UpdatePolicy(r.Context(), policy); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.store.DeletePolicy(r.Context(), name); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Bucket handlers

type CreateBucketRequest struct {
	Name         string `json:"name"`
	Region       string `json:"region"`
	StorageClass string `json:"storage_class"`
}

func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.bucket.ListBuckets(r.Context(), "")
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, buckets)
}

func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	var req CreateBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	owner := r.Header.Get("X-User-ID")
	if owner == "" {
		owner = "admin"
	}

	bucket, err := h.bucket.CreateBucket(r.Context(), req.Name, owner, req.Region, req.StorageClass)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, bucket)
}

func (h *Handler) GetBucket(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	bucket, err := h.bucket.GetBucket(r.Context(), name)
	if err != nil {
		writeError(w, "Bucket not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, bucket)
}

func (h *Handler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.bucket.DeleteBucket(r.Context(), name); err != nil {
		if strings.Contains(err.Error(), "not empty") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Cluster handlers

func (h *Handler) GetClusterStatus(w http.ResponseWriter, r *http.Request) {
	info, err := h.store.GetClusterInfo(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.store.ListNodes(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, nodes)
}

// Storage handlers

func (h *Handler) GetStorageInfo(w http.ResponseWriter, r *http.Request) {
	// For now, return basic info from cluster
	info, err := h.store.GetClusterInfo(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster":   info,
		"is_leader": h.store.IsLeader(),
	})
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, status, map[string]string{"error": message})
}
