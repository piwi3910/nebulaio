// Package console implements the NebulaIO web console API.
//
// The console API provides endpoints for the React-based web UI, including:
//   - Session management: Login, logout, token refresh
//   - Bucket browsing: List buckets, view objects, upload/download
//   - User profile: View and update user settings
//   - Dashboard: System status, metrics, and activity
//
// This API is designed for browser-based access and uses JWT session tokens.
// For programmatic access, use the Admin API or S3 API instead.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// Console handler constants.
const (
	// Default presigned URL expiration in seconds.
	defaultPresignedExpirySec = 3600 // 1 hour
	// Copy buffer size in bytes.
	copyBufferSize = 32 * 1024 // 32KB
	// Maximum presigned URL expiry in seconds (7 days).
	maxPresignedExpirySec = 604800
	// Maximum multipart form size in bytes (32MB).
	maxMultipartFormSize = 32 << 20
)

// Handler handles Console API requests (user-facing, non-admin).
type Handler struct {
	auth   *auth.Service
	bucket *bucket.Service
	object *object.Service
	store  metadata.Store
}

// NewHandler creates a new Console API handler.
func NewHandler(authService *auth.Service, bucketService *bucket.Service, objectService *object.Service, store metadata.Store) *Handler {
	return &Handler{
		auth:   authService,
		bucket: bucketService,
		object: objectService,
		store:  store,
	}
}

// RegisterRoutes registers Console API routes.
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

	// Object content
	r.Get("/buckets/{bucket}/objects/{key:.*}/content", h.GetObjectContent)

	// Presigned URLs for downloads
	r.Get("/buckets/{bucket}/objects/{key:.*}/download-url", h.GetDownloadURL)
	r.Post("/presign", h.GeneratePresignedURL)

	// Bucket settings (read-only for console users)
	r.Get("/buckets/{bucket}/settings", h.GetBucketSettings)
}

