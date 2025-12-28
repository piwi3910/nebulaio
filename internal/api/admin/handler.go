package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/cluster"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
)

// Handler handles Admin API requests
type Handler struct {
	auth           *auth.Service
	bucket         *bucket.Service
	object         *object.Service
	store          metadata.Store
	discovery      *cluster.Discovery
	tieringHandler *TieringHandler
}

// NewHandler creates a new Admin API handler
func NewHandler(authService *auth.Service, bucketService *bucket.Service, objectService *object.Service, store metadata.Store, discovery *cluster.Discovery) *Handler {
	return &Handler{
		auth:      authService,
		bucket:    bucketService,
		object:    objectService,
		store:     store,
		discovery: discovery,
	}
}

// SetTieringHandler sets the tiering handler for tiering policy management
func (h *Handler) SetTieringHandler(tieringHandler *TieringHandler) {
	h.tieringHandler = tieringHandler
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
		r.Post("/policies/{name}/attach", h.AttachPolicyToUser)
		r.Post("/policies/{name}/detach", h.DetachPolicyFromUser)

		// Buckets
		r.Get("/buckets", h.ListBuckets)
		r.Post("/buckets", h.CreateBucket)
		r.Get("/buckets/{name}", h.GetBucket)
		r.Delete("/buckets/{name}", h.DeleteBucket)

		// Bucket Settings
		r.Get("/buckets/{name}/versioning", h.GetBucketVersioning)
		r.Put("/buckets/{name}/versioning", h.SetBucketVersioning)
		r.Get("/buckets/{name}/lifecycle", h.GetBucketLifecycle)
		r.Put("/buckets/{name}/lifecycle", h.SetBucketLifecycle)
		r.Delete("/buckets/{name}/lifecycle", h.DeleteBucketLifecycle)
		r.Get("/buckets/{name}/cors", h.GetBucketCORS)
		r.Put("/buckets/{name}/cors", h.SetBucketCORS)
		r.Delete("/buckets/{name}/cors", h.DeleteBucketCORS)
		r.Get("/buckets/{name}/policy", h.GetBucketPolicy)
		r.Put("/buckets/{name}/policy", h.SetBucketPolicy)
		r.Delete("/buckets/{name}/policy", h.DeleteBucketPolicy)
		r.Get("/buckets/{name}/tags", h.GetBucketTags)
		r.Put("/buckets/{name}/tags", h.SetBucketTags)
		r.Delete("/buckets/{name}/tags", h.DeleteBucketTags)

		// Cluster
		r.Get("/cluster/status", h.GetClusterStatus)
		r.Get("/cluster/nodes", h.ListNodes)
		r.Get("/cluster/nodes/{nodeId}/metrics", h.GetNodeMetrics)
		r.Get("/cluster/raft", h.GetRaftState)

		// Storage
		r.Get("/storage/info", h.GetStorageInfo)
		r.Get("/storage/metrics", h.GetStorageMetrics)

		// Presigned URLs
		r.Post("/presign", h.GeneratePresignedURL)

		// Audit logs
		r.Get("/audit-logs", h.ListAuditLogs)

		// AI/ML Features
		h.RegisterAIMLRoutes(r)

		// Tiering Policies (if tiering handler is set)
		if h.tieringHandler != nil {
			h.tieringHandler.RegisterTieringRoutes(r)
		}
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
	if h.discovery == nil {
		writeError(w, "Discovery not initialized", http.StatusServiceUnavailable)
		return
	}

	var nodes []*NodeResponse
	var healthyCount int

	// Get leader ID
	leaderID := h.discovery.LeaderID()

	// Get cluster configuration to determine voters
	voterMap := make(map[string]bool)
	if dbStore, ok := h.store.(*metadata.DragonboatStore); ok {
		if config, err := dbStore.GetClusterConfiguration(); err == nil {
			for _, server := range config.Servers {
				voterMap[fmt.Sprintf("%d", server.ID)] = server.IsVoter
			}
		}
	}

	// Get nodes from discovery
	for _, member := range h.discovery.Members() {
		isLeader := member.NodeID == leaderID
		isVoter := voterMap[member.NodeID]

		node := &NodeResponse{
			NodeID:     member.NodeID,
			RaftAddr:   member.RaftAddr,
			S3Addr:     member.S3Addr,
			AdminAddr:  member.AdminAddr,
			GossipAddr: member.GossipAddr,
			Role:       member.Role,
			Version:    member.Version,
			Status:     member.Status,
			IsLeader:   isLeader,
			IsVoter:    isVoter,
			JoinedAt:   member.JoinedAt,
			LastSeen:   member.LastSeen,
		}
		nodes = append(nodes, node)

		if member.Status == "alive" {
			healthyCount++
		}
	}

	response := &ListNodesResponse{
		Nodes:        nodes,
		TotalNodes:   len(nodes),
		HealthyNodes: healthyCount,
		LeaderID:     leaderID,
	}

	writeJSON(w, http.StatusOK, response)
}

