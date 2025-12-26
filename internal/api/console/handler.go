package console

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
)

// Handler handles Console API requests (user-facing, non-admin)
type Handler struct {
	auth   *auth.Service
	bucket *bucket.Service
	object *object.Service
}

// NewHandler creates a new Console API handler
func NewHandler(authService *auth.Service, bucketService *bucket.Service, objectService *object.Service) *Handler {
	return &Handler{
		auth:   authService,
		bucket: bucketService,
		object: objectService,
	}
}

// RegisterRoutes registers Console API routes
func (h *Handler) RegisterRoutes(r chi.Router) {
	// All console endpoints require authentication
	r.Use(h.authMiddleware)

	// Current user endpoints
	r.Get("/me", h.GetCurrentUser)
	r.Put("/me/password", h.UpdateMyPassword)

	// My access keys
	r.Get("/me/keys", h.ListMyAccessKeys)
	r.Post("/me/keys", h.CreateMyAccessKey)
	r.Delete("/me/keys/{accessKeyId}", h.DeleteMyAccessKey)

	// Bucket browsing (filtered by user access)
	r.Get("/buckets", h.ListMyBuckets)
	r.Get("/buckets/{bucket}/objects", h.ListBucketObjects)
	r.Get("/buckets/{bucket}/objects/{key:.*}", h.GetObjectInfo)

	// File operations (based on permissions)
	r.Post("/buckets/{bucket}/objects", h.UploadObject)
	r.Delete("/buckets/{bucket}/objects/{key:.*}", h.DeleteObject)
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

		// Add user info to request headers
		r.Header.Set("X-User-ID", claims.UserID)
		r.Header.Set("X-Username", claims.Username)
		r.Header.Set("X-User-Role", string(claims.Role))

		next.ServeHTTP(w, r)
	})
}

// User profile handlers