// Auth middleware.
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
		r.Header.Set("X-User-Id", claims.UserID)
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
	userID := r.Header.Get("X-User-Id")
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
	ctx := r.Context()
	userID := r.Header.Get("X-User-Id")

	var req UpdatePasswordRequest

	decodeErr := json.NewDecoder(r.Body).Decode(&req)
	if decodeErr != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate current password before updating
	user, err := h.auth.GetUserByID(ctx, userID)
	if err != nil {
		writeError(w, "User not found", http.StatusNotFound)
		return
	}

	// Verify current password matches
	verifyErr := auth.VerifyPassword(user.PasswordHash, req.CurrentPassword)
	if verifyErr != nil {
		writeError(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	// Update password
	updateErr := h.auth.UpdatePassword(ctx, userID, req.NewPassword)
	if updateErr != nil {
		writeError(w, updateErr.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

// Access key handlers

type AccessKeyResponse struct {
	CreatedAt       time.Time `json:"created_at"`
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key,omitempty"`
	Description     string    `json:"description"`
	Enabled         bool      `json:"enabled"`
}

func (h *Handler) ListMyAccessKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.Header.Get("X-User-Id")

	// Fetch access keys from store
	keys, err := h.store.ListAccessKeys(ctx, userID)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transform to response format (without exposing secrets)
	response := make([]AccessKeyResponse, 0, len(keys))
	for _, key := range keys {
		response = append(response, AccessKeyResponse{
			AccessKeyID: key.AccessKeyID,
			Description: key.Description,
			Enabled:     key.Enabled,
			CreatedAt:   key.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

type CreateAccessKeyRequest struct {
	Description string `json:"description"`
}

func (h *Handler) CreateMyAccessKey(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")

	var req CreateAccessKeyRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
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
	ctx := r.Context()
	userID := r.Header.Get("X-User-Id")
	accessKeyID := chi.URLParam(r, "accessKeyId")

	// Verify the key exists and belongs to the current user
	key, err := h.store.GetAccessKey(ctx, accessKeyID)
	if err != nil {
		writeError(w, "Access key not found", http.StatusNotFound)
		return
	}

	if key.UserID != userID {
		writeError(w, "Access denied: key does not belong to current user", http.StatusForbidden)
		return
	}

	// Delete the access key
	err = h.store.DeleteAccessKey(ctx, accessKeyID)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Access key deleted"})
}

// Bucket browsing handlers

type BucketSummary struct {
	CreatedAt   time.Time `json:"created_at"`
	Name        string    `json:"name"`
	ObjectCount int       `json:"object_count"`
	TotalSize   int64     `json:"total_size"`
	CanRead     bool      `json:"can_read"`
	CanWrite    bool      `json:"can_write"`
	CanDelete   bool      `json:"can_delete"`
}

func (h *Handler) ListMyBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	role := metadata.UserRole(r.Header.Get("X-User-Role"))

	// For admins, show all buckets
	// For regular users, show only buckets they have access to
	var owner string
	if role != metadata.RoleSuperAdmin && role != metadata.RoleAdmin {
		owner = r.Header.Get("X-User-Id")
	}

	buckets, err := h.bucket.ListBuckets(ctx, owner)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transform to summary with permissions
	response := make([]BucketSummary, 0, len(buckets))
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
	LastModified time.Time `json:"last_modified"`
	Key          string    `json:"key"`
	ContentType  string    `json:"content_type"`
	ETag         string    `json:"etag"`
	Size         int64     `json:"size"`
	IsFolder     bool      `json:"is_folder"`
}

type ListObjectsResponse struct {
	Prefix        string          `json:"prefix"`
	NextPageToken string          `json:"next_page_token,omitempty"`
	Objects       []ObjectSummary `json:"objects"`
	Folders       []string        `json:"folders"`
	IsTruncated   bool            `json:"is_truncated"`
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
		mk, err := strconv.Atoi(maxKeysStr)
		if err == nil && mk > 0 && mk <= 1000 {
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
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	Key          string            `json:"key"`
	Bucket       string            `json:"bucket"`
	ContentType  string            `json:"content_type"`
	ETag         string            `json:"etag"`
	StorageClass string            `json:"storage_class"`
	VersionID    string            `json:"version_id,omitempty"`
	DownloadURL  string            `json:"download_url,omitempty"`
	Size         int64             `json:"size"`
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
	}

	// Generate presigned URL if user has access keys
	userID := r.Header.Get("X-User-Id")

	keys, err := h.store.ListAccessKeys(ctx, userID)
	if err != nil || len(keys) == 0 {
		writeJSON(w, http.StatusOK, response)

		return
	}

	// Find first enabled key
	var accessKey *metadata.AccessKey

	for _, k := range keys {
		if k.Enabled {
			accessKey = k

			break
		}
	}

	// Generate presigned URL if we have an enabled key
	if accessKey == nil {
		writeJSON(w, http.StatusOK, response)

		return
	}

	generator := auth.NewPresignedURLGenerator("us-east-1", "")

	presignedURL, err := generator.GeneratePresignedURL(auth.PresignParams{
		Method:      "GET",
		Bucket:      bucketName,
		Key:         key,
		Expiration:  defaultPresignedExpirySec * time.Second,
		AccessKeyID: accessKey.AccessKeyID,
		SecretKey:   accessKey.SecretAccessKey,
		Region:      "us-east-1",
	})
	if err == nil {
		response.DownloadURL = presignedURL
	}

	writeJSON(w, http.StatusOK, response)
}

// GetObjectContent streams the object content directly to the client.
func (h *Handler) GetObjectContent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// Get object metadata first
	meta, err := h.object.HeadObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "Object not found", http.StatusNotFound)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	// Get object content
	reader, objMeta, err := h.object.GetObject(ctx, bucketName, key)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer func() { _ = reader.Close() }()

	// Set response headers
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(objMeta.Size, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.ModifiedAt.UTC().Format(time.RFC1123))

	// Stream content to response
	w.WriteHeader(http.StatusOK)
	_, _ = copyBuffer(w, reader)
}

// copyBuffer copies from src to dst using a buffer.
func copyBuffer(dst http.ResponseWriter, src interface {
	Read(p []byte) (n int, err error)
}) (int64, error) {
	buf := make([]byte, copyBufferSize)

	var written int64

	for {
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}

			if werr != nil {
				return written, werr
			}
		}

		if rerr != nil {
			if rerr == io.EOF {
				return written, nil
			}

			return written, rerr
		}
	}
}

// BucketSettingsResponse contains bucket settings for console users.
type BucketSettingsResponse struct {
	Name         string `json:"name"`
	Region       string `json:"region"`
	Versioning   string `json:"versioning"`
	StorageClass string `json:"storage_class"`
}

// GetBucketSettings returns bucket settings (read-only for console users).
func (h *Handler) GetBucketSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	bucket, err := h.bucket.GetBucket(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	response := BucketSettingsResponse{
		Name:         bucket.Name,
		Region:       bucket.Region,
		Versioning:   string(bucket.Versioning),
		StorageClass: bucket.StorageClass,
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
	err := r.ParseMultipartForm(maxMultipartFormSize)
	if err != nil {
		writeError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, "No file provided", http.StatusBadRequest)
		return
	}

	defer func() { _ = file.Close() }()

	// Get optional path prefix
	pathPrefix := r.FormValue("path")

	key := header.Filename
	if pathPrefix != "" {
		key = strings.TrimSuffix(pathPrefix, "/") + "/" + key
	}

	owner := r.Header.Get("X-User-Id")

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

	_, err := h.object.DeleteObject(ctx, bucketName, key)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Presigned URL handlers

// DownloadURLResponse contains the presigned download URL.
type DownloadURLResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
}

// GetDownloadURL generates a presigned download URL for an object.
func (h *Handler) GetDownloadURL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	userID := r.Header.Get("X-User-Id")

	// Verify object exists
	_, err := h.object.HeadObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "Object not found", http.StatusNotFound)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	// Get user's access key
	keys, err := h.store.ListAccessKeys(ctx, userID)
	if err != nil || len(keys) == 0 {
		writeError(w, "No access keys found for user. Please create an access key first.", http.StatusBadRequest)
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

	// Get expiration from query param (default 1 hour, max 7 days)
	expirationStr := r.URL.Query().Get("expiration")
	expiration := defaultPresignedExpirySec

	if expirationStr != "" {
		exp, err := strconv.Atoi(expirationStr)
		if err == nil && exp > 0 {
			expiration = exp
		}
	}

	if expiration > maxPresignedExpirySec {
		expiration = maxPresignedExpirySec
	}

	// Generate the presigned URL
	generator := auth.NewPresignedURLGenerator("us-east-1", "")

	presignedURL, err := generator.GeneratePresignedURL(auth.PresignParams{
		Method:      "GET",
		Bucket:      bucketName,
		Key:         key,
		Expiration:  time.Duration(expiration) * time.Second,
		AccessKeyID: accessKey.AccessKeyID,
		SecretKey:   accessKey.SecretAccessKey,
		Region:      "us-east-1",
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := DownloadURLResponse{
		URL:       presignedURL,
		ExpiresAt: time.Now().Add(time.Duration(expiration) * time.Second),
	}

	writeJSON(w, http.StatusOK, response)
}

// ConsolePresignRequest is the request body for generating a presigned URL from console.
type ConsolePresignRequest struct {
	Headers    map[string]string `json:"headers"`
	Method     string            `json:"method"`
	Bucket     string            `json:"bucket"`
	Key        string            `json:"key"`
	Expiration int               `json:"expiration"`
}

// ConsolePresignResponse is the response containing the presigned URL.
type ConsolePresignResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
	Method    string    `json:"method"`
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
}

// GeneratePresignedURL generates a presigned URL for user's own buckets.
func (h *Handler) GeneratePresignedURL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.Header.Get("X-User-Id")
	role := metadata.UserRole(r.Header.Get("X-User-Role"))

	req, err := h.parsePresignRequest(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.validatePresignRequest(req, role); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.checkPresignPermissions(ctx, userID, role, req); err != nil {
		writeError(w, err.Error(), http.StatusForbidden)
		return
	}

	bucket, err := h.getBucketWithAccess(ctx, userID, role, req.Bucket)
	if err != nil {
		writeError(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = bucket // Used for access validation

	h.normalizePresignExpiration(req)

	accessKey, err := h.getEnabledAccessKey(ctx, userID)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	presignedURL, err := h.generateURL(req, accessKey)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := ConsolePresignResponse{
		URL:       presignedURL,
		Method:    req.Method,
		Bucket:    req.Bucket,
		Key:       req.Key,
		ExpiresAt: time.Now().Add(time.Duration(req.Expiration) * time.Second),
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) parsePresignRequest(r *http.Request) (*ConsolePresignRequest, error) {
	var req ConsolePresignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.New("Invalid request body")
	}

	if req.Bucket == "" {
		return nil, errors.New("Bucket is required")
	}

	if req.Method == "" {
		req.Method = "GET"
	}
	req.Method = strings.ToUpper(req.Method)

	return &req, nil
}

func (h *Handler) validatePresignRequest(req *ConsolePresignRequest, role metadata.UserRole) error {
	validMethods := map[string]bool{"GET": true, "PUT": true, "DELETE": true, "HEAD": true}
	if !validMethods[req.Method] {
		return errors.New("Invalid method. Allowed: GET, PUT, DELETE, HEAD")
	}
	return nil
}

func (h *Handler) checkPresignPermissions(
	ctx context.Context,
	userID string,
	role metadata.UserRole,
	req *ConsolePresignRequest,
) error {
	if req.Method == http.MethodPut && role == metadata.RoleReadOnly {
		return errors.New("Permission denied: read-only user cannot create upload URLs")
	}

	if req.Method == http.MethodDelete && (role == metadata.RoleReadOnly || role == metadata.RoleUser) {
		return errors.New("Permission denied: cannot create delete URLs")
	}

	return nil
}

func (h *Handler) getBucketWithAccess(
	ctx context.Context,
	userID string,
	role metadata.UserRole,
	bucketName string,
) (*metadata.Bucket, error) {
	bucket, err := h.bucket.GetBucket(ctx, bucketName)
	if err != nil {
		return nil, errors.New("Bucket not found")
	}

	if role != metadata.RoleSuperAdmin && role != metadata.RoleAdmin {
		if bucket.Owner != userID {
			return nil, errors.New("Access denied to bucket")
		}
	}

	return bucket, nil
}

func (h *Handler) normalizePresignExpiration(req *ConsolePresignRequest) {
	if req.Expiration <= 0 {
		req.Expiration = defaultPresignedExpirySec
	}
	if req.Expiration > maxPresignedExpirySec {
		req.Expiration = maxPresignedExpirySec
	}
}

func (h *Handler) getEnabledAccessKey(ctx context.Context, userID string) (*metadata.AccessKey, error) {
	keys, err := h.store.ListAccessKeys(ctx, userID)
	if err != nil || len(keys) == 0 {
		return nil, errors.New("No access keys found for user. Please create an access key first.")
	}

	for _, k := range keys {
		if k.Enabled {
			return k, nil
		}
	}

	return nil, errors.New("No enabled access keys found")
}

func (h *Handler) generateURL(req *ConsolePresignRequest, accessKey *metadata.AccessKey) (string, error) {
	generator := auth.NewPresignedURLGenerator("us-east-1", "")

	return generator.GeneratePresignedURL(auth.PresignParams{
		Method:      req.Method,
		Bucket:      req.Bucket,
		Key:         req.Key,
		Expiration:  time.Duration(req.Expiration) * time.Second,
		AccessKeyID: accessKey.AccessKeyID,
		SecretKey:   accessKey.SecretAccessKey,
		Region:      "us-east-1",
		Headers:     req.Headers,
	})
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, status, map[string]string{"error": message})
}