// RaftStateProvider is an interface for accessing Raft state
type RaftStateProvider interface {
	Stats() map[string]string
}

func (h *Handler) GetRaftState(w http.ResponseWriter, r *http.Request) {
	// Get cluster info which contains raft state
	info, err := h.store.GetClusterInfo(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Try to get detailed stats if the store supports it
	response := map[string]interface{}{
		"state":     info.RaftState,
		"leader_id": info.LeaderID,
	}

	// If the store implements RaftStateProvider, get more details
	if provider, ok := h.store.(RaftStateProvider); ok {
		stats := provider.Stats()
		if term, ok := stats["term"]; ok {
			response["term"] = term
		}
		if commitIndex, ok := stats["commit_index"]; ok {
			response["commit_index"] = commitIndex
		}
		if appliedIndex, ok := stats["applied_index"]; ok {
			response["applied_index"] = appliedIndex
		}
		if lastLogIndex, ok := stats["last_log_index"]; ok {
			response["last_log_index"] = lastLogIndex
		}
		if numPeers, ok := stats["num_peers"]; ok {
			response["voters"] = numPeers
		}
	}

	writeJSON(w, http.StatusOK, response)
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

// Presigned URL handlers

// PresignRequest is the request body for generating a presigned URL
type PresignRequest struct {
	Method     string            `json:"method"`     // HTTP method: GET, PUT, DELETE, HEAD
	Bucket     string            `json:"bucket"`     // Bucket name
	Key        string            `json:"key"`        // Object key
	Expiration int               `json:"expiration"` // Expiration in seconds (max 604800 = 7 days)
	Headers    map[string]string `json:"headers"`    // Optional headers to sign
}

// PresignResponse is the response containing the presigned URL
type PresignResponse struct {
	URL       string    `json:"url"`
	Method    string    `json:"method"`
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GeneratePresignedURL generates a presigned URL for S3 operations
func (h *Handler) GeneratePresignedURL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.Header.Get("X-User-ID")

	var req PresignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Bucket == "" {
		writeError(w, "Bucket is required", http.StatusBadRequest)
		return
	}
	if req.Method == "" {
		req.Method = "GET"
	}

	// Validate method
	validMethods := map[string]bool{"GET": true, "PUT": true, "DELETE": true, "HEAD": true}
	if !validMethods[strings.ToUpper(req.Method)] {
		writeError(w, "Invalid method. Allowed: GET, PUT, DELETE, HEAD", http.StatusBadRequest)
		return
	}
	req.Method = strings.ToUpper(req.Method)

	// Validate bucket exists
	if _, err := h.bucket.GetBucket(ctx, req.Bucket); err != nil {
		writeError(w, "Bucket not found", http.StatusNotFound)
		return
	}

	// Set default expiration (1 hour)
	if req.Expiration <= 0 {
		req.Expiration = 3600
	}
	// Max 7 days
	if req.Expiration > 604800 {
		req.Expiration = 604800
	}

	// Get user's access key
	keys, err := h.store.ListAccessKeys(ctx, userID)
	if err != nil || len(keys) == 0 {
		writeError(w, "No access keys found for user", http.StatusBadRequest)
		return
	}

	// Use the first enabled access key
	var accessKey *metadata.AccessKey
	for _, k := range keys {
		if k.Enabled {
			accessKey = k
			break
		}
	}
	if accessKey == nil {
		writeError(w, "No enabled access keys found", http.StatusBadRequest)
		return
	}

	// Generate the presigned URL
	generator := auth.NewPresignedURLGenerator("us-east-1", "")
	presignedURL, err := generator.GeneratePresignedURL(auth.PresignParams{
		Method:      req.Method,
		Bucket:      req.Bucket,
		Key:         req.Key,
		Expiration:  time.Duration(req.Expiration) * time.Second,
		AccessKeyID: accessKey.AccessKeyID,
		SecretKey:   accessKey.SecretAccessKey,
		Region:      "us-east-1",
		Headers:     req.Headers,
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := PresignResponse{
		URL:       presignedURL,
		Method:    req.Method,
		Bucket:    req.Bucket,
		Key:       req.Key,
		ExpiresAt: time.Now().Add(time.Duration(req.Expiration) * time.Second),
	}

	writeJSON(w, http.StatusOK, response)
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

// Audit log handlers

// ListAuditLogs lists audit events with filtering
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	filter := audit.AuditFilter{}

	// Parse start_time
	if startStr := r.URL.Query().Get("start_time"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			filter.StartTime = t
		}
	}

	// Parse end_time
	if endStr := r.URL.Query().Get("end_time"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			filter.EndTime = t
		}
	}

	// Parse bucket filter
	filter.Bucket = r.URL.Query().Get("bucket")

	// Parse user filter
	filter.User = r.URL.Query().Get("user")

	// Parse event_type filter
	filter.EventType = r.URL.Query().Get("event_type")

	// Parse result filter
	filter.Result = r.URL.Query().Get("result")

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filter.MaxResults = limit
		}
	}
	if filter.MaxResults <= 0 {
		filter.MaxResults = 100
	}

	// Parse next_token for pagination
	filter.NextToken = r.URL.Query().Get("next_token")

	// Get audit events
	result, err := h.store.ListAuditEvents(ctx, filter)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ====================
// Bucket Settings Handlers
// ====================

// GetBucketVersioning returns versioning configuration for a bucket
func (h *Handler) GetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	status, err := h.bucket.GetVersioning(r.Context(), bucketName)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": status == metadata.VersioningEnabled,
		"status":  string(status),
	})
}