type UserProfileResponse struct {
	ID          string            `json:"id"`
	Username    string            `json:"username"`
	Email       string            `json:"email,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Role        metadata.UserRole `json:"role"`
}

func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	username := r.Header.Get("X-Username")
	role := metadata.UserRole(r.Header.Get("X-User-Role"))

	// Return user profile from token claims
	// In a full implementation, we'd fetch from the store
	response := UserProfileResponse{
		ID:       userID,
		Username: username,
		Role:     role,
	}

	writeJSON(w, http.StatusOK, response)
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Handler) UpdateMyPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	var req UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Verify current password before updating
	if err := h.auth.UpdatePassword(r.Context(), userID, req.NewPassword); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

// Access key handlers

type AccessKeyResponse struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key,omitempty"`
	Description     string    `json:"description"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
}

func (h *Handler) ListMyAccessKeys(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement - fetch from auth service
	writeJSON(w, http.StatusOK, []AccessKeyResponse{})
}

type CreateAccessKeyRequest struct {
	Description string `json:"description"`
}

func (h *Handler) CreateMyAccessKey(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

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

	response := AccessKeyResponse{
		AccessKeyID:     key.AccessKeyID,
		SecretAccessKey: secret, // Only returned once
		Description:     key.Description,
		Enabled:         key.Enabled,
		CreatedAt:       key.CreatedAt,
	}

	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) DeleteMyAccessKey(w http.ResponseWriter, r *http.Request) {
	// TODO: Verify the key belongs to the current user
	accessKeyID := chi.URLParam(r, "accessKeyId")
	_ = accessKeyID

	writeJSON(w, http.StatusOK, map[string]string{"message": "Access key deleted"})
}

// Bucket browsing handlers

type BucketSummary struct {
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	ObjectCount  int       `json:"object_count"`
	TotalSize    int64     `json:"total_size"`
	CanRead      bool      `json:"can_read"`
	CanWrite     bool      `json:"can_write"`
	CanDelete    bool      `json:"can_delete"`
}

func (h *Handler) ListMyBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	role := metadata.UserRole(r.Header.Get("X-User-Role"))

	// For admins, show all buckets
	// For regular users, show only buckets they have access to
	var owner string
	if role != metadata.RoleSuperAdmin && role != metadata.RoleAdmin {
		owner = r.Header.Get("X-User-ID")
	}

	buckets, err := h.bucket.ListBuckets(ctx, owner)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transform to summary with permissions
	var response []BucketSummary
	for _, b := range buckets {
		summary := BucketSummary{
			Name:      b.Name,
			CreatedAt: b.CreatedAt,
			CanRead:   true,
			CanWrite:  role != metadata.RoleReadOnly,
			CanDelete: role == metadata.RoleSuperAdmin || role == metadata.RoleAdmin,
		}
		response = append(response, summary)
	}

	writeJSON(w, http.StatusOK, response)
}

type ObjectSummary struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ContentType  string    `json:"content_type"`
	ETag         string    `json:"etag"`
	IsFolder     bool      `json:"is_folder"`
}

type ListObjectsResponse struct {
	Objects       []ObjectSummary `json:"objects"`
	Folders       []string        `json:"folders"`
	Prefix        string          `json:"prefix"`
	IsTruncated   bool            `json:"is_truncated"`
	NextPageToken string          `json:"next_page_token,omitempty"`
}

func (h *Handler) ListBucketObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	query := r.URL.Query()

	prefix := query.Get("prefix")
	delimiter := query.Get("delimiter")
	if delimiter == "" {
		delimiter = "/" // Default to folder-like listing
	}

	maxKeysStr := query.Get("max_keys")
	maxKeys := 100
	if maxKeysStr != "" {
		if mk, err := strconv.Atoi(maxKeysStr); err == nil && mk > 0 && mk <= 1000 {
			maxKeys = mk
		}
	}

	pageToken := query.Get("page_token")

	listing, err := h.object.ListObjects(ctx, bucketName, prefix, delimiter, maxKeys, pageToken)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := ListObjectsResponse{
		Prefix:        prefix,
		Folders:       listing.CommonPrefixes,
		IsTruncated:   listing.IsTruncated,
		NextPageToken: listing.NextContinuationToken,
	}

	for _, obj := range listing.Objects {
		response.Objects = append(response.Objects, ObjectSummary{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.ModifiedAt,
			ContentType:  obj.ContentType,
			ETag:         obj.ETag,
			IsFolder:     false,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

type ObjectInfoResponse struct {
	Key          string            `json:"key"`
	Bucket       string            `json:"bucket"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	ETag         string            `json:"etag"`
	LastModified time.Time         `json:"last_modified"`
	StorageClass string            `json:"storage_class"`
	VersionID    string            `json:"version_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	// Presigned URL for download
	DownloadURL string `json:"download_url,omitempty"`
}

func (h *Handler) GetObjectInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	meta, err := h.object.HeadObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "Object not found", http.StatusNotFound)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := ObjectInfoResponse{
		Key:          meta.Key,
		Bucket:       meta.Bucket,
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		ETag:         meta.ETag,
		LastModified: meta.ModifiedAt,
		StorageClass: meta.StorageClass,
		VersionID:    meta.VersionID,
		Metadata:     meta.Metadata,
		Tags:         meta.Tags,
		// TODO: Generate presigned URL
	}

	writeJSON(w, http.StatusOK, response)
}

// File operation handlers

func (h *Handler) UploadObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	role := metadata.UserRole(r.Header.Get("X-User-Role"))

	// Check write permission
	if role == metadata.RoleReadOnly {
		writeError(w, "Permission denied", http.StatusForbidden)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Get optional path prefix
	pathPrefix := r.FormValue("path")
	key := header.Filename
	if pathPrefix != "" {
		key = strings.TrimSuffix(pathPrefix, "/") + "/" + key
	}

	owner := r.Header.Get("X-User-ID")
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	meta, err := h.object.PutObject(ctx, bucketName, key, file, header.Size, contentType, owner, nil)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, ObjectSummary{
		Key:          meta.Key,
		Size:         meta.Size,
		LastModified: meta.ModifiedAt,
		ContentType:  meta.ContentType,
		ETag:         meta.ETag,
	})
}

func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	role := metadata.UserRole(r.Header.Get("X-User-Role"))

	// Check delete permission
	if role == metadata.RoleReadOnly || role == metadata.RoleUser {
		writeError(w, "Permission denied", http.StatusForbidden)
		return
	}

	if err := h.object.DeleteObject(ctx, bucketName, key); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