// SetBucketVersioningRequest represents the request body
type SetBucketVersioningRequest struct {
	Enabled bool `json:"enabled"`
}

// SetBucketVersioning updates versioning configuration for a bucket
func (h *Handler) SetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	var req SetBucketVersioningRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var status metadata.VersioningStatus
	if req.Enabled {
		status = metadata.VersioningEnabled
	} else {
		status = metadata.VersioningSuspended
	}

	if err := h.bucket.SetVersioning(r.Context(), bucketName, status); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": req.Enabled,
		"status":  string(status),
	})
}

// GetBucketLifecycle returns lifecycle configuration for a bucket
func (h *Handler) GetBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	rules, err := h.bucket.GetLifecycle(r.Context(), bucketName)
	if err != nil {
		// Return empty rules if not configured
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"rules": []metadata.LifecycleRule{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": rules,
	})
}

// SetBucketLifecycleRequest represents the request body
type SetBucketLifecycleRequest struct {
	Rules []metadata.LifecycleRule `json:"rules"`
}

// SetBucketLifecycle updates lifecycle configuration for a bucket
func (h *Handler) SetBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	var req SetBucketLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.bucket.SetLifecycle(r.Context(), bucketName, req.Rules); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": req.Rules,
	})
}

// DeleteBucketLifecycle deletes lifecycle configuration for a bucket
func (h *Handler) DeleteBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	if err := h.bucket.SetLifecycle(r.Context(), bucketName, nil); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetBucketCORS returns CORS configuration for a bucket
func (h *Handler) GetBucketCORS(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	rules, err := h.bucket.GetCORS(r.Context(), bucketName)
	if err != nil {
		// Return empty rules if not configured
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"rules": []metadata.CORSRule{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": rules,
	})
}

// SetBucketCORSRequest represents the request body
type SetBucketCORSRequest struct {
	Rules []metadata.CORSRule `json:"rules"`
}

// SetBucketCORS updates CORS configuration for a bucket
func (h *Handler) SetBucketCORS(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	var req SetBucketCORSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.bucket.SetCORS(r.Context(), bucketName, req.Rules); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": req.Rules,
	})
}

// DeleteBucketCORS deletes CORS configuration for a bucket
func (h *Handler) DeleteBucketCORS(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	if err := h.bucket.SetCORS(r.Context(), bucketName, nil); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetBucketPolicy returns bucket policy
func (h *Handler) GetBucketPolicy(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	policy, err := h.bucket.GetBucketPolicy(r.Context(), bucketName)
	if err != nil {
		// Return empty policy if not configured
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"policy": "",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"policy": policy,
	})
}

// SetBucketPolicyRequest represents the request body
type SetBucketPolicyRequest struct {
	Policy string `json:"policy"`
}

// SetBucketPolicy updates bucket policy
func (h *Handler) SetBucketPolicy(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	var req SetBucketPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.bucket.SetBucketPolicy(r.Context(), bucketName, req.Policy); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"policy": req.Policy,
	})
}

// DeleteBucketPolicy deletes bucket policy
func (h *Handler) DeleteBucketPolicy(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	if err := h.bucket.SetBucketPolicy(r.Context(), bucketName, ""); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetBucketTags returns bucket tags
func (h *Handler) GetBucketTags(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	tags, err := h.bucket.GetBucketTags(r.Context(), bucketName)
	if err != nil {
		// Return empty tags if not configured
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tags": map[string]string{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tags": tags,
	})
}

// SetBucketTagsRequest represents the request body
type SetBucketTagsRequest struct {
	Tags map[string]string `json:"tags"`
}

// SetBucketTags updates bucket tags
func (h *Handler) SetBucketTags(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	var req SetBucketTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.bucket.SetBucketTags(r.Context(), bucketName, req.Tags); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tags": req.Tags,
	})
}

// DeleteBucketTags deletes bucket tags
func (h *Handler) DeleteBucketTags(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "name")
	if err := h.bucket.SetBucketTags(r.Context(), bucketName, nil); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ====================
// Policy Attachment Handlers
// ====================

// AttachPolicyRequest represents the request body for attaching a policy
type AttachPolicyRequest struct {
	UserID string `json:"user_id"`
}

// AttachPolicyToUser attaches a policy to a user
func (h *Handler) AttachPolicyToUser(w http.ResponseWriter, r *http.Request) {
	policyName := chi.URLParam(r, "name")

	var req AttachPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		writeError(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Verify policy exists
	if _, err := h.store.GetPolicy(r.Context(), policyName); err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	// Get user
	user, err := h.store.GetUser(r.Context(), req.UserID)
	if err != nil {
		writeError(w, "User not found", http.StatusNotFound)
		return
	}

	// Check if policy is already attached
	for _, p := range user.Policies {
		if p == policyName {
			writeJSON(w, http.StatusOK, map[string]string{
				"message": "Policy already attached to user",
			})
			return
		}
	}

	// Attach policy
	user.Policies = append(user.Policies, policyName)
	user.UpdatedAt = time.Now()

	if err := h.store.UpdateUser(r.Context(), user); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Policy attached successfully",
	})
}

// DetachPolicyFromUser detaches a policy from a user
func (h *Handler) DetachPolicyFromUser(w http.ResponseWriter, r *http.Request) {
	policyName := chi.URLParam(r, "name")

	var req AttachPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		writeError(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Get user
	user, err := h.store.GetUser(r.Context(), req.UserID)
	if err != nil {
		writeError(w, "User not found", http.StatusNotFound)
		return
	}

	// Find and remove policy
	found := false
	newPolicies := make([]string, 0, len(user.Policies))
	for _, p := range user.Policies {
		if p == policyName {
			found = true
			continue
		}
		newPolicies = append(newPolicies, p)
	}

	if !found {
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "Policy not attached to user",
		})
		return
	}

	user.Policies = newPolicies
	user.UpdatedAt = time.Now()

	if err := h.store.UpdateUser(r.Context(), user); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Policy detached successfully",
	})
}

// ====================
// Node Metrics Handlers
// ====================

// GetNodeMetrics returns metrics for a specific node
func (h *Handler) GetNodeMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")

	if h.discovery == nil {
		writeError(w, "Discovery not initialized", http.StatusServiceUnavailable)
		return
	}

	// Find the specific node
	var targetMember *cluster.NodeInfo
	for _, member := range h.discovery.Members() {
		if member.NodeID == nodeID {
			targetMember = member
			break
		}
	}

	if targetMember == nil {
		writeError(w, "Node not found", http.StatusNotFound)
		return
	}

	// Build metrics response
	metrics := map[string]interface{}{
		"node_id":        targetMember.NodeID,
		"address":        targetMember.RaftAddr,
		"role":           targetMember.Role,
		"status":         targetMember.Status,
		"joined_at":      targetMember.JoinedAt,
		"last_heartbeat": targetMember.LastSeen,
	}

	writeJSON(w, http.StatusOK, metrics)
}

// ====================
// Storage Metrics Handlers
// ====================

// GetStorageMetrics returns aggregate storage metrics across all nodes
func (h *Handler) GetStorageMetrics(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.store.ListNodes(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate aggregate storage metrics
	var totalBytes, usedBytes, availableBytes, objectCount int64
	var storageNodeCount int

	for _, node := range nodes {
		if node.StorageInfo != nil {
			storageNodeCount++
			totalBytes += node.StorageInfo.TotalBytes
			usedBytes += node.StorageInfo.UsedBytes
			availableBytes += node.StorageInfo.AvailableBytes
			objectCount += node.StorageInfo.ObjectCount
		}
	}

	// Get bucket count
	buckets, err := h.bucket.ListBuckets(r.Context(), "")
	bucketCount := 0
	if err == nil {
		bucketCount = len(buckets)
	}

	// Calculate usage percentage
	var usagePercent float64
	if totalBytes > 0 {
		usagePercent = float64(usedBytes) / float64(totalBytes) * 100
	}

	response := map[string]interface{}{
		"total_bytes":        totalBytes,
		"used_bytes":         usedBytes,
		"available_bytes":    availableBytes,
		"usage_percent":      usagePercent,
		"object_count":       objectCount,
		"bucket_count":       bucketCount,
		"storage_node_count": storageNodeCount,
	}

	writeJSON(w, http.StatusOK, response)
}
